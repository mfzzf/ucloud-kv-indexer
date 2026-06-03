package config

import "testing"

func ptrInt(i int) *int       { return &i }
func ptrF(f float64) *float64 { return &f }
func ptrB(b bool) *bool       { return &b }
func ptrS(s string) *string   { return &s }

func TestEffectivePolicyMergeOrder(t *testing.T) {
	s := NewStore("", nil)
	// global-ish (no scope) sets threshold 100
	s.UpsertPolicy(Policy{PolicyID: "g", LongPromptThresholdTokens: ptrInt(100)})
	// model scope overrides to 200 and sets min hit 0.6
	s.UpsertPolicy(Policy{PolicyID: "m", Scope: Scope{ModelID: "qwen"},
		LongPromptThresholdTokens: ptrInt(200), MinHitRatioForLongPrompt: ptrF(0.6)})
	// tenant+model scope overrides threshold to 300 (most specific)
	s.UpsertPolicy(Policy{PolicyID: "t", Scope: Scope{ModelID: "qwen", TenantID: "acme"},
		LongPromptThresholdTokens: ptrInt(300)})

	eff := s.EffectivePolicy("", "qwen", "acme")
	if eff.LongPromptThresholdTokens != 300 {
		t.Fatalf("most specific should win threshold=300, got %d", eff.LongPromptThresholdTokens)
	}
	if eff.MinHitRatioForLongPrompt != 0.6 {
		t.Fatalf("model-scope min hit should survive=0.6, got %v", eff.MinHitRatioForLongPrompt)
	}
	// source order: global, model, tenant (plus default at front).
	if len(eff.SourcePolicyIDs) != 4 {
		t.Fatalf("expected 4 sources (default+3), got %v", eff.SourcePolicyIDs)
	}
	if eff.SourcePolicyIDs[len(eff.SourcePolicyIDs)-1] != "t" {
		t.Fatalf("tenant policy should be last (highest precedence): %v", eff.SourcePolicyIDs)
	}
}

func TestEffectivePolicyScopeFiltering(t *testing.T) {
	s := NewStore("", nil)
	s.UpsertPolicy(Policy{PolicyID: "other", Scope: Scope{ModelID: "llama"},
		LongPromptThresholdTokens: ptrInt(999), Enabled: ptrB(false)})
	eff := s.EffectivePolicy("", "qwen", "acme")
	// llama policy must NOT apply to qwen; falls back to default.
	def := DefaultEffectivePolicy()
	if eff.LongPromptThresholdTokens != def.LongPromptThresholdTokens {
		t.Fatalf("non-matching scope must not apply; got %d", eff.LongPromptThresholdTokens)
	}
	if !eff.Enabled {
		t.Fatalf("default should be enabled (other policy must not disable qwen)")
	}
}

func TestRemovePolicy(t *testing.T) {
	s := NewStore("", nil)
	s.UpsertPolicy(Policy{PolicyID: "p1", Scope: Scope{ModelID: "qwen"}, Enabled: ptrB(false)})
	if !s.RemovePolicy("p1") {
		t.Fatal("expected existing policy to be removed")
	}
	if s.RemovePolicy("p1") {
		t.Fatal("expected missing policy remove to return false")
	}
	if ps := s.ListPolicies(); len(ps) != 0 {
		t.Fatalf("policy not removed: %+v", ps)
	}
	audit := s.Audit()
	if got := audit[len(audit)-1]; got.Action != "remove" || got.Entity != "policy" || got.EntityID != "p1" {
		t.Fatalf("remove audit missing: %+v", got)
	}
}

func TestModelProfileVersionBump(t *testing.T) {
	s := NewStore("", nil)
	p := s.UpsertModelProfile(ModelProfile{ModelID: "qwen", HashProfile: "vllm-v1-text", BlockSize: 16})
	if p.Version != 1 {
		t.Fatalf("first profile should be v1, got %d", p.Version)
	}
	// Non-hash change (e.g. nothing material) should keep version.
	p2 := s.UpsertModelProfile(ModelProfile{ModelID: "qwen", HashProfile: "vllm-v1-text", BlockSize: 16})
	if p2.Version != 1 {
		t.Fatalf("identical profile should stay v1, got %d", p2.Version)
	}
	// Block size change MUST bump version (affects request_key namespace).
	p3 := s.UpsertModelProfile(ModelProfile{ModelID: "qwen", HashProfile: "vllm-v1-text", BlockSize: 32})
	if p3.Version != 2 {
		t.Fatalf("block size change should bump to v2, got %d", p3.Version)
	}
	if p.Namespace() == p3.Namespace() {
		t.Fatalf("namespace must differ after version bump: %s vs %s", p.Namespace(), p3.Namespace())
	}
	// Audit should flag the bump.
	var bumps int
	for _, a := range s.Audit() {
		if a.VersionBump {
			bumps++
		}
	}
	if bumps != 1 {
		t.Fatalf("expected exactly 1 version-bump audit entry, got %d", bumps)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cfg.json"
	s := NewStore(path, nil)
	s.UpsertCluster(Cluster{ClusterID: "c1", Enabled: true})
	s.UpsertModelProfile(ModelProfile{ModelID: "qwen", HashProfile: "h", BlockSize: 16})
	v := s.Version()

	s2 := NewStore(path, nil)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if s2.Version() != v {
		t.Fatalf("version mismatch after reload: %d vs %d", s2.Version(), v)
	}
	if len(s2.ListClusters()) != 1 || len(s2.ListModelProfiles()) != 1 {
		t.Fatalf("entities not restored")
	}
}
