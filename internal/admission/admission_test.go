package admission

import (
	"testing"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/residency"
)

func ptrF(f float64) *float64 { return &f }
func ptrB(b bool) *bool       { return &b }

func requireCacheRule(id string, priority int, minTokens int, minHit float64) config.Policy {
	return config.Policy{
		RuleID:   id,
		Name:     id,
		Priority: priority,
		Conditions: []config.RuleCondition{{
			Field: config.ConditionFieldInputTokens,
			Op:    config.ConditionOpGTE,
			Value: minTokens,
		}},
		Action: config.RuleAction{
			Type:         config.ActionRequireCacheHit,
			MinHitRatio:  ptrF(minHit),
			OnLowHit:     config.RuleOutcomeReject,
			OnUncertain:  config.RuleOutcomeFallbackAccept,
			RejectStatus: 429,
		},
	}
}

func rejectRule(id string, priority int, minTokens int) config.Policy {
	return config.Policy{
		RuleID:   id,
		Priority: priority,
		Conditions: []config.RuleCondition{{
			Field: config.ConditionFieldInputTokens,
			Op:    config.ConditionOpGTE,
			Value: minTokens,
		}},
		Action: config.RuleAction{Type: config.ActionReject, RejectStatus: 429},
	}
}

func queryWith(longest, gpu, cpu, disk int) *residency.QueryResult {
	return &residency.QueryResult{Instances: map[string]*residency.InstanceHit{
		"w0": {LongestMatched: longest, GPU: gpu, CPU: cpu, Disk: disk, DP: map[string]int{"0": longest}},
	}}
}

func baseInput(tokens int, rules []config.Policy, q *residency.QueryResult) Input {
	return Input{
		ClusterID:     "local-vllm",
		ModelID:       "qwen",
		TenantID:      "default",
		InputTokens:   tokens,
		BlockSize:     16,
		Rules:         rules,
		Tokenized:     true,
		HashSupported: true,
		Fresh:         true,
		HasCandidates: true,
		Query:         q,
	}
}

func TestNoMatchingRuleAccepts(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	r := Evaluate(baseInput(128, rules, queryWith(0, 0, 0, 0)))
	if r.Decision != DecisionAccept || r.Reason != ReasonNoMatchingRule {
		t.Fatalf("no matching rule should accept, got %+v", r)
	}
}

func TestRequireCacheHitLowHitRejects(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	r := Evaluate(baseInput(500, rules, queryWith(100, 100, 100, 100)))
	if r.Decision != DecisionReject || r.Reason != ReasonCacheHitTooLow {
		t.Fatalf("low hit should reject, got %+v", r)
	}
	if r.MatchedRuleID != "long" || r.HTTPStatus != 429 {
		t.Fatalf("matched rule/status wrong: %+v", r)
	}
}

func TestRequireCacheHitHighHitAccepts(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	r := Evaluate(baseInput(500, rules, queryWith(400, 400, 400, 400)))
	if r.Decision != DecisionAccept || r.Reason != ReasonCacheHitRequirement {
		t.Fatalf("high hit should accept, got %+v", r)
	}
}

func TestStaleEventsFallbackAccepts(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	in := baseInput(500, rules, queryWith(0, 0, 0, 0))
	in.Fresh = false
	r := Evaluate(in)
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackStale || !r.Fallback {
		t.Fatalf("stale events must fallback accept, got %+v", r)
	}
}

func TestTokenizationUnavailableFallback(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	in := baseInput(500, rules, nil)
	in.Tokenized = false
	r := Evaluate(in)
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackUnavailable || !r.Fallback {
		t.Fatalf("untokenized request must fallback accept, got %+v", r)
	}
}

func TestHashFeatureUnsupportedFallback(t *testing.T) {
	rules := []config.Policy{requireCacheRule("long", 100, 256, 0.5)}
	in := baseInput(500, rules, queryWith(0, 0, 0, 0))
	in.HashSupported = false
	r := Evaluate(in)
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackHashMismatch || !r.Fallback {
		t.Fatalf("hash-unsupported request must fallback accept, got %+v", r)
	}
}

func TestPriorityFirstMatchWins(t *testing.T) {
	rules := []config.Policy{
		requireCacheRule("low-priority-cache", 50, 256, 0.5),
		rejectRule("high-priority-cap", 100, 1024),
	}
	r := Evaluate(baseInput(2000, rules, queryWith(2000, 2000, 2000, 2000)))
	if r.Decision != DecisionReject || r.Reason != ReasonRuleRejected || r.MatchedRuleID != "high-priority-cap" {
		t.Fatalf("highest priority matching rule should win, got %+v", r)
	}
}

func TestAllConditionsAreAND(t *testing.T) {
	rules := []config.Policy{{
		RuleID:   "cluster-and-model",
		Priority: 100,
		Conditions: []config.RuleCondition{
			{Field: config.ConditionFieldClusterID, Op: config.ConditionOpEq, Value: "local-vllm"},
			{Field: config.ConditionFieldModelID, Op: config.ConditionOpEq, Value: "qwen"},
			{Field: config.ConditionFieldInputTokens, Op: config.ConditionOpGTE, Value: 256},
		},
		Action: config.RuleAction{Type: config.ActionReject, RejectStatus: 429},
	}}
	in := baseInput(500, rules, nil)
	in.ModelID = "llama"
	r := Evaluate(in)
	if r.Decision != DecisionAccept || r.Reason != ReasonNoMatchingRule {
		t.Fatalf("one failed condition should miss the rule, got %+v", r)
	}
}

func TestDisabledRuleSkipped(t *testing.T) {
	rule := rejectRule("off", 100, 0)
	rule.Enabled = ptrB(false)
	r := Evaluate(baseInput(500, []config.Policy{rule}, nil))
	if r.Decision != DecisionAccept || r.Reason != ReasonNoMatchingRule {
		t.Fatalf("disabled rule should be skipped, got %+v", r)
	}
}
