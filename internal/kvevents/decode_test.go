package kvevents

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

// TestDecodeGoldenVLLMBatch decodes a real captured vLLM event batch and
// asserts the hybrid group structure (mamba degenerate + full_attention real).
func TestDecodeGoldenVLLMBatch(t *testing.T) {
	raw, err := os.ReadFile("testdata/vllm_batch.hex")
	if err != nil {
		t.Skip("golden fixture missing")
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	var payload []any
	if err := msgpack.Unmarshal(b, &payload); err != nil {
		t.Fatalf("msgpack: %v", err)
	}
	batch, err := DecodeBatch(7, payload)
	if err != nil {
		t.Fatalf("DecodeBatch: %v", err)
	}
	if batch.DPRank != 0 {
		t.Fatalf("expected dp_rank 0, got %d", batch.DPRank)
	}

	var stored, removed int
	var attnStored *Event
	for i := range batch.Events {
		ev := &batch.Events[i]
		switch ev.Kind {
		case KindBlockStored:
			stored++
			if ev.SpecKind == "full_attention" {
				attnStored = ev
			}
			if ev.BlockSize != 528 {
				t.Fatalf("expected block_size 528, got %d", ev.BlockSize)
			}
		case KindBlockRemoved:
			removed++
		}
	}
	if stored == 0 || removed == 0 {
		t.Fatalf("expected both stored (%d) and removed (%d) events", stored, removed)
	}
	if attnStored == nil {
		t.Fatalf("expected a full_attention BlockStored event")
	}
	// full_attention block over 1056 tokens at block 528 => 2 block hashes.
	if len(attnStored.BlockHashes) != 2 {
		t.Fatalf("full_attention should have 2 block hashes, got %d", len(attnStored.BlockHashes))
	}
	if len(attnStored.TokenIDs) != 1056 {
		t.Fatalf("expected 1056 token ids, got %d", len(attnStored.TokenIDs))
	}
	// Hashes must be large uint64 values (low-64-bits of digest).
	for _, h := range attnStored.BlockHashes {
		if h == 0 {
			t.Fatalf("unexpected zero block hash")
		}
	}
}

func TestToUint64Forms(t *testing.T) {
	cases := []struct {
		in   any
		want uint64
	}{
		{uint64(12345), 12345},
		{int64(-1), ^uint64(0)},
		{int(42), 42},
		{[]byte{0, 0, 0, 0, 0, 0, 0, 5}, 5},
		{[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}, 0x0203040506070809}, // last 8 bytes
	}
	for _, c := range cases {
		got, ok := toUint64(c.in)
		if !ok || got != c.want {
			t.Fatalf("toUint64(%v)=%d ok=%v want %d", c.in, got, ok, c.want)
		}
	}
}

func TestDecodeShortArrayTolerated(t *testing.T) {
	// BlockStored with only the minimum 5 fields (tag+hashes+parent+tokens+bs).
	fields := []any{"BlockStored", []any{uint64(99)}, nil, []any{int64(1), int64(2)}, int64(2)}
	ev, ok := decodeEvent(KindBlockStored, fields)
	if !ok {
		t.Fatal("should decode minimal BlockStored")
	}
	if ev.BlockSize != 2 || len(ev.BlockHashes) != 1 || ev.HasParent {
		t.Fatalf("bad minimal decode: %+v", ev)
	}
}

func TestDecodeVLLMOptionalFields(t *testing.T) {
	fields := []any{
		"BlockStored",
		[]any{uint64(99)},
		uint64(11),
		[]any{int64(1), int64(2), int64(3), int64(4)},
		int64(4),
		int64(7),
		"GPU",
		"adapter-a",
		[]any{[]any{"image-hash", int64(0)}, nil},
		int64(3),
		"sliding_window",
		int64(4096),
	}
	ev, ok := decodeEvent(KindBlockStored, fields)
	if !ok {
		t.Fatal("should decode full vLLM BlockStored")
	}
	if !ev.HasParent || ev.ParentHash != 11 {
		t.Fatalf("parent not decoded: %+v", ev)
	}
	if !ev.HasLoraID || ev.LoraID != 7 || ev.LoraName != "adapter-a" {
		t.Fatalf("lora not decoded: %+v", ev)
	}
	if !ev.HasExtraKeys || ev.ExtraKeyCount != 1 || len(ev.ExtraKeys) != 1 {
		t.Fatalf("extra keys not decoded: %+v", ev)
	}
	if ev.GroupIdx != 3 || ev.SpecKind != "sliding_window" || !ev.HasSlidingWindow || ev.SlidingWindow != 4096 {
		t.Fatalf("group/spec metadata not decoded: %+v", ev)
	}
}

func TestDecodeNestedTokenIDsFlagged(t *testing.T) {
	fields := []any{
		"BlockStored",
		[]any{uint64(99)},
		nil,
		[]any{[]any{int64(1), int64(2)}, []any{int64(2), int64(3)}},
		int64(2),
	}
	ev, ok := decodeEvent(KindBlockStored, fields)
	if !ok {
		t.Fatal("should decode nested-token BlockStored")
	}
	if !ev.HasNestedTokenIDs {
		t.Fatalf("nested token ids should be flagged: %+v", ev)
	}
	if len(ev.TokenIDs) != 0 {
		t.Fatalf("nested token ids should not be flattened into request keys: %+v", ev.TokenIDs)
	}
}
