package config

import (
	"path/filepath"
	"testing"
)

func TestSQLitePersisterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")

	p, err := NewSQLitePersister(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer p.Close()

	// Fresh DB => ok=false, no error.
	if _, ok, err := p.Load(); err != nil || ok {
		t.Fatalf("fresh load: ok=%v err=%v", ok, err)
	}

	st := NewStoreWith(p, nil)
	st.UpsertCluster(Cluster{ClusterID: "h20-1", Region: "h20", Enabled: true})
	st.UpsertModelProfile(ModelProfile{ModelID: "qwen3", Framework: FrameworkSGLang,
		HashProfile: "sglang-v1-text", BlockSize: 16, HashSeed: "0"})
	st.UpsertEngine(Engine{EngineID: "e1", ClusterID: "h20-1", Framework: FrameworkSGLang,
		ServedModels: []string{"qwen3"}})
	enabled := true
	st.UpsertPolicy(Policy{RuleID: "p1",
		Conditions: []RuleCondition{{Field: ConditionFieldModelID, Op: ConditionOpEq, Value: "qwen3"}},
		Action:     RuleAction{Type: ActionAccept}, Enabled: &enabled})

	wantVer := st.Version()

	// Reopen the SAME db file into a fresh store; everything must round-trip.
	p2, err := NewSQLitePersister(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer p2.Close()
	st2 := NewStoreWith(p2, nil)
	if err := st2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got := st2.Version(); got != wantVer {
		t.Fatalf("version mismatch: %d vs %d", got, wantVer)
	}
	if cs := st2.ListClusters(); len(cs) != 1 || cs[0].ClusterID != "h20-1" {
		t.Fatalf("clusters not restored: %+v", cs)
	}
	if engs := st2.ListEngines(); len(engs) != 1 || engs[0].EngineID != "e1" ||
		engs[0].Framework != FrameworkSGLang {
		t.Fatalf("engines not restored: %+v", engs)
	}
	pr, ok := st2.ResolveProfile("qwen3")
	if !ok || pr.BlockSize != 16 {
		t.Fatalf("profile not restored: %+v ok=%v", pr, ok)
	}
	if ps := st2.ListPolicies(); len(ps) != 1 || ps[0].RuleID != "p1" {
		t.Fatalf("policies not restored: %+v", ps)
	}
	// Audit trail should survive too (4 mutations above).
	if a := st2.Audit(); len(a) != len(st.Audit()) || len(a) == 0 {
		t.Fatalf("audit not restored: got %d want %d", len(a), len(st.Audit()))
	}
}

func TestSQLitePersisterReplacesRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.db")
	p, err := NewSQLitePersister(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer p.Close()

	st := NewStoreWith(p, nil)
	st.UpsertEngine(Engine{EngineID: "e1"})
	st.UpsertEngine(Engine{EngineID: "e2"})
	st.RemoveEngine("e1") // Save must delete-all + reinsert, so e1 is gone on disk.

	p2, _ := NewSQLitePersister(path)
	defer p2.Close()
	st2 := NewStoreWith(p2, nil)
	if err := st2.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	engs := st2.ListEngines()
	if len(engs) != 1 || engs[0].EngineID != "e2" {
		t.Fatalf("stale row not replaced: %+v", engs)
	}
}
