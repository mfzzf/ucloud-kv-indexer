package kvevents

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"strconv"
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
	EngineID       string `json:"engine_id"`
	Endpoint       string `json:"endpoint"`
	ReplayEndpoint string `json:"replay_endpoint,omitempty"`
	Topic          string `json:"topic"`
	Connected      bool   `json:"connected"`
	LastSeq        int64  `json:"last_seq"`
	LastEventUnix  int64  `json:"last_event_unix"`
	EventsTotal    int64  `json:"events_total"`
	GapsTotal      int64  `json:"gaps_total"`
	SkippedTotal   int64  `json:"skipped_total"`
	DecodeErrors   int64  `json:"decode_errors"`
	// QueueDepth/QueueCap expose the recv→apply backpressure buffer. A depth
	// persistently near cap means the index-apply step (decode + lock + store)
	// is not keeping up with this engine's event rate — the signal to shard the
	// cluster across more kvindexer processes (see docs/scaling.md).
	QueueDepth int    `json:"queue_depth"`
	QueueCap   int    `json:"queue_cap"`
	LastError  string `json:"last_error,omitempty"`
}

// EventSink receives decoded KV-cache events after the listener has interpreted
// whether they were indexed or skipped. Implementations must be non-blocking or
// internally buffered: this sits on the ZMQ ingest path.
type EventSink interface {
	RecordKVEvent(KVEventRecord)
}

// KVEventRecord is the Mongo-friendly persistence shape for received ZMQ KV
// events. uint64 hashes are stringified so BSON never has to coerce them into
// signed int64 values.
type KVEventRecord struct {
	ObservedAt     time.Time `json:"observed_at" bson:"observed_at"`
	EngineID       string    `json:"engine_id" bson:"engine_id"`
	Model          string    `json:"model" bson:"model"`
	Namespace      string    `json:"namespace,omitempty" bson:"namespace,omitempty"`
	Seq            string    `json:"seq" bson:"seq"`
	BatchTS        float64   `json:"batch_ts,omitempty" bson:"batch_ts,omitempty"`
	DPRank         int       `json:"dp_rank" bson:"dp_rank"`
	Kind           string    `json:"kind" bson:"kind"`
	BlockHashes    []string  `json:"block_hashes,omitempty" bson:"block_hashes,omitempty"`
	ParentHash     string    `json:"parent_hash,omitempty" bson:"parent_hash,omitempty"`
	TokenIDs       []int32   `json:"token_ids,omitempty" bson:"token_ids,omitempty"`
	NestedTokenIDs bool      `json:"nested_token_ids,omitempty" bson:"nested_token_ids,omitempty"`
	BlockSize      int       `json:"block_size,omitempty" bson:"block_size,omitempty"`
	Medium         string    `json:"medium,omitempty" bson:"medium,omitempty"`
	Tier           string    `json:"tier,omitempty" bson:"tier,omitempty"`
	LoraID         *int      `json:"lora_id,omitempty" bson:"lora_id,omitempty"`
	LoraName       string    `json:"lora_name,omitempty" bson:"lora_name,omitempty"`
	ExtraKeys      []string  `json:"extra_keys,omitempty" bson:"extra_keys,omitempty"`
	ExtraKeyCount  int       `json:"extra_key_count,omitempty" bson:"extra_key_count,omitempty"`
	GroupIdx       int       `json:"group_idx,omitempty" bson:"group_idx,omitempty"`
	SpecKind       string    `json:"spec_kind,omitempty" bson:"spec_kind,omitempty"`
	SlidingWindow  int       `json:"sliding_window,omitempty" bson:"sliding_window,omitempty"`
	RequestKeys    []string  `json:"request_keys,omitempty" bson:"request_keys,omitempty"`
	Indexed        bool      `json:"indexed" bson:"indexed"`
	SkipReason     string    `json:"skip_reason,omitempty" bson:"skip_reason,omitempty"`
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
	engineID       string
	model          string // primary served model used for namespace resolution
	endpoint       string
	replayEndpoint string
	topic          string

	resolver IndexResolver
	now      func() time.Time
	sink     EventSink

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
func NewListener(engineID, model, endpoint, topic string, resolver IndexResolver, nowFn func() time.Time, sinks ...EventSink) *Listener {
	if nowFn == nil {
		nowFn = time.Now
	}
	var sink EventSink
	if len(sinks) > 0 {
		sink = sinks[0]
	}
	return &Listener{
		engineID: engineID, model: model, endpoint: endpoint, topic: topic,
		resolver: resolver, now: nowFn, sink: sink, done: make(chan struct{}),
	}
}

// SetReplayEndpoint configures the optional vLLM/SGLang replay ROUTER endpoint.
// Replay is best-effort; live SUB remains the source of truth if replay fails.
func (l *Listener) SetReplayEndpoint(endpoint string) {
	l.replayEndpoint = endpoint
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

	l.replayBuffered(ctx)

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

func (l *Listener) replayBuffered(ctx context.Context) {
	if l.replayEndpoint == "" {
		return
	}
	start := l.nextReplaySeq()
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req := zmq4.NewReq(rctx)
	defer req.Close()
	if err := req.Dial(l.replayEndpoint); err != nil {
		l.setErr(fmt.Errorf("replay dial: %w", err))
		return
	}
	startFrame := make([]byte, 8)
	binary.BigEndian.PutUint64(startFrame, uint64(start))
	if err := req.Send(zmq4.NewMsg(startFrame)); err != nil {
		l.setErr(fmt.Errorf("replay request: %w", err))
		return
	}
	for {
		msg, err := req.Recv()
		if err != nil {
			l.setErr(fmt.Errorf("replay recv: %w", err))
			return
		}
		if len(msg.Frames) == 0 {
			l.decErrs.Add(1)
			continue
		}
		seqFrame := msg.Frames[0]
		if isReplayEnd(seqFrame) {
			return
		}
		if len(seqFrame) != 8 || len(msg.Frames) < 2 {
			l.decErrs.Add(1)
			continue
		}
		seq := binary.BigEndian.Uint64(seqFrame)
		var payload []any
		if err := msgpack.Unmarshal(msg.Frames[1], &payload); err != nil {
			l.decErrs.Add(1)
			l.setErr(err)
			continue
		}
		batch, err := DecodeBatch(seq, payload)
		if err != nil {
			l.decErrs.Add(1)
			l.setErr(err)
			continue
		}
		l.trackSeq(int64(seq))
		l.ingest(batch)
	}
}

func (l *Listener) nextReplaySeq() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.hasSeq {
		return 0
	}
	return l.lastSeq + 1
}

func isReplayEnd(frame []byte) bool {
	if len(frame) != 8 {
		return false
	}
	return int64(binary.BigEndian.Uint64(frame)) == -1
}

func (l *Listener) trackSeq(seq int64) {
	l.mu.Lock()
	if l.hasSeq && seq <= l.lastSeq {
		l.mu.Unlock()
		return
	}
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
	nano := l.now().UnixNano()
	if !ok {
		for i := range batch.Events {
			l.recordEvent(batch, &batch.Events[i], "", "", nil, false, "no_resolver")
		}
		l.events.Add(int64(len(batch.Events)))
		l.mu.Lock()
		l.lastEvent = nano
		l.mu.Unlock()
		return
	}
	for i := range batch.Events {
		ev := &batch.Events[i]
		switch ev.Kind {
		case KindBlockStored:
			if !ingestableSpecKind(ev.SpecKind) {
				l.recordEvent(batch, ev, namespace, "", nil, false, "non_ingestable_spec_kind")
				continue
			}
			if ev.HasNestedTokenIDs {
				l.recordEvent(batch, ev, namespace, "", nil, false, "unsupported_nested_token_ids")
				continue
			}
			if hasUnsupportedFeatureKeys(ev) {
				l.recordEvent(batch, ev, namespace, "", nil, false, "unsupported_hash_extra_keys")
				continue
			}
			tier := mediumToTier(ev.Medium)
			if len(ev.TokenIDs) == 0 && len(ev.BlockHashes) > 0 {
				reqKeys, ok := ix.StoreEventByEngineKeys(l.engineID, batch.DPRank, tier, toEngineKeys(ev.BlockHashes))
				if !ok {
					l.skipped.Add(1)
					l.recordEvent(batch, ev, namespace, tier, nil, false, "unresolved_engine_key")
					continue
				}
				l.recordEvent(batch, ev, namespace, tier, reqKeys, true, "")
				continue
			}
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
				l.recordEvent(batch, ev, namespace, tier, nil, false, "unresolved_parent")
				continue
			}
			if len(reqKeys) == 0 {
				l.skipped.Add(1)
				l.recordEvent(batch, ev, namespace, tier, nil, false, "no_full_token_block")
				continue
			}
			engKeys := toEngineKeys(ev.BlockHashes)
			ix.StoreEvent(l.engineID, batch.DPRank, tier, engKeys, reqKeys)
			l.recordEvent(batch, ev, namespace, tier, reqKeys, true, "")
		case KindBlockRemoved:
			// Removes are always processed: BlockRemoved carries no spec_kind,
			// and a remove for a block we never indexed is a harmless no-op
			// (the bridge lookup simply misses).
			tier := mediumToTier(ev.Medium)
			ix.RemoveEvent(l.engineID, batch.DPRank, tier, toEngineKeys(ev.BlockHashes))
			l.recordEvent(batch, ev, namespace, tier, nil, true, "")
		case KindAllBlocksCleared:
			ix.ClearEngine(l.engineID, "")
			l.recordEvent(batch, ev, namespace, "", nil, true, "")
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

func hasUnsupportedFeatureKeys(ev *Event) bool {
	return ev.HasExtraKeys || ev.HasLoraID || ev.LoraName != ""
}

func (l *Listener) recordEvent(batch *Batch, ev *Event, namespace, tier string, reqKeys []residency.RequestKey, indexed bool, skipReason string) {
	if l.sink == nil || ev == nil {
		return
	}
	rec := KVEventRecord{
		ObservedAt:     l.now(),
		EngineID:       l.engineID,
		Model:          l.model,
		Namespace:      namespace,
		Seq:            strconv.FormatUint(batch.Seq, 10),
		BatchTS:        batch.TS,
		DPRank:         batch.DPRank,
		Kind:           string(ev.Kind),
		BlockHashes:    uint64Strings(ev.BlockHashes),
		TokenIDs:       append([]int32(nil), ev.TokenIDs...),
		NestedTokenIDs: ev.HasNestedTokenIDs,
		BlockSize:      ev.BlockSize,
		Medium:         ev.Medium,
		Tier:           tier,
		LoraName:       ev.LoraName,
		ExtraKeys:      append([]string(nil), ev.ExtraKeys...),
		ExtraKeyCount:  ev.ExtraKeyCount,
		GroupIdx:       ev.GroupIdx,
		SpecKind:       ev.SpecKind,
		RequestKeys:    requestKeyStrings(reqKeys),
		Indexed:        indexed,
		SkipReason:     skipReason,
	}
	if ev.HasLoraID {
		id := ev.LoraID
		rec.LoraID = &id
	}
	if ev.HasSlidingWindow {
		rec.SlidingWindow = ev.SlidingWindow
	}
	if ev.HasParent {
		rec.ParentHash = strconv.FormatUint(ev.ParentHash, 10)
	}
	l.sink.RecordKVEvent(rec)
}

func uint64Strings(in []uint64) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strconv.FormatUint(v, 10)
	}
	return out
}

func requestKeyStrings(in []residency.RequestKey) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strconv.FormatUint(uint64(v), 10)
	}
	return out
}

// Health returns a snapshot of listener health.
func (l *Listener) Health() StreamHealth {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return StreamHealth{
		EngineID:       l.engineID,
		Endpoint:       l.endpoint,
		ReplayEndpoint: l.replayEndpoint,
		Topic:          l.topic,
		Connected:      l.connected,
		LastSeq:        l.lastSeq,
		LastEventUnix:  l.lastEvent / 1e9,
		EventsTotal:    l.events.Load(),
		GapsTotal:      l.gaps.Load(),
		SkippedTotal:   l.skipped.Load(),
		DecodeErrors:   l.decErrs.Load(),
		QueueDepth:     int(l.queueLen.Load()),
		QueueCap:       applyQueueSize,
		LastError:      l.lastErr,
	}
}

var _ = log.Println
