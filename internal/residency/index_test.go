package residency

import "testing"

func toks(start, n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = int32(start + i)
	}
	return out
}

func TestRequestKeysDeterministicAndChained(t *testing.T) {
	seed := SeedNamespace("qwen/v1/vllm-v1-text/4")
	a := RequestKeysFromTokens(seed, toks(0, 16), 4)
	b := RequestKeysFromTokens(seed, toks(0, 16), 4)
	if len(a) != 4 {
		t.Fatalf("want 4 keys, got %d", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
	// A shared prefix produces identical leading keys.
	c := RequestKeysFromTokens(seed, append(toks(0, 8), toks(100, 8)...), 4)
	if a[0] != c[0] || a[1] != c[1] {
		t.Fatalf("shared 2-block prefix should match: %v vs %v", a[:2], c[:2])
	}
	if a[2] == c[2] {
		t.Fatalf("divergent 3rd block should differ")
	}
	// Different seed (namespace) => different keys (isolation).
	other := RequestKeysFromTokens(SeedNamespace("qwen/v2/vllm-v1-text/4"), toks(0, 16), 4)
	if a[0] == other[0] {
		t.Fatalf("different namespace must isolate keys")
	}
}

func TestPartialBlockDropped(t *testing.T) {
	seed := SeedNamespace("ns")
	// 10 tokens, block 4 => 2 full blocks, partial dropped.
	keys := RequestKeysFromTokens(seed, toks(0, 10), 4)
	if len(keys) != 2 {
		t.Fatalf("want 2 keys (partial dropped), got %d", len(keys))
	}
}

func TestStoreQueryContiguousPrefix(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	full := toks(0, 16) // 4 blocks
	keys := RequestKeysFromTokens(seed, full, bs)

	// Engine w0 stores all 4 blocks on GPU.
	ek := []EngineKey{1, 2, 3, 4}
	ix.StoreEvent("w0", 0, "gpu", ek, keys)

	res := ix.Query(keys, bs)
	ih := res.Instances["w0"]
	if ih == nil || ih.LongestMatched != 16 {
		t.Fatalf("w0 should match all 16 tokens, got %+v", ih)
	}
	if ih.GPU != 16 || ih.CPU != 16 || ih.Disk != 16 {
		t.Fatalf("tier counts wrong: %+v", ih)
	}
	if ih.DP["0"] != 16 {
		t.Fatalf("dp[0] should be 16, got %d", ih.DP["0"])
	}

	// Query a longer sequence sharing only first 2 blocks => match 8.
	longer := append(toks(0, 8), toks(500, 8)...)
	lk := RequestKeysFromTokens(seed, longer, bs)
	res2 := ix.Query(lk, bs)
	if got := res2.Instances["w0"].LongestMatched; got != 8 {
		t.Fatalf("shared 2-block prefix => 8 tokens, got %d", got)
	}
}

func TestContiguityGapBreaksRun(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 16), bs) // 4 blocks

	// Store blocks 0,1,3 but NOT 2 (gap) on w0.
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{10}, keys[0:1])
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{11}, keys[1:2])
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{13}, keys[3:4])

	res := ix.Query(keys, bs)
	// Contiguous prefix is only blocks 0,1 => 8 tokens (block 2 missing).
	if got := res.Instances["w0"].LongestMatched; got != 8 {
		t.Fatalf("gap at block 2 should cap match at 8, got %d", got)
	}
}

func TestRemoveEventDropsResidency(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 8), bs) // 2 blocks
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{100, 101}, keys)

	if got := ix.Query(keys, bs).Instances["w0"].LongestMatched; got != 8 {
		t.Fatalf("pre-remove match should be 8, got %d", got)
	}
	// Remove engine key 100 (block 0) => prefix match drops to 0.
	ix.RemoveEvent("w0", 0, "gpu", []EngineKey{100})
	res := ix.Query(keys, bs)
	if ih := res.Instances["w0"]; ih != nil && ih.LongestMatched != 0 {
		t.Fatalf("after removing block 0, match should be 0, got %+v", ih)
	}
}

func TestAllBlocksClearedWipesEngine(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 8), bs)
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{1, 2}, keys)
	ix.StoreEvent("w1", 0, "gpu", []EngineKey{1, 2}, keys)

	ix.ClearEngine("w0", "")
	res := ix.Query(keys, bs)
	if ih := res.Instances["w0"]; ih != nil && ih.LongestMatched != 0 {
		t.Fatalf("w0 should be cleared, got %+v", ih)
	}
	if res.Instances["w1"] == nil || res.Instances["w1"].LongestMatched != 8 {
		t.Fatalf("w1 should still match 8")
	}
	nrk, nb, ne := ix.Stats()
	_ = nrk
	_ = nb
	if ne != 1 {
		t.Fatalf("only w1 engine should remain, got %d", ne)
	}
}

func TestTailAlignBridgeMamba(t *testing.T) {
	// Simulate mamba-align: 4 token blocks but only 2 engine hashes for the
	// last 2 blocks (interior nulls skipped). Removal of the engine keys must
	// drop the correct (tail) request keys.
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 16), bs) // 4 request keys
	// Only 2 engine keys provided, tail-aligned to keys[2], keys[3].
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{900, 901}, keys)
	res := ix.Query(keys, bs)
	if got := res.Instances["w0"].LongestMatched; got != 16 {
		t.Fatalf("all request keys should be resident, got %d", got)
	}
	// Removing engine key 901 should drop the LAST block (keys[3]).
	ix.RemoveEvent("w0", 0, "gpu", []EngineKey{901})
	res2 := ix.Query(keys, bs)
	if got := res2.Instances["w0"].LongestMatched; got != 12 {
		t.Fatalf("removing tail engine key should leave 12 tokens, got %d", got)
	}
}

func TestTailAlignMoreEngineThanRequestKeys(t *testing.T) {
	// Defensive inverse case: more engine keys than request keys. Both lists
	// must tail-align so the LAST engine key pairs with the LAST request key.
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 8), bs) // 2 request keys
	// 4 engine keys; only the last 2 should bridge (to keys[0], keys[1]).
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{700, 701, 702, 703}, keys)
	if got := ix.Query(keys, bs).Instances["w0"].LongestMatched; got != 8 {
		t.Fatalf("residency should cover both request keys, got %d", got)
	}
	// Removing the LAST engine key (703) must drop the LAST request key (keys[1]).
	ix.RemoveEvent("w0", 0, "gpu", []EngineKey{703})
	if got := ix.Query(keys, bs).Instances["w0"].LongestMatched; got != 4 {
		t.Fatalf("removing tail engine key 703 should leave 4 tokens, got %d", got)
	}
	// Engine keys 700/701 were never bridged (head, beyond min-length); removing
	// them is a harmless no-op.
	ix.RemoveEvent("w0", 0, "gpu", []EngineKey{700, 701})
	if got := ix.Query(keys, bs).Instances["w0"].LongestMatched; got != 4 {
		t.Fatalf("removing unbridged head keys must not change residency, got %d", got)
	}
}

func TestTierBreakdown(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 8), bs)
	// Block 0 on gpu, block 1 only on cpu.
	ix.StoreEvent("w0", 0, "gpu", []EngineKey{1}, keys[0:1])
	ix.StoreEvent("w0", 0, "cpu", []EngineKey{2}, keys[1:2])
	res := ix.Query(keys, bs)
	ih := res.Instances["w0"]
	if ih.LongestMatched != 8 {
		t.Fatalf("match 8, got %d", ih.LongestMatched)
	}
	if ih.GPU != 4 {
		t.Fatalf("gpu should be 4 (block0 only), got %d", ih.GPU)
	}
	if ih.CPU != 8 {
		t.Fatalf("cpu cumulative should be 8, got %d", ih.CPU)
	}
	if ih.Disk != 8 {
		t.Fatalf("disk cumulative should be 8, got %d", ih.Disk)
	}
}

func TestStoreEventByEngineKeysAddsLowerTier(t *testing.T) {
	ix := NewIndex(nil)
	seed := SeedNamespace("ns")
	bs := 4
	keys := RequestKeysFromTokens(seed, toks(0, 8), bs)
	engKeys := []EngineKey{1, 2}

	ix.StoreEvent("w0", 0, "gpu", engKeys, keys)
	resolved, ok := ix.StoreEventByEngineKeys("w0", 0, "cpu", engKeys)
	if !ok {
		t.Fatalf("lower-tier store should resolve existing engine keys")
	}
	if len(resolved) != len(keys) || resolved[0] != keys[0] || resolved[1] != keys[1] {
		t.Fatalf("resolved request keys mismatch: got %v want %v", resolved, keys)
	}

	// Removing the GPU tier must not destroy the bridge needed to remove CPU.
	ix.RemoveEvent("w0", 0, "gpu", []EngineKey{1})
	res := ix.Query(keys, bs)
	ih := res.Instances["w0"]
	if ih == nil || ih.LongestMatched != 8 || ih.CPU != 8 {
		t.Fatalf("CPU lower tier should still cover the prefix, got %+v", ih)
	}

	ix.RemoveEvent("w0", 0, "cpu", []EngineKey{1})
	res = ix.Query(keys, bs)
	ih = res.Instances["w0"]
	if ih != nil && ih.LongestMatched != 0 {
		t.Fatalf("removing CPU block 0 should break contiguous prefix to 0, got %+v", ih)
	}
}
