package gateway

import "testing"

func TestConnStoreSeedOnceAndCRUD(t *testing.T) {
	s := NewMemoryStore()

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

	list := s.List()
	if len(list) != 1 || list[0].ID != "b-0" || list[0].Token != "tok-b" {
		t.Fatalf("list=%+v want b-0 with token", list)
	}
}

func TestConnStoreRefreshesAcrossInstances(t *testing.T) {
	t.Skip("cross-process refresh is covered by the optional Mongo integration test")
}

// TestConnStoreValidation rejects rows missing required fields or with a
// malformed URL (no scheme/host).
func TestConnStoreValidation(t *testing.T) {
	s := NewMemoryStore()
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
