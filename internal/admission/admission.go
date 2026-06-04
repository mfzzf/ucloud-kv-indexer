// Package admission implements rule-based KV-cache admission. A policy set is
// evaluated as an ordered OR list: rules are sorted by priority descending, and
// the first enabled rule whose conditions all match decides the request.
package admission

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/residency"
)

const (
	gpuHitWeight  = 1.0
	cpuHitWeight  = 0.75
	diskHitWeight = 0.25
)

// Decision is the admission verdict.
type Decision string

const (
	DecisionAccept Decision = "accept"
	DecisionReject Decision = "reject"
)

// Reason categorizes the verdict for structured responses and dashboards.
type Reason string

const (
	ReasonNoMatchingRule       Reason = "no_matching_policy_rule"
	ReasonRuleAccepted         Reason = "policy_rule_accept"
	ReasonRuleRejected         Reason = "policy_rule_reject"
	ReasonCacheHitRequirement  Reason = "kv_cache_hit_requirement_met"
	ReasonCacheHitTooLow       Reason = "kv_cache_hit_requirement_failed"
	ReasonFallbackStale        Reason = "fallback_stale_events"
	ReasonFallbackUnavailable  Reason = "fallback_tokenization_unavailable"
	ReasonFallbackHashMismatch Reason = "fallback_hash_feature_unsupported"
	ReasonNoCandidates         Reason = "no_eligible_candidates"
	ReasonUnknownAction        Reason = "unknown_policy_action"
)

// HitInfo summarizes the best instance's cache hit for the request.
type HitInfo struct {
	InstanceID    string  `json:"instance_id,omitempty"`
	InputTokens   int     `json:"input_tokens"`
	BestHitTokens int     `json:"best_hit_tokens"`
	HitRatio      float64 `json:"hit_ratio"`
	GPUTokens     int     `json:"gpu_tokens"`
	CPUTokens     int     `json:"cpu_tokens"`
	DiskTokens    int     `json:"disk_tokens"`
	// EffectiveCachedTokens is the tier-weighted credit used for the ratio.
	EffectiveCachedTokens int `json:"effective_cached_tokens"`
}

// Input bundles everything the judgment needs.
type Input struct {
	ClusterID string
	ModelID   string
	TenantID  string

	InputTokens int
	BlockSize   int
	Rules       []config.Policy
	Query       *residency.QueryResult // may be nil if no query was possible
	HitOverride *HitInfo               // optional synthetic hit, used by previews
	// Fresh indicates events for the matched residency are trustworthy.
	Fresh bool
	// Tokenized indicates tokenization succeeded (token IDs trusted).
	Tokenized bool
	// HashSupported indicates the request's features are supported by the
	// active hash profile (no LoRA/MM/cache_salt mismatch).
	HashSupported bool
	// HasCandidates indicates at least one eligible engine serves the model.
	HasCandidates bool
}

// Result is the full admission outcome.
type Result struct {
	Decision   Decision `json:"decision"`
	Reason     Reason   `json:"reason"`
	HTTPStatus int      `json:"http_status"`
	Fallback   bool     `json:"fallback"`
	Hit        HitInfo  `json:"hit"`
	// MinRequiredHitRatio echoes the threshold used by require_cache_hit.
	MinRequiredHitRatio float64  `json:"min_required_hit_ratio"`
	MatchedRuleID       string   `json:"matched_rule_id,omitempty"`
	MatchedRuleName     string   `json:"matched_rule_name,omitempty"`
	MatchedRulePriority int      `json:"matched_rule_priority,omitempty"`
	EvaluatedRuleIDs    []string `json:"evaluated_rule_ids,omitempty"`
}

// bestHit picks the instance with the largest tier-weighted effective cached
// tokens and fills a HitInfo. Returns zero HitInfo if no query/instances.
func bestHit(in Input) HitInfo {
	if in.HitOverride != nil {
		hi := *in.HitOverride
		if hi.InputTokens == 0 {
			hi.InputTokens = in.InputTokens
		}
		return hi
	}
	hi := HitInfo{InputTokens: in.InputTokens}
	if in.Query == nil {
		return hi
	}
	bestEff := -1.0
	for id, inst := range in.Query.Instances {
		// Tier counts are cumulative (gpu <= cpu <= disk). Convert to disjoint
		// tier token counts for weighting.
		gpu := inst.GPU
		cpuOnly := inst.CPU - inst.GPU
		diskOnly := inst.Disk - inst.CPU
		if cpuOnly < 0 {
			cpuOnly = 0
		}
		if diskOnly < 0 {
			diskOnly = 0
		}
		eff := float64(gpu)*gpuHitWeight +
			float64(cpuOnly)*cpuHitWeight +
			float64(diskOnly)*diskHitWeight
		if eff > bestEff {
			bestEff = eff
			hi.InstanceID = id
			hi.BestHitTokens = inst.LongestMatched
			hi.GPUTokens = inst.GPU
			hi.CPUTokens = inst.CPU
			hi.DiskTokens = inst.Disk
			hi.EffectiveCachedTokens = int(eff)
		}
	}
	if in.InputTokens > 0 {
		eff := hi.EffectiveCachedTokens
		if eff > in.InputTokens {
			eff = in.InputTokens
		}
		hi.HitRatio = float64(eff) / float64(in.InputTokens)
	}
	return hi
}

// Evaluate runs the rule-based admission judgment.
func Evaluate(in Input) Result {
	hit := bestHit(in)
	res := Result{Hit: hit}
	rules := append([]config.Policy(nil), in.Rules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].RuleID < rules[j].RuleID
	})

	for _, rule := range rules {
		if rule.RuleID != "" {
			res.EvaluatedRuleIDs = append(res.EvaluatedRuleIDs, rule.RuleID)
		}
		if !rule.IsEnabled() || rule.RuleID == "" {
			continue
		}
		if !matchesRule(in, hit, rule) {
			continue
		}
		res.MatchedRuleID = rule.RuleID
		res.MatchedRuleName = rule.DisplayName()
		res.MatchedRulePriority = rule.Priority
		return applyAction(in, res, rule.Action)
	}

	return accept(res, ReasonNoMatchingRule, false)
}

func applyAction(in Input, res Result, action config.RuleAction) Result {
	switch action.TypeOrDefault() {
	case config.ActionAccept:
		return accept(res, ReasonRuleAccepted, false)
	case config.ActionReject:
		return reject(res, ReasonRuleRejected, action.RejectStatusOrDefault())
	case config.ActionRequireCacheHit:
		res.MinRequiredHitRatio = action.MinHitRatioOrDefault()
		if reason, uncertain := uncertaintyReason(in); uncertain {
			return applyOutcome(res, action.OnUncertainOrDefault(), reason, action.RejectStatusOrDefault(), true)
		}
		if res.Hit.HitRatio < res.MinRequiredHitRatio {
			return applyOutcome(res, action.OnLowHitOrDefault(), ReasonCacheHitTooLow, action.RejectStatusOrDefault(), false)
		}
		return accept(res, ReasonCacheHitRequirement, false)
	default:
		return reject(res, ReasonUnknownAction, action.RejectStatusOrDefault())
	}
}

func uncertaintyReason(in Input) (Reason, bool) {
	if !in.Tokenized {
		return ReasonFallbackUnavailable, true
	}
	if !in.HashSupported {
		return ReasonFallbackHashMismatch, true
	}
	if !in.HasCandidates {
		return ReasonNoCandidates, true
	}
	if !in.Fresh {
		return ReasonFallbackStale, true
	}
	return "", false
}

func applyOutcome(res Result, outcome string, reason Reason, status int, uncertainty bool) Result {
	switch outcome {
	case config.RuleOutcomeAccept:
		return accept(res, reason, false)
	case config.RuleOutcomeFallbackAccept:
		return accept(res, reason, true)
	case config.RuleOutcomeReject:
		return reject(res, reason, status)
	default:
		if uncertainty {
			return accept(res, reason, true)
		}
		return reject(res, reason, status)
	}
}

func accept(res Result, reason Reason, fallback bool) Result {
	res.Decision = DecisionAccept
	res.Reason = reason
	res.HTTPStatus = 200
	res.Fallback = fallback
	return res
}

func reject(res Result, reason Reason, status int) Result {
	res.Decision = DecisionReject
	res.Reason = reason
	if status <= 0 {
		status = 429
	}
	res.HTTPStatus = status
	return res
}

func matchesRule(in Input, hit HitInfo, rule config.Policy) bool {
	for _, cond := range rule.Conditions {
		actual, ok := conditionValue(in, hit, cond.Field)
		if !ok || !matchesCondition(actual, cond) {
			return false
		}
	}
	return true
}

func conditionValue(in Input, hit HitInfo, field string) (any, bool) {
	switch field {
	case config.ConditionFieldClusterID:
		return in.ClusterID, true
	case config.ConditionFieldModelID:
		return in.ModelID, true
	case config.ConditionFieldTenantID:
		return in.TenantID, true
	case config.ConditionFieldInputTokens:
		return in.InputTokens, true
	case config.ConditionFieldHitRatio:
		return hit.HitRatio, true
	case config.ConditionFieldBestHitTokens:
		return hit.BestHitTokens, true
	case config.ConditionFieldEffectiveCachedTokens:
		return hit.EffectiveCachedTokens, true
	case config.ConditionFieldKVEventState:
		if in.Fresh {
			return config.KVEventStateAvailable, true
		}
		return config.KVEventStateStale, true
	case config.ConditionFieldTokenized:
		return in.Tokenized, true
	case config.ConditionFieldHashSupported:
		return in.HashSupported, true
	case config.ConditionFieldHasCandidates:
		return in.HasCandidates, true
	default:
		return nil, false
	}
}

func matchesCondition(actual any, cond config.RuleCondition) bool {
	switch cond.Op {
	case "", config.ConditionOpEq:
		return valuesEqual(actual, cond.Value)
	case config.ConditionOpNeq:
		return !valuesEqual(actual, cond.Value)
	case config.ConditionOpIn:
		return listContains(cond.Value, actual)
	case config.ConditionOpNotIn:
		return !listContains(cond.Value, actual)
	case config.ConditionOpGT, config.ConditionOpGTE, config.ConditionOpLT, config.ConditionOpLTE:
		return compareNumeric(actual, cond.Value, cond.Op)
	case config.ConditionOpContains:
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(cond.Value))
	default:
		return false
	}
}

func valuesEqual(a, b any) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	if ab, ok := toBool(a); ok {
		if bb, ok := toBool(b); ok {
			return ab == bb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func compareNumeric(actual, expected any, op string) bool {
	a, ok := toFloat(actual)
	if !ok {
		return false
	}
	b, ok := toFloat(expected)
	if !ok {
		return false
	}
	switch op {
	case config.ConditionOpGT:
		return a > b
	case config.ConditionOpGTE:
		return a >= b
	case config.ConditionOpLT:
		return a < b
	case config.ConditionOpLTE:
		return a <= b
	default:
		return false
	}
}

func listContains(list any, actual any) bool {
	v := reflect.ValueOf(list)
	if v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
		for i := 0; i < v.Len(); i++ {
			if valuesEqual(actual, v.Index(i).Interface()) {
				return true
			}
		}
		return false
	}
	return valuesEqual(actual, list)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(x)
		return b, err == nil
	default:
		return false, false
	}
}
