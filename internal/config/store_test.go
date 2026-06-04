package config

import "testing"

func ptrB(b bool) *bool { return &b }

func TestPoliciesSortedByPriority(t *testing.T) {
	s := NewStore("", nil)
	s.UpsertPolicy(Policy{RuleID: "b", Priority: 10})
	s.UpsertPolicy(Policy{RuleID: "a", Priority: 100})
	s.UpsertPolicy(Policy{RuleID: "c", Priority: 100})

	policies := s.ListPolicies()
	if len(policies) != 3 {
		t.Fatalf("want 3 policies, got %+v", policies)
	}
	got := []string{policies[0].RuleID, policies[1].RuleID, policies[2].RuleID}
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("policies sorted wrong: got %v want %v", got, want)
		}
	}
}

func TestPatchPolicy(t *testing.T) {
	s := NewStore("", nil)
	s.UpsertPolicy(Policy{RuleID: "p1", Priority: 1})
	ok := s.PatchPolicy("p1", func(p *Policy) {
		p.Name = "patched"
		p.Priority = 99
	})
	if !ok {
		t.Fatal("patch should find policy")
	}
	policies := s.ListPolicies()
	if len(policies) != 1 || policies[0].Name != "patched" || policies[0].Priority != 99 {
		t.Fatalf("patch not applied: %+v", policies)
	}
}

func TestRemovePolicy(t *testing.T) {
	s := NewStore("", nil)
	s.UpsertPolicy(Policy{RuleID: "p1", Enabled: ptrB(false)})
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
