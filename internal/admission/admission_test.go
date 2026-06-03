package admission

import (
	"testing"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/residency"
)

func basePolicy() config.EffectivePolicy {
	p := config.DefaultEffectivePolicy()
	p.LongPromptThresholdTokens = 100
	p.HardLongPromptThresholdTokens = 1000
	p.MinHitRatioForLongPrompt = 0.5
	return p
}

func queryWith(longest, gpu, cpu, disk int) *residency.QueryResult {
	return &residency.QueryResult{Instances: map[string]*residency.InstanceHit{
		"w0": {LongestMatched: longest, GPU: gpu, CPU: cpu, Disk: disk, DP: map[string]int{"0": longest}},
	}}
}

func TestShortPromptAlwaysAccepts(t *testing.T) {
	r := Evaluate(Input{InputTokens: 50, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true,
		Query: queryWith(0, 0, 0, 0)})
	if r.Decision != DecisionAccept || r.Reason != ReasonOrdinaryShort {
		t.Fatalf("short prompt should accept ordinary, got %+v", r)
	}
}

func TestHardCapacityRejects(t *testing.T) {
	r := Evaluate(Input{InputTokens: 2000, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true,
		Query: queryWith(2000, 2000, 2000, 2000)}) // even full hit
	if r.Decision != DecisionReject || r.Reason != ReasonHardCapacityExceeded {
		t.Fatalf("hard cap should reject regardless of hit, got %+v", r)
	}
}

func TestLongPromptLowHitRejects(t *testing.T) {
	// input 500 (long), gpu hit only 100 => effective 100, ratio 0.2 < 0.5.
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true,
		Query: queryWith(100, 100, 100, 100)})
	if r.Decision != DecisionReject || r.Reason != ReasonLongPromptLowHit {
		t.Fatalf("long low-hit should reject, got %+v", r)
	}
	if r.HTTPStatus != 429 {
		t.Fatalf("expected 429, got %d", r.HTTPStatus)
	}
}

func TestLongPromptHighHitAccepts(t *testing.T) {
	// input 500, gpu hit 400 => ratio 0.8 >= 0.5.
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true,
		Query: queryWith(400, 400, 400, 400)})
	if r.Decision != DecisionAccept || r.Reason != ReasonLongPromptHighHit {
		t.Fatalf("long high-hit should accept, got %+v", r)
	}
}

func TestStaleEventsFallbackAccepts(t *testing.T) {
	// Long, low hit, but events are stale => must fallback accept, NOT 429.
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: true, Fresh: false, HasCandidates: true,
		Query: queryWith(0, 0, 0, 0)})
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackStale || !r.Fallback {
		t.Fatalf("stale events must fallback accept, got %+v", r)
	}
}

func TestTokenizationUnavailableFallback(t *testing.T) {
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: basePolicy(),
		Tokenized: false, HashSupported: true, Fresh: true, HasCandidates: true})
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackUnavailable {
		t.Fatalf("untokenized long prompt must fallback accept, got %+v", r)
	}
}

func TestHashFeatureUnsupportedFallback(t *testing.T) {
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: basePolicy(),
		Tokenized: true, HashSupported: false, Fresh: true, HasCandidates: true,
		Query: queryWith(0, 0, 0, 0)})
	if r.Decision != DecisionAccept || r.Reason != ReasonFallbackHashMismatch {
		t.Fatalf("hash-unsupported long prompt must fallback accept, got %+v", r)
	}
}

func TestTierWeightingAffectsRatio(t *testing.T) {
	p := basePolicy()
	p.GPUHitWeight = 1.0
	p.CPUHitWeight = 0.0
	p.DiskHitWeight = 0.0
	// longest=400 but ALL on cpu-only (gpu=0). Effective gpu-weighted = 0.
	q := &residency.QueryResult{Instances: map[string]*residency.InstanceHit{
		"w0": {LongestMatched: 400, GPU: 0, CPU: 400, Disk: 400, DP: map[string]int{"0": 400}},
	}}
	r := Evaluate(Input{InputTokens: 500, BlockSize: 16, Policy: p,
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true, Query: q})
	if r.Decision != DecisionReject {
		t.Fatalf("cpu-only hit with gpu-only weighting should reject, got %+v (ratio=%v)", r, r.Hit.HitRatio)
	}
}

func TestPolicyDisabledAlwaysAccepts(t *testing.T) {
	p := basePolicy()
	p.Enabled = false
	r := Evaluate(Input{InputTokens: 5000, BlockSize: 16, Policy: p,
		Tokenized: true, HashSupported: true, Fresh: true, HasCandidates: true,
		Query: queryWith(0, 0, 0, 0)})
	if r.Decision != DecisionAccept || r.Reason != ReasonPolicyDisabled {
		t.Fatalf("disabled policy should accept, got %+v", r)
	}
}
