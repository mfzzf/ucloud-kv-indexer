package gateway

import (
	"path/filepath"
	"testing"
)

func TestConnStoreSeedOnceAndCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conns.db")

	s, err := OpenConnStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Seed once when empty.
	seed := []Connection{
		{ID: "a-0", Cluster: "a", URL: "http://10.0.0.1:8090", Token: "tok-a", Enabled: true},
		{ID: "b-0", Cluster: "b", URL: "http://10.0.1.1:8090", Token: "tok-b", Enabled: false},
	}
	ok, err := s.SeedIfEmpty(seed)
	if err != nil || !ok {
		t.Fatalf("seed: ok=%v err=%v", ok, err)
	}
	if s.Count() != 2 {
		t.Fatalf("count=%d want 2", s.Count())
	}
	// Backends returns only ENABLED connections, with token attached.
	bs := s.Backends()
	if len(bs) != 1 || bs[0].ID != "a-0" || bs[0].Token != "tok-a" {
		t.Fatalf("backends=%+v want only enabled a-0 with token", bs)
	}

	// Seed again is a no-op (already populated).
	ok, err = s.SeedIfEmpty([]Connection{{ID: "c-0", Cluster: "c", URL: "http://x", Enabled: true}})
	if err != nil || ok {
		t.Fatalf("re-seed should be no-op: ok=%v err=%v", ok, err)
	}
	if s.Count() != 2 {
		t.Fatalf("re-seed changed count to %d", s.Count())
	}

	// Enable b-0 → now federated.
	if err := s.Upsert(Connection{ID: "b-0", Cluster: "b", URL: "http://10.0.1.1:8090", Token: "tok-b", Enabled: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(s.Backends()) != 2 {
		t.Fatalf("after enabling b-0, backends=%d want 2", len(s.Backends()))
	}

	// Delete a-0.
	if err := s.Delete("a-0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("after delete count=%d want 1", s.Count())
	}

	// Persistence across reopen.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s2, err := OpenConnStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Count() != 1 {
		t.Fatalf("reopened count=%d want 1", s2.Count())
	}
	bs2 := s2.Backends()
	if len(bs2) != 1 || bs2[0].ID != "b-0" || bs2[0].Token != "tok-b" {
		t.Fatalf("reopened backends=%+v want b-0 with token", bs2)
	}
}

func TestConnStoreRefreshesAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	a, err := OpenConnStore(path)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()
	b, err := OpenConnStore(path)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	defer b.Close()

	if err := a.Upsert(Connection{ID: "idx-0", Cluster: "th-gb200", URL: "http://10.0.0.1:8090", Token: "tok", Enabled: true}); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	got := b.Backends()
	if len(got) != 1 || got[0].ID != "idx-0" || got[0].Token != "tok" {
		t.Fatalf("store b did not observe store a upsert: %+v", got)
	}

	if err := a.Delete("idx-0"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if got := b.Backends(); len(got) != 0 {
		t.Fatalf("store b did not observe store a delete: %+v", got)
	}
}

// TestConnStoreValidation rejects rows missing required fields or with a
// malformed URL (no scheme/host).
func TestConnStoreValidation(t *testing.T) {
	s, err := OpenConnStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Upsert(Connection{ID: "", Cluster: "a", URL: "http://x"}); err == nil {
		t.Fatal("expected error for missing id")
	}
	if err := s.Upsert(Connection{ID: "x", Cluster: "", URL: "http://x"}); err == nil {
		t.Fatal("expected error for missing cluster")
	}
	// Malformed URLs must be rejected at registration, not deferred to a 502.
	for _, bad := range []string{"10.0.0.1:8090", "ftp://x", "not a url", "/just/a/path"} {
		if err := s.Upsert(Connection{ID: "x", Cluster: "a", URL: bad, Enabled: true}); err == nil {
			t.Fatalf("expected error for malformed url %q", bad)
		}
	}
	// A well-formed URL is accepted.
	if err := s.Upsert(Connection{ID: "x", Cluster: "a", URL: "http://10.0.0.1:8090", Enabled: true}); err != nil {
		t.Fatalf("valid url rejected: %v", err)
	}
}
