// Package residency implements the dual-key KV prefix residency index.
//
// Two key spaces:
//
//   - request_key: a deterministic chained hash over block_size-sized token
//     chunks, seeded by a profile namespace. Computed identically (a) at query
//     time from tokenizer output and (b) at ingest time from a BlockStored
//     event's token_ids. This is what request-time prefix scoring matches on.
//   - engine_key: the opaque uint64 hash carried in vLLM/SGLang KV events. Used
//     to process BlockRemoved (which only carries engine hashes) and to resolve
//     parent chaining.
//
// On BlockStored we learn both the engine_keys and the request_keys for the
// same blocks and record an engine_key -> request_key bridge (tail-aligned when
// the counts differ, e.g. mamba-align null-block skipping). On BlockRemoved we
// look up the bridge to drop the right request_key residency.
package residency

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
	"sync"
	"time"
)

// RequestKey is a deterministic prefix-block hash in our own namespace.
type RequestKey uint64

// EngineKey is the engine-reported block hash (vLLM/SGLang uint64).
type EngineKey uint64

// SeedNamespace derives the chain seed for a profile namespace string.
func SeedNamespace(namespace string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	return h.Sum64()
}

// ChunkTokens splits tokens into full block_size chunks, dropping any partial
// trailing block (matching vLLM/llm-d: only full blocks are hashed).
func ChunkTokens(tokens []int32, blockSize int) [][]int32 {
	if blockSize <= 0 {
		return nil
	}
	n := len(tokens) / blockSize
	out := make([][]int32, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, tokens[i*blockSize:(i+1)*blockSize])
	}
	return out
}

// hashBlock chains parent with a token chunk: FNV-64a over
// (parent LE u64 || tokenID LE u32 ...). Deterministic and endian-stable.
func hashBlock(parent uint64, chunk []int32) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], parent)
	_, _ = h.Write(buf[:])
	var tb [4]byte
	for _, t := range chunk {
		binary.LittleEndian.PutUint32(tb[:], uint32(t))
		_, _ = h.Write(tb[:])
	}
	return h.Sum64()
}

// RequestKeysFromTokens computes the chained request_keys for a token sequence
// under a namespace seed. Returns one key per full block, in prefix order.
func RequestKeysFromTokens(seed uint64, tokens []int32, blockSize int) []RequestKey {
	chunks := ChunkTokens(tokens, blockSize)
	keys := make([]RequestKey, 0, len(chunks))
	parent := seed
	for _, c := range chunks {
		parent = hashBlock(parent, c)
		keys = append(keys, RequestKey(parent))
	}
	return keys
}

// RequestKeysFromTokensSeeded is an alias used by event ingest, where the seed
// may be either the namespace seed (prefix start) or a parent block's
// request_key (continuation). The chaining math is identical.
func RequestKeysFromTokensSeeded(parentSeed uint64, tokens []int32, blockSize int) []RequestKey {
	return RequestKeysFromTokens(parentSeed, tokens, blockSize)
}

// residents tracks which (engine,dp,tier) hold a request_key, with last-seen.
type resident struct {
	engineID string
	dpRank   int
	tier     string
}

type residencyEntry struct {
	// holders maps a resident -> last seen unix-nano.
	holders map[resident]int64
}

// Index is the in-memory residency index for a single profile namespace.
type Index struct {
	mu sync.RWMutex

	// request_key -> residency
	byRequestKey map[RequestKey]*residencyEntry
	// (engine_id, engine_key) -> request_key bridge for removal/parent lookup.
	bridge map[engineBridgeKey]RequestKey
	// engine_id -> set of request_keys it holds (for AllBlocksCleared).
	byEngine map[string]map[RequestKey]struct{}

	now func() time.Time
}

type engineBridgeKey struct {
	engineID  string
	engineKey EngineKey
}

// NewIndex creates an empty index. nowFn may be nil.
func NewIndex(nowFn func() time.Time) *Index {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Index{
		byRequestKey: map[RequestKey]*residencyEntry{},
		bridge:       map[engineBridgeKey]RequestKey{},
		byEngine:     map[string]map[RequestKey]struct{}{},
		now:          nowFn,
	}
}

// StoreEvent records a BlockStored: engineKeys and requestKeys are the
// per-block keys for the SAME contiguous blocks, both in prefix order ending at
// the SAME final block. We bridge by tail-aligning both lists: the last engine
// key pairs with the last request key, second-to-last with second-to-last, and
// so on for min(len) pairs. This is correct regardless of which list is longer
// (e.g. mamba-align may skip interior null blocks from engineKeys, and the
// degenerate len-mismatch cases collapse to a safe partial bridge). All request
// keys still record residency (the whole prefix is resident); only the engine
// keys we actually received get bridged for later removal.
func (ix *Index) StoreEvent(engineID string, dpRank int, tier string, engineKeys []EngineKey, requestKeys []RequestKey) {
	if len(requestKeys) == 0 {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ts := ix.now().UnixNano()
	r := resident{engineID: engineID, dpRank: dpRank, tier: tier}

	eng := ix.byEngine[engineID]
	if eng == nil {
		eng = map[RequestKey]struct{}{}
		ix.byEngine[engineID] = eng
	}

	for _, rk := range requestKeys {
		e := ix.byRequestKey[rk]
		if e == nil {
			e = &residencyEntry{holders: map[resident]int64{}}
			ix.byRequestKey[rk] = e
		}
		e.holders[r] = ts
		eng[rk] = struct{}{}
	}

	// Bridge engine keys to request keys, tail-aligned from the end of BOTH
	// lists so the final block of each always pairs together.
	nEng, nReq := len(engineKeys), len(requestKeys)
	k := nEng
	if nReq < k {
		k = nReq
	}
	for i := 0; i < k; i++ {
		ek := engineKeys[nEng-k+i]
		rk := requestKeys[nReq-k+i]
		ix.bridge[engineBridgeKey{engineID, ek}] = rk
	}
}

// RemoveEvent processes a BlockRemoved: resolve engine keys via the bridge and
// drop that resident's hold on the corresponding request keys.
func (ix *Index) RemoveEvent(engineID string, dpRank int, tier string, engineKeys []EngineKey) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	r := resident{engineID: engineID, dpRank: dpRank, tier: tier}
	for _, ek := range engineKeys {
		bk := engineBridgeKey{engineID, ek}
		rk, ok := ix.bridge[bk]
		if !ok {
			continue
		}
		delete(ix.bridge, bk)
		if e := ix.byRequestKey[rk]; e != nil {
			delete(e.holders, r)
			if len(e.holders) == 0 {
				delete(ix.byRequestKey, rk)
			}
		}
		if eng := ix.byEngine[engineID]; eng != nil {
			delete(eng, rk)
		}
	}
}

// ClearEngine drops all residency contributed by an engine (AllBlocksCleared).
// If tier is non-empty, only that tier's holds are cleared.
func (ix *Index) ClearEngine(engineID string, tier string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	eng := ix.byEngine[engineID]
	if eng == nil {
		return
	}
	for rk := range eng {
		e := ix.byRequestKey[rk]
		if e == nil {
			continue
		}
		for res := range e.holders {
			if res.engineID != engineID {
				continue
			}
			if tier != "" && res.tier != tier {
				continue
			}
			delete(e.holders, res)
		}
		if len(e.holders) == 0 {
			delete(ix.byRequestKey, rk)
		}
	}
	if tier == "" {
		// also wipe bridges and engine set
		for bk := range ix.bridge {
			if bk.engineID == engineID {
				delete(ix.bridge, bk)
			}
		}
		delete(ix.byEngine, engineID)
	}
}

// InstanceHit is the per-instance prefix hit breakdown (Mooncake/Dynamo shape).
// Token counts are cumulative-by-tier: gpu is device only; cpu = gpu+host;
// disk = cpu+disk. longest_matched is the contiguous prefix length in tokens.
type InstanceHit struct {
	LongestMatched int            `json:"longest_matched"`
	GPU            int            `json:"gpu"`
	CPU            int            `json:"cpu"`
	Disk           int            `json:"disk"`
	DP             map[string]int `json:"dp"`
}

// QueryResult is the full query response across instances.
type QueryResult struct {
	Instances map[string]*InstanceHit `json:"instances"`
	// LastSeenNano is the newest holder timestamp that contributed (0 if none),
	// used by the caller to judge freshness.
	LastSeenNano int64 `json:"-"`
}

// Query walks the request_keys in prefix order and, per instance, accumulates
// the contiguous prefix hit. An instance's matched prefix is the largest k such
// that the instance holds blocks 0..k-1 contiguously from the start; an
// instance first seen at block i>0 does not count. blockSize converts block
// counts to token counts. Tier breakdown is cumulative: gpu = device tier only,
// cpu = gpu+host, disk = cpu+lower; any residency counts toward disk.
func (ix *Index) Query(requestKeys []RequestKey, blockSize int) *QueryResult {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	res := &QueryResult{Instances: map[string]*InstanceHit{}}
	if len(requestKeys) == 0 {
		return res
	}

	// active is the set of instances still on a contiguous run from block 0.
	// Seeded from block 0's holders; instances appearing later cannot join.
	var active map[string]bool

	for i, rk := range requestKeys {
		entry := ix.byRequestKey[rk]
		// holders of this block, by engineID -> per-tier presence + dp tokens.
		type blockInfo struct {
			tiers map[string]bool
			dp    map[string]int
		}
		holders := map[string]*blockInfo{}
		if entry != nil {
			for r, ts := range entry.holders {
				if ts > res.LastSeenNano {
					res.LastSeenNano = ts
				}
				bi := holders[r.engineID]
				if bi == nil {
					bi = &blockInfo{tiers: map[string]bool{}, dp: map[string]int{}}
					holders[r.engineID] = bi
				}
				bi.tiers[r.tier] = true
				bi.dp[itoa(r.dpRank)] += blockSize
			}
		}

		if i == 0 {
			active = map[string]bool{}
			for eng := range holders {
				active[eng] = true
			}
		} else {
			// Drop any active instance that does not hold this block.
			for eng := range active {
				if _, ok := holders[eng]; !ok {
					delete(active, eng)
				}
			}
		}
		if len(active) == 0 {
			break
		}

		for eng := range active {
			bi := holders[eng]
			ih := res.Instances[eng]
			if ih == nil {
				ih = &InstanceHit{DP: map[string]int{}}
				res.Instances[eng] = ih
			}
			ih.LongestMatched += blockSize
			if bi.tiers[tierGPU] {
				ih.GPU += blockSize
			}
			if bi.tiers[tierGPU] || bi.tiers[tierCPU] {
				ih.CPU += blockSize
			}
			ih.Disk += blockSize // any tier residency is available at >= disk
			for dpRank, tok := range bi.dp {
				ih.DP[dpRank] += tok
			}
		}
	}
	return res
}

const (
	tierGPU  = "gpu"
	tierCPU  = "cpu"
	tierDisk = "disk"
)

// Exported tier names for use by ingest adapters.
const (
	TierGPU  = tierGPU
	TierCPU  = tierCPU
	TierDisk = tierDisk
)

// Stats returns basic index sizes for observability.
func (ix *Index) Stats() (numRequestKeys, numBridges, numEngines int) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.byRequestKey), len(ix.bridge), len(ix.byEngine)
}

// LookupBridge resolves an engine block hash to its request_key, if known.
// Used to chain request_key derivation across BlockStored events that carry a
// parent_block_hash referencing an earlier (already-ingested) block.
func (ix *Index) LookupBridge(engineID string, ek EngineKey) (RequestKey, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	rk, ok := ix.bridge[engineBridgeKey{engineID, ek}]
	return rk, ok
}

// sortedEngineIDs is a small helper for deterministic iteration in tests.
func (ix *Index) sortedEngineIDs() []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.byEngine))
	for e := range ix.byEngine {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
