// Package admission implements the length + cache-hit-rate ADMISSION judgment.
// It is NOT a scheduler: it only decides whether to accept a request or reject
// it (429), and reports the best cache-hit instance for observability. The
// rules follow PLAN.md:
//
//	input < long_prompt_threshold            -> accept (ordinary)
//	hard cap exceeded                        -> 429 (capacity)
//	events stale/unavailable/hash-mismatch   -> fallback accept (never low-hit 429)
//	best_hit_ratio < min_hit_ratio (long)    -> 429 long_prompt_low_cache_hit
//	else                                     -> accept
package admission

import (
	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/residency"
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
	ReasonOrdinaryShort        Reason = "ordinary_short_prompt"
	ReasonLongPromptHighHit    Reason = "long_prompt_high_cache_hit"
	ReasonLongPromptLowHit     Reason = "long_prompt_low_cache_hit"
	ReasonHardCapacityExceeded Reason = "hard_capacity_limit_exceeded"
	ReasonFallbackStale        Reason = "fallback_stale_events"
	ReasonFallbackUnavailable  Reason = "fallback_tokenization_unavailable"
	ReasonFallbackHashMismatch Reason = "fallback_hash_feature_unsupported"
	ReasonPolicyDisabled       Reason = "policy_disabled"
	ReasonNoCandidates         Reason = "no_eligible_candidates"
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
	InputTokens int
	BlockSize   int
	Policy      config.EffectivePolicy
	Query       *residency.QueryResult // may be nil if no query was possible
	// Fresh indicates events for the matched residency are within TTL.
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
	// MinRequiredHitRatio echoes the threshold used (for long prompts).
	MinRequiredHitRatio float64 `json:"min_required_hit_ratio"`
}

// bestHit picks the instance with the largest tier-weighted effective cached
// tokens and fills a HitInfo. Returns zero HitInfo if no query/instances.
func bestHit(in Input) HitInfo {
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
		eff := float64(gpu)*in.Policy.GPUHitWeight +
			float64(cpuOnly)*in.Policy.CPUHitWeight +
			float64(diskOnly)*in.Policy.DiskHitWeight
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
		// Hit ratio uses effective cached tokens, capped at input length.
		eff := hi.EffectiveCachedTokens
		if eff > in.InputTokens {
			eff = in.InputTokens
		}
		hi.HitRatio = float64(eff) / float64(in.InputTokens)
	}
	return hi
}

// Evaluate runs the admission judgment.
func Evaluate(in Input) Result {
	pol := in.Policy
	hit := bestHit(in)
	res := Result{Hit: hit, MinRequiredHitRatio: pol.MinHitRatioForLongPrompt}

	// Policy disabled => always accept (ordinary).
	if !pol.Enabled {
		return accept(res, ReasonPolicyDisabled, false)
	}

	// Hard capacity / context limit is checked first and unconditionally.
	if pol.HardLongPromptThresholdTokens > 0 && in.InputTokens >= pol.HardLongPromptThresholdTokens {
		res.Decision = DecisionReject
		res.Reason = ReasonHardCapacityExceeded
		res.HTTPStatus = pol.LowHitRejectStatus
		return res
	}

	// Short prompts always route ordinarily.
	if in.InputTokens < pol.LongPromptThresholdTokens {
		return accept(res, ReasonOrdinaryShort, false)
	}

	// From here the prompt is "long". If we cannot trust the hit signal, we
	// fall back to accepting (never low-hit 429 under uncertainty).
	if !in.Tokenized {
		return accept(res, ReasonFallbackUnavailable, true)
	}
	if !in.HashSupported {
		return accept(res, ReasonFallbackHashMismatch, true)
	}
	if !in.HasCandidates {
		// No engine to serve it; this is a routing concern. Accept as fallback
		// (the gateway handles absence of a target). Checked before freshness
		// so the reason is precise.
		return accept(res, ReasonNoCandidates, true)
	}
	if !in.Fresh {
		// Stale/disconnected event stream: the index view can't be trusted, so
		// a miss might be spurious. Fall back rather than 429.
		return accept(res, ReasonFallbackStale, true)
	}

	// Long prompt with a trustworthy hit signal: apply the hit-ratio gate.
	if hit.HitRatio < pol.MinHitRatioForLongPrompt {
		res.Decision = DecisionReject
		res.Reason = ReasonLongPromptLowHit
		res.HTTPStatus = pol.LowHitRejectStatus
		return res
	}
	return accept(res, ReasonLongPromptHighHit, false)
}

func accept(res Result, reason Reason, fallback bool) Result {
	res.Decision = DecisionAccept
	res.Reason = reason
	res.HTTPStatus = 200
	res.Fallback = fallback
	return res
}
