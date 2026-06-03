package config

import "testing"

func multiClusterBootstrap() *Bootstrap {
	enabled := true
	long := 4096
	return &Bootstrap{
		Clusters: []BootstrapCluster{
			{ClusterID: "h20-1", Enabled: &enabled, Backends: []string{"http://10.0.0.1:8090"}},
			{ClusterID: "h20-2", Enabled: &enabled, Backends: []string{"http://10.0.1.1:8090"}},
			{ClusterID: "h200-1", Enabled: &enabled, Backends: []string{"http://10.0.2.1:8090"}},
		},
		Profiles: []BootstrapProfile{
			{ModelID: "qwen3", Framework: "sglang", HashProfile: "sglang-v1-text", BlockSize: 1, HashSeed: "0"},
			{ModelID: "qwen2.5", Framework: "vllm", HashProfile: "vllm-v1-text", BlockSize: 16, HashSeed: "0"},
			{ModelID: "qwen3.6", Framework: "sglang", HashProfile: "sglang-v1-text", BlockSize: 1, HashSeed: "0"},
		},
		Engines: []BootstrapEngine{
			{EngineID: "sglang-h20-1-0", ClusterID: "h20-1", Framework: "sglang", ServedModels: []string{"qwen3"}},
			{EngineID: "vllm-h20-2-0", ClusterID: "h20-2", Framework: "vllm", ServedModels: []string{"qwen2.5"}},
			{EngineID: "sglang-h200-1-0", ClusterID: "h200-1", Framework: "sglang", ServedModels: []string{"qwen3.6"}},
		},
		Policies: []BootstrapPolicy{
			{PolicyID: "global-default", LongPromptThresholdTokens: &long, Enabled: &enabled},
			{PolicyID: "qwen3.6-long", ScopeModelID: "qwen3.6", LongPromptThresholdTokens: &long, Enabled: &enabled},
		},
	}
}

func TestApplyBootstrapWholeTopology(t *testing.T) {
	st := NewStoreWith(nil, nil)
	if !st.ApplyBootstrap(multiClusterBootstrap()) {
		t.Fatal("ApplyBootstrap should return true on empty store")
	}
	if n := len(st.ListClusters()); n != 3 {
		t.Fatalf("want 3 clusters, got %d", n)
	}
	if n := len(st.ListEngines()); n != 3 {
		t.Fatalf("want 3 engines, got %d", n)
	}
	if n := len(st.ListModelProfiles()); n != 3 {
		t.Fatalf("want 3 profiles, got %d", n)
	}
	if n := len(st.ListPolicies()); n != 2 {
		t.Fatalf("want 2 policies, got %d", n)
	}
	// Frameworks must round-trip so the adapter selection works downstream.
	if p, _ := st.ResolveProfile("qwen2.5"); p.Framework != FrameworkVLLM {
		t.Fatalf("qwen2.5 should be vllm, got %q", p.Framework)
	}
	if p, _ := st.ResolveProfile("qwen3"); p.Framework != FrameworkSGLang {
		t.Fatalf("qwen3 should be sglang, got %q", p.Framework)
	}
}

func TestApplyBootstrapScopedToCluster(t *testing.T) {
	st := NewStoreWith(nil, nil)
	if !st.ApplyBootstrapForCluster(multiClusterBootstrap(), "h20-2") {
		t.Fatal("scoped ApplyBootstrap should return true")
	}
	// Only the h20-2 cluster + its engine + its served model are seeded.
	if cs := st.ListClusters(); len(cs) != 1 || cs[0].ClusterID != "h20-2" {
		t.Fatalf("scoped clusters wrong: %+v", cs)
	}
	if es := st.ListEngines(); len(es) != 1 || es[0].EngineID != "vllm-h20-2-0" {
		t.Fatalf("scoped engines wrong: %+v", es)
	}
	if ps := st.ListModelProfiles(); len(ps) != 1 || ps[0].ModelID != "qwen2.5" {
		t.Fatalf("scoped profiles wrong: %+v", ps)
	}
	// global-default (no scope) is kept; qwen3.6-long (scopes a non-served model)
	// is dropped under this cluster.
	pols := st.ListPolicies()
	if len(pols) != 1 || pols[0].PolicyID != "global-default" {
		t.Fatalf("scoped policies wrong: %+v", pols)
	}
}

func TestApplyBootstrapLegacySingleCluster(t *testing.T) {
	enabled := true
	bs := &Bootstrap{
		Cluster:  BootstrapCluster{ClusterID: "local", Region: "localhost", Enabled: &enabled},
		Engines:  []BootstrapEngine{{EngineID: "e0", ClusterID: "local", Framework: "sglang", ServedModels: []string{"m"}}},
		Profiles: []BootstrapProfile{{ModelID: "m", Framework: "sglang", HashProfile: "x", BlockSize: 1}},
	}
	st := NewStoreWith(nil, nil)
	if !st.ApplyBootstrap(bs) {
		t.Fatal("legacy single-cluster bootstrap should apply")
	}
	if cs := st.ListClusters(); len(cs) != 1 || cs[0].ClusterID != "local" {
		t.Fatalf("legacy cluster not seeded: %+v", cs)
	}
}

func TestAllClustersMergesAndDedups(t *testing.T) {
	bs := &Bootstrap{
		Cluster:  BootstrapCluster{ClusterID: "dup"},
		Clusters: []BootstrapCluster{{ClusterID: "dup"}, {ClusterID: "other"}},
	}
	cs := bs.AllClusters()
	if len(cs) != 2 {
		t.Fatalf("want 2 deduped clusters, got %d: %+v", len(cs), cs)
	}
}

func TestFlattenNestedInheritsClusterFields(t *testing.T) {
	bs := &Bootstrap{
		Clusters: []BootstrapCluster{{
			ClusterID: "h20-1",
			Framework: "sglang",
			Models: []BootstrapProfile{
				{ModelID: "qwen3", BlockSize: 1, HashSeed: "0"}, // framework+hash_profile inherited
			},
			Engines: []BootstrapEngine{
				{EngineID: "e0", ServedModels: []string{"qwen3"}}, // cluster_id+framework inherited
			},
		}},
	}
	bs.flattenNested()

	if len(bs.Engines) != 1 {
		t.Fatalf("nested engine not flattened: %+v", bs.Engines)
	}
	e := bs.Engines[0]
	if e.ClusterID != "h20-1" || e.Framework != "sglang" {
		t.Fatalf("engine did not inherit cluster fields: %+v", e)
	}
	if len(bs.Profiles) != 1 {
		t.Fatalf("nested model not flattened: %+v", bs.Profiles)
	}
	p := bs.Profiles[0]
	if p.Framework != "sglang" || p.HashProfile != "sglang-v1-text" {
		t.Fatalf("model did not inherit framework/default hash_profile: %+v", p)
	}
	// Nested fields cleared so cluster rows stay clean.
	if bs.Clusters[0].Engines != nil || bs.Clusters[0].Models != nil {
		t.Fatalf("nested fields should be cleared after flatten")
	}
}

func TestNestedExplicitOverridesInheritance(t *testing.T) {
	bs := &Bootstrap{
		Clusters: []BootstrapCluster{{
			ClusterID: "mixed",
			Framework: "sglang",
			Models: []BootstrapProfile{
				{ModelID: "m", Framework: "vllm", HashProfile: "custom"}, // explicit wins
			},
		}},
	}
	bs.flattenNested()
	p := bs.Profiles[0]
	if p.Framework != "vllm" || p.HashProfile != "custom" {
		t.Fatalf("explicit fields should override cluster defaults: %+v", p)
	}
}
