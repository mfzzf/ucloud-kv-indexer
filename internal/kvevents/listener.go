package kvevents

import (
	"context"
	"encoding/binary"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-zeromq/zmq4"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/ucloud/kv-indexer/internal/residency"
)

// IndexResolver maps a (model, engine) to the residency index + namespace to
// write into. It also reports the block size to chunk token_ids with and the
// seed for request_key derivation. Returns ok=false to drop the event (e.g.
// model not configured).
type IndexResolver interface {
	ResolveIngest(model, engineID string) (ix *residency.Index, namespace string, seed uint64, blockSize int, ok bool)
}

// StreamHealth is observable per-engine listener state.
type StreamHealth struct {
	EngineID      string `json:"engine_id"`
	Endpoint      string `json:"endpoint"`
	Topic         string `json:"topic"`
	Connected     bool   `json:"connected"`
	LastSeq       int64  `json:"last_seq"`
	LastEventUnix int64  `json:"last_event_unix"`
	EventsTotal   int64  `json:"events_total"`
	GapsTotal     int64  `json:"gaps_total"`
	SkippedTotal  int64  `json:"skipped_total"`
	DecodeErrors  int64  `json:"decode_errors"`
	// QueueDepth/QueueCap expose the recv→apply backpressure buffer. A depth
	// persistently near cap means the index-apply step (decode + lock + store)
	// is not keeping up with this engine's event rate — the signal to shard the
	// cluster across more kvindexer processes (see docs/scaling.md).
	QueueDepth int    `json:"queue_depth"`
	QueueCap   int    `json:"queue_cap"`
	LastError  string `json:"last_error,omitempty"`
}

// applyQueueSize bounds the recv→apply hand-off buffer (per engine). The pure-Go
// zmq4 SUB socket only buffers ~10 frames internally before TCP/HWM backpressure
// kicks in, which is too shallow to absorb a prefill burst from a busy engine.
// We add a deeper bounded queue and a dedicated apply goroutine: the recv loop
// stays hot (draining the socket) while decode+index-apply runs concurrently.
// The queue is bounded, so a sustained overrun blocks the recv loop (natural
// backpressure → ZMQ HWM → an eventual seq gap, which we detect and which forces
// admission fallback) rather than growing memory without limit.
const applyQueueSize = 8192

// Listener subscribes to one engine's ZMQ KV event stream.
type Listener struct {
	engineID string
	model    string // primary served model used for namespace resolution
	endpoint string
	topic    string

	resolver IndexResolver
	now      func() time.Time

	// health (atomic-ish; guarded by mu for strings)
	mu        sync.RWMutex
	connected bool
	lastSeq   int64
	hasSeq    bool
	lastEvent int64
	events    atomic.Int64
	gaps      atomic.Int64
	decErrs   atomic.Int64
	skipped   atomic.Int64 // BlockStored events skipped (unresolvable parent)
	queueLen  atomic.Int64 // current recv→apply queue depth (observability)
	lastErr   string

	cancel context.CancelFunc
	done   chan struct{}
}

// NewListener builds a listener. model is the served model used to resolve the
// target index namespace for ingested events.
func NewListener(engineID, model, endpoint, topic string, resolver IndexResolver, nowFn func() time.Time) *Listener {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Listener{
		engineID: engineID, model: model, endpoint: endpoint, topic: topic,
		resolver: resolver, now: nowFn, done: make(chan struct{}),
	}
}

// Start launches the subscribe loop in a goroutine.
func (l *Listener) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	l.cancel = cancel
	go l.run(ctx)
}

// Stop signals the loop to exit and waits briefly.
func (l *Listener) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
	select {
	case <-l.done:
	case <-time.After(2 * time.Second):
	}
}

func (l *Listener) setErr(err error) {
	l.mu.Lock()
	if err != nil {
		l.lastErr = err.Error()
	}
	l.mu.Unlock()
}

func (l *Listener) run(ctx context.Context) {
	defer close(l.done)
	backoff := 500 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := l.subscribeLoop(ctx); err != nil {
			l.setErr(err)
			l.mu.Lock()
			l.connected = false
			l.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		return
	}
}

func (l *Listener) subscribeLoop(ctx context.Context) error {
	sub := zmq4.NewSub(ctx)
	defer sub.Close()
	if err := sub.Dial(l.endpoint); err != nil {
		return err
	}
	// Subscribe to all topics (empty filter) so we tolerate topic naming;
	// we still record the topic we expected for observability.
	if err := sub.SetOption(zmq4.OptionSubscribe, ""); err != nil {
		return err
	}
	l.mu.Lock()
	l.connected = true
	l.lastErr = ""
	l.mu.Unlock()

	// Decouple recv from apply: the recv loop below stays hot draining the
	// socket while a dedicated apply goroutine does the expensive work
	// (msgpack decode + index lock + store). The queue is bounded, so under a
	// sustained overrun the recv loop blocks on send → ZMQ backpressure → an
	// eventual seq gap we detect (never unbounded memory growth). On loop exit
	// we close the queue and wait for the applier to drain.
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan [][]byte, applyQueueSize)
	applyDone := make(chan struct{})
	go l.applyLoop(queue, applyDone)
	defer func() {
		close(queue)
		<-applyDone
		l.queueLen.Store(0)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		msg, err := sub.Recv()
		if err != nil {
			return err
		}
		// Copy frames: zmq4 may reuse the underlying buffers after Recv returns,
		// and the apply goroutine reads them asynchronously.
		frames := cloneFrames(msg.Frames)
		// Increment BEFORE the send so the apply goroutine's decrement can never
		// race ahead and make the observable depth go negative.
		l.queueLen.Add(1)
		select {
		case <-loopCtx.Done():
			l.queueLen.Add(-1)
			return nil
		case queue <- frames:
		}
	}
}

// applyLoop consumes queued frames and applies them to the index. Running in its
// own goroutine keeps the socket recv loop from stalling on decode/lock latency.
func (l *Listener) applyLoop(queue <-chan [][]byte, done chan<- struct{}) {
	defer close(done)
	for frames := range queue {
		l.queueLen.Add(-1)
		l.handleFrames(frames)
	}
}

// cloneFrames deep-copies ZMQ frame slices so they survive past the socket's
// buffer reuse once handed to the async apply goroutine.
func cloneFrames(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, f := range in {
		c := make([]byte, len(f))
		copy(c, f)
		out[i] = c
	}
	return out
}

// handleFrames processes a 3-frame [topic, seq, payload] message.
func (l *Listener) handleFrames(frames [][]byte) {
	if len(frames) != 3 {
		l.decErrs.Add(1)
		return
	}
	seq := binary.BigEndian.Uint64(frames[1])
	var payload []any
	if err := msgpack.Unmarshal(frames[2], &payload); err != nil {
		l.decErrs.Add(1)
		l.setErr(err)
		return
	}
	batch, err := DecodeBatch(seq, payload)
	if err != nil {
		l.decErrs.Add(1)
		l.setErr(err)
		return
	}
	l.trackSeq(int64(seq))
	l.ingest(batch)
}

func (l *Listener) trackSeq(seq int64) {
	l.mu.Lock()
	if l.hasSeq && seq > l.lastSeq+1 {
		l.gaps.Add(1)
	}
	l.lastSeq = seq
	l.hasSeq = true
	l.mu.Unlock()
}

// ingestableSpecKind reports whether a group kind reflects real per-block
// prefix-cacheable attention KV. Mamba/recurrent groups emit a degenerate
// single hash and must be skipped for prefix hit scoring.
func ingestableSpecKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "mamba", "mamba2", "linear_attention", "short_conv":
		return false
	default:
		// "full_attention", "sliding_window", "" (unknown/standard) => index.
		return true
	}
}

// ingest applies a decoded batch to the residency index.
func (l *Listener) ingest(batch *Batch) {
	ix, namespace, seed, blockSize, ok := l.resolver.ResolveIngest(l.model, l.engineID)
	if !ok {
		return
	}
	_ = namespace
	nano := l.now().UnixNano()
	for i := range batch.Events {
		ev := &batch.Events[i]
		switch ev.Kind {
		case KindBlockStored:
			if !ingestableSpecKind(ev.SpecKind) {
				continue
			}
			tier := mediumToTier(ev.Medium)
			// Effective block size: trust the event's block_size if present.
			bs := ev.BlockSize
			if bs <= 0 {
				bs = blockSize
			}
			// Compute request_keys for this event's token range. request_keys
			// chain from the parent block's request_key, resolved via the
			// engine parent hash. If the event has a parent we cannot resolve
			// (parent not yet ingested — e.g. out-of-order delivery), seeding
			// from the namespace seed would produce WRONG keys that never match
			// a query, so we skip the event rather than poison the index. The
			// parent's later arrival does not retroactively fix this block, but
			// a subsequent identical request re-emits the full chain in order.
			reqKeys, ok := l.requestKeysForEvent(ix, l.engineID, seed, bs, ev)
			if !ok {
				l.skipped.Add(1)
				continue
			}
			engKeys := toEngineKeys(ev.BlockHashes)
			ix.StoreEvent(l.engineID, batch.DPRank, tier, engKeys, reqKeys)
		case KindBlockRemoved:
			// Removes are always processed: BlockRemoved carries no spec_kind,
			// and a remove for a block we never indexed is a harmless no-op
			// (the bridge lookup simply misses).
			tier := mediumToTier(ev.Medium)
			ix.RemoveEvent(l.engineID, batch.DPRank, tier, toEngineKeys(ev.BlockHashes))
		case KindAllBlocksCleared:
			ix.ClearEngine(l.engineID, "")
		}
	}
	l.events.Add(int64(len(batch.Events)))
	l.mu.Lock()
	l.lastEvent = nano
	l.mu.Unlock()
}

// requestKeysForEvent derives request_keys for the event's token_ids. It chains
// from the parent engine block's request_key (via the index bridge) when the
// event has a parent, else from the namespace seed (prefix start). Returns
// ok=false when the event declares a parent we cannot resolve, so the caller
// can skip rather than seed from the wrong base.
func (l *Listener) requestKeysForEvent(ix *residency.Index, engineID string, seed uint64, blockSize int, ev *Event) ([]residency.RequestKey, bool) {
	parentSeed := seed
	if ev.HasParent {
		rk, ok := ix.LookupBridge(engineID, residency.EngineKey(ev.ParentHash))
		if !ok {
			return nil, false
		}
		parentSeed = uint64(rk)
	}
	return residency.RequestKeysFromTokensSeeded(parentSeed, ev.TokenIDs, blockSize), true
}

func toEngineKeys(hs []uint64) []residency.EngineKey {
	out := make([]residency.EngineKey, len(hs))
	for i, h := range hs {
		out[i] = residency.EngineKey(h)
	}
	return out
}

func mediumToTier(medium string) string {
	switch strings.ToUpper(medium) {
	case "", "GPU":
		return residency.TierGPU
	case "CPU", "CPU_PINNED":
		// vLLM emits "CPU"; SGLang's StorageMedium.CPU serializes as "CPU_PINNED"
		// (host pinned memory). Both map to the host/L2 residency tier.
		return residency.TierCPU
	case "DISK", "EXTERNAL":
		return residency.TierDisk
	default:
		return strings.ToLower(medium)
	}
}

// Health returns a snapshot of listener health.
func (l *Listener) Health() StreamHealth {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return StreamHealth{
		EngineID:      l.engineID,
		Endpoint:      l.endpoint,
		Topic:         l.topic,
		Connected:     l.connected,
		LastSeq:       l.lastSeq,
		LastEventUnix: l.lastEvent / 1e9,
		EventsTotal:   l.events.Load(),
		GapsTotal:     l.gaps.Load(),
		SkippedTotal:  l.skipped.Load(),
		DecodeErrors:  l.decErrs.Load(),
		QueueDepth:    int(l.queueLen.Load()),
		QueueCap:      applyQueueSize,
		LastError:     l.lastErr,
	}
}

var _ = log.Println
