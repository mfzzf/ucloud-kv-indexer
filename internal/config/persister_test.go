package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePersisterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	p := NewFilePersister(path)

	// Fresh file => ok=false, no error.
	if _, ok, err := p.Load(); err != nil || ok {
		t.Fatalf("fresh load: ok=%v err=%v", ok, err)
	}

	st := NewStoreWith(p, nil)
	st.UpsertCluster(Cluster{ClusterID: "c1", Region: "r1", Enabled: true})
	st.UpsertEngine(Engine{EngineID: "e1", ClusterID: "c1", ServedModels: []string{"m"}})

	// File should now exist and reload into a fresh store identically.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	st2 := NewStoreWith(NewFilePersister(path), nil)
	if err := st2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := st2.Version(); got != st.Version() {
		t.Fatalf("version mismatch: %d vs %d", got, st.Version())
	}
	if engs := st2.ListEngines(); len(engs) != 1 || engs[0].EngineID != "e1" {
		t.Fatalf("engines not restored: %+v", engs)
	}
}

// memPersister is an in-memory Persister for testing the Store<->Persister seam
// without touching disk.
type memPersister struct {
	snap  Snapshot
	saved bool
	saves int
}

func (m *memPersister) Save(s Snapshot)               { m.snap = s; m.saved = true; m.saves++ }
func (m *memPersister) Load() (Snapshot, bool, error) { return m.snap, m.saved, nil }

func TestStorePersistsEveryMutation(t *testing.T) {
	m := &memPersister{}
	st := NewStoreWith(m, nil)
	st.UpsertCluster(Cluster{ClusterID: "c1"})
	st.UpsertEngine(Engine{EngineID: "e1"})
	st.UpsertPolicy(Policy{PolicyID: "p1"})
	if m.saves != 3 {
		t.Fatalf("want 3 saves, got %d", m.saves)
	}
	if len(m.snap.Engines) != 1 || m.snap.Engines[0].EngineID != "e1" {
		t.Fatalf("snapshot missing engine: %+v", m.snap)
	}
}

func TestApplyBootstrapOnceOnly(t *testing.T) {
	enabled := true
	long := 1024
	bs := &Bootstrap{
		Cluster: BootstrapCluster{ClusterID: "gz", Region: "cn-guangzhou", Enabled: &enabled},
		Profiles: []BootstrapProfile{{ModelID: "qwen3.5-4b", Framework: "sglang",
			HashProfile: "sglang-v1-text", BlockSize: 1, HashSeed: "0"}},
		Engines: []BootstrapEngine{{EngineID: "e0", ClusterID: "gz", Framework: "sglang",
			ServedModels: []string{"qwen3.5-4b"}}},
		Policies: []BootstrapPolicy{{PolicyID: "gz-default", ScopeModelID: "qwen3.5-4b",
			LongPromptThresholdTokens: &long, Enabled: &enabled}},
	}

	st := NewStoreWith(nil, nil)
	if !st.ApplyBootstrap(bs) {
		t.Fatal("first ApplyBootstrap should return true")
	}
	if len(st.ListEngines()) != 1 || len(st.ListClusters()) != 1 ||
		len(st.ListModelProfiles()) != 1 || len(st.ListPolicies()) != 1 {
		t.Fatalf("bootstrap did not seed all entities")
	}
	v := st.Version()

	// Second apply must be a no-op (store non-empty) — never clobbers runtime state.
	if st.ApplyBootstrap(bs) {
		t.Fatal("second ApplyBootstrap should return false (store non-empty)")
	}
	if st.Version() != v {
		t.Fatalf("version changed on skipped bootstrap: %d -> %d", v, st.Version())
	}
}

func TestLoadBootstrapRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("cluster:\n  cluster_id: x\n  bogus_field: 1\n"), 0o644)
	if _, err := LoadBootstrap(path); err == nil {
		t.Fatal("expected error on unknown YAML key, got nil")
	}
}
