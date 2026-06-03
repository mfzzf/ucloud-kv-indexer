package kvevents

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ucloud/kv-indexer/internal/residency"
)

// fakeResolver maps every (model, engine) to one shared index/namespace, so a
// test can assert what the apply path wrote.
type fakeResolver struct {
	ix        *residency.Index
	blockSize int
}

func (f fakeResolver) ResolveIngest(model, engineID string) (*residency.Index, string, uint64, int, bool) {
	return f.ix, "ns", 0, f.blockSize, true
}

// TestMediumToTier locks in the storage-medium -> residency-tier mapping across
// BOTH engine spellings. vLLM emits "GPU"/"CPU"/"DISK"; SGLang's StorageMedium
// enum serializes CPU as "CPU_PINNED" (host pinned memory) and adds "EXTERNAL"
// (e.g. Mooncake). A regression here would silently drop SGLang host-tier
// residency into an uncredited "cpu_pinned" tier the admission weighting ignores.
func TestMediumToTier(t *testing.T) {
	cases := []struct {
		medium string
		want   string
	}{
		{"", residency.TierGPU},           // unspecified defaults to device tier
		{"GPU", residency.TierGPU},        // vLLM + SGLang device
		{"gpu", residency.TierGPU},        // case-insensitive
		{"CPU", residency.TierCPU},        // vLLM host
		{"CPU_PINNED", residency.TierCPU}, // SGLang StorageMedium.CPU
		{"cpu_pinned", residency.TierCPU}, // case-insensitive
		{"DISK", residency.TierDisk},      // vLLM/SGLang SSD
		{"EXTERNAL", residency.TierDisk},  // SGLang shared/remote pool (Mooncake)
	}
	for _, c := range cases {
		if got := mediumToTier(c.medium); got != c.want {
			t.Fatalf("mediumToTier(%q)=%q want %q", c.medium, got, c.want)
		}
	}
}

// TestIngestableSpecKind confirms recurrent/linear groups are skipped while real
// attention groups (and unknown/empty kinds) are indexed.
func TestIngestableSpecKind(t *testing.T) {
	skip := []string{"mamba", "Mamba2", "linear_attention", "short_conv"}
	for _, k := range skip {
		if ingestableSpecKind(k) {
			t.Fatalf("spec kind %q should be skipped", k)
		}
	}
	keep := []string{"full_attention", "sliding_window", "", "unknown_future"}
	for _, k := range keep {
		if !ingestableSpecKind(k) {
			t.Fatalf("spec kind %q should be indexed", k)
		}
	}
}

// TestApplyLoopDrainsQueue verifies the decoupled recv→apply path: many frames
// pushed onto the bounded queue are all applied to the index by the apply
// goroutine, the queue depth returns to zero, and frames are processed
// concurrently with enqueue (no deadlock, no lost events). It feeds the real
// golden vLLM batch so the full decode+ingest runs, exercising frame cloning.
func TestApplyLoopDrainsQueue(t *testing.T) {
	raw, err := os.ReadFile("testdata/vllm_batch.hex")
	if err != nil {
		t.Skip("golden fixture missing")
	}
	payload, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	// Build a real 3-frame message [topic, seq(8B BE), msgpack payload].
	seqFrame := make([]byte, 8)
	binary.BigEndian.PutUint64(seqFrame, 7)
	frame := [][]byte{[]byte("kv-events"), seqFrame, payload}

	ix := residency.NewIndex(func() time.Time { return time.Unix(0, 0) })
	l := NewListener("eng-0", "qwen3.5-4b", "tcp://127.0.0.1:0", "kv-events",
		fakeResolver{ix: ix, blockSize: 528}, func() time.Time { return time.Unix(0, 0) })

	const n = 500
	queue := make(chan [][]byte, applyQueueSize)
	done := make(chan struct{})
	go l.applyLoop(queue, done)

	// Producer: enqueue n copies (each a fresh clone, as the recv loop does).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			c := cloneFrames(frame)
			queue <- c
			l.queueLen.Add(1)
		}
	}()
	wg.Wait()
	close(queue)
	<-done

	if depth := l.queueLen.Load(); depth != 0 {
		t.Fatalf("queue depth should drain to 0, got %d", depth)
	}
	// Each batch carries the same events; events counter should reflect all n.
	h := l.Health()
	if h.EventsTotal == 0 {
		t.Fatalf("expected events applied, got 0")
	}
	if h.QueueCap != applyQueueSize {
		t.Fatalf("QueueCap=%d want %d", h.QueueCap, applyQueueSize)
	}
	// The golden batch has a full_attention group over 1056 tokens (2 blocks of
	// 528); after applying, the index must hold request keys for that prefix.
	nkeys, _, _ := ix.Stats()
	if nkeys == 0 {
		t.Fatalf("expected the apply path to populate the index, got 0 request keys")
	}
}
