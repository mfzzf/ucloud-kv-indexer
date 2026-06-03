// Package kvevents subscribes to vLLM/SGLang KV-cache ZMQ event streams and
// feeds them into the residency index. The wire format (verified against a live
// vLLM) is a 3-frame ZMQ PUB message:
//
//	[ topic_bytes, seq(8-byte big-endian), msgpack_payload ]
//
// The msgpack payload is a msgspec array_like batch:
//
//	[ ts(float), events([]event), data_parallel_rank(int|null) ]
//
// Each event is a tag-prefixed array. BlockStored:
//
//	[ "BlockStored", block_hashes, parent_block_hash, token_ids, block_size,
//	  lora_id, medium, lora_name, extra_keys, group_idx, kv_cache_spec_kind,
//	  sliding_window ]
//
// BlockRemoved: [ "BlockRemoved", block_hashes, medium, group_idx ]
// AllBlocksCleared: [ "AllBlocksCleared" ]
//
// Trailing optional fields may be absent (msgspec omit_defaults); we tolerate
// short arrays via positional access with bounds checks.
package kvevents

import "fmt"

// EventKind is the decoded event tag.
type EventKind string

const (
	KindBlockStored      EventKind = "BlockStored"
	KindBlockRemoved     EventKind = "BlockRemoved"
	KindAllBlocksCleared EventKind = "AllBlocksCleared"
)

// Event is the decoded, framework-neutral KV event.
type Event struct {
	Kind              EventKind
	BlockHashes       []uint64
	ParentHash        uint64
	HasParent         bool
	TokenIDs          []int32
	HasNestedTokenIDs bool
	BlockSize         int
	Medium            string // GPU/CPU/DISK (as reported); lowercased by ingest
	LoraID            int
	HasLoraID         bool
	LoraName          string
	ExtraKeys         []string
	ExtraKeyCount     int
	GroupIdx          int
	SpecKind          string
	SlidingWindow     int
	HasSlidingWindow  bool
	HasExtraKeys      bool // true if any non-nil extra_keys present (feature flag)
}

// Batch is a decoded event batch with its sequence number and DP rank.
type Batch struct {
	Seq    uint64
	TS     float64
	DPRank int
	Events []Event
}

// toUint64 coerces an msgpack-decoded numeric or byte-slice hash into uint64,
// matching llm-d's getHashAsUint64 (handles uint64, int64, and []byte tails).
func toUint64(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint64:
		return x, true
	case int64:
		return uint64(x), true
	case int8:
		return uint64(uint8(x)), true
	case uint8:
		return uint64(x), true
	case int16:
		return uint64(uint16(x)), true
	case uint16:
		return uint64(x), true
	case int32:
		return uint64(uint32(x)), true
	case uint32:
		return uint64(x), true
	case int:
		return uint64(x), true
	case []byte:
		var out uint64
		n := len(x)
		start := 0
		if n > 8 {
			start = n - 8 // take last 8 bytes, big-endian
		}
		for i := start; i < n; i++ {
			out = (out << 8) | uint64(x[i])
		}
		return out, true
	default:
		return 0, false
	}
}

func toInt(v any) (int, bool) {
	u, ok := toUint64(v)
	if !ok {
		return 0, false
	}
	return int(int64(u)), true
}

func toInt32Slice(v any) []int32 {
	out, _ := toInt32SliceInfo(v)
	return out
}

func toInt32SliceInfo(v any) ([]int32, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int32, 0, len(arr))
	nested := false
	for _, e := range arr {
		if i, ok := toInt(e); ok {
			out = append(out, int32(i))
			continue
		}
		if _, ok := e.([]any); ok {
			nested = true
		}
	}
	return out, nested
}

func toUint64Slice(v any) []uint64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]uint64, 0, len(arr))
	for _, e := range arr {
		if u, ok := toUint64(e); ok {
			out = append(out, u)
		}
	}
	return out
}

func fieldAt(fields []any, i int) any {
	if i < 0 || i >= len(fields) {
		return nil
	}
	return fields[i]
}

// DecodeBatch decodes a msgpack-decoded payload (already unmarshaled into
// []any) plus a sequence number into a Batch. The payload is
// [ts, events, dp_rank?].
func DecodeBatch(seq uint64, payload []any) (*Batch, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("batch payload too short: %d", len(payload))
	}
	b := &Batch{Seq: seq}
	if ts, ok := payload[0].(float64); ok {
		b.TS = ts
	}
	if dp, ok := toInt(fieldAt(payload, 2)); ok {
		b.DPRank = dp
	}
	rawEvents, ok := payload[1].([]any)
	if !ok {
		return nil, fmt.Errorf("batch events not an array")
	}
	for _, re := range rawEvents {
		fields, ok := re.([]any)
		if !ok || len(fields) == 0 {
			continue
		}
		tag, _ := fields[0].(string)
		ev, ok := decodeEvent(EventKind(tag), fields)
		if ok {
			b.Events = append(b.Events, ev)
		}
	}
	return b, nil
}

// decodeEvent decodes one tag-prefixed event array.
func decodeEvent(kind EventKind, fields []any) (Event, bool) {
	switch kind {
	case KindBlockStored:
		// [tag, block_hashes, parent, token_ids, block_size, lora_id, medium,
		//  lora_name, extra_keys, group_idx, spec_kind, sliding_window]
		if len(fields) < 5 {
			return Event{}, false
		}
		ev := Event{Kind: KindBlockStored}
		ev.BlockHashes = toUint64Slice(fieldAt(fields, 1))
		if p := fieldAt(fields, 2); p != nil {
			if u, ok := toUint64(p); ok {
				ev.ParentHash = u
				ev.HasParent = true
			}
		}
		ev.TokenIDs, ev.HasNestedTokenIDs = toInt32SliceInfo(fieldAt(fields, 3))
		if bs, ok := toInt(fieldAt(fields, 4)); ok {
			ev.BlockSize = bs
		}
		if li := fieldAt(fields, 5); li != nil {
			if id, ok := toInt(li); ok {
				ev.LoraID = id
				ev.HasLoraID = true
			}
		}
		if m, ok := fieldAt(fields, 6).(string); ok {
			ev.Medium = m
		}
		if ln, ok := fieldAt(fields, 7).(string); ok {
			ev.LoraName = ln
		}
		ev.HasExtraKeys = hasNonNilExtraKeys(fieldAt(fields, 8))
		ev.ExtraKeys, ev.ExtraKeyCount = extraKeyStrings(fieldAt(fields, 8))
		if gi, ok := toInt(fieldAt(fields, 9)); ok {
			ev.GroupIdx = gi
		}
		if sk, ok := fieldAt(fields, 10).(string); ok {
			ev.SpecKind = sk
		}
		if sw, ok := toInt(fieldAt(fields, 11)); ok {
			ev.SlidingWindow = sw
			ev.HasSlidingWindow = true
		}
		return ev, true
	case KindBlockRemoved:
		// [tag, block_hashes, medium, group_idx]
		if len(fields) < 2 {
			return Event{}, false
		}
		ev := Event{Kind: KindBlockRemoved}
		ev.BlockHashes = toUint64Slice(fieldAt(fields, 1))
		if m, ok := fieldAt(fields, 2).(string); ok {
			ev.Medium = m
		}
		if gi, ok := toInt(fieldAt(fields, 3)); ok {
			ev.GroupIdx = gi
		}
		return ev, true
	case KindAllBlocksCleared:
		return Event{Kind: KindAllBlocksCleared}, true
	default:
		return Event{}, false
	}
}

// hasNonNilExtraKeys reports whether extra_keys carries any real feature data
// (used to flag LoRA/MM/cache_salt requests the text-only profile can't hash).
func hasNonNilExtraKeys(v any) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if e != nil {
			return true
		}
	}
	return false
}

func extraKeyStrings(v any) ([]string, int) {
	arr, ok := v.([]any)
	if !ok {
		return nil, 0
	}
	out := make([]string, 0, len(arr))
	count := 0
	for _, e := range arr {
		if e == nil {
			continue
		}
		count++
		if len(out) < 16 {
			out = append(out, fmt.Sprint(e))
		}
	}
	return out, count
}
