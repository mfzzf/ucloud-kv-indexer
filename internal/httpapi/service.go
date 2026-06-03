// Package httpapi wires the config store, residency index, KV-event listeners,
// and tokenizer client into an HTTP service. It exposes the admission /route
// endpoint, the Mooncake/Dynamo-compatible /query-prefix, tokenize and
// effective-policy previews, full config CRUD, and observability endpoints.
package httpapi

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/kvevents"
	"github.com/ucloud/kv-indexer/internal/residency"
	"github.com/ucloud/kv-indexer/internal/tokenizer"
)

// Service holds all shared state.
type Service struct {
	Store     *config.Store
	Index     *residency.Manager
	Tokenizer *tokenizer.Client
	EventSink kvevents.EventSink
	EventLog  KVEventLog
	now       func() time.Time

	// AuthToken, when non-empty, requires every request (except /healthz) to
	// carry "Authorization: Bearer <AuthToken>". Empty disables auth (loopback
	// dev). Set before Router() is called.
	AuthToken string

	mu        sync.Mutex
	listeners map[string]*kvevents.Listener // engineID -> listener
	ctx       context.Context
	cancel    context.CancelFunc

	// decisions ring buffer for the Live Decisions page.
	decMu     sync.Mutex
	decisions []RouteRecord
	decCap    int

	// KV-event ring buffer + live subscribers for the Streams page. This sits
	// behind RecordKVEvent, so every decoded ZMQ event can be shown live while
	// still being forwarded to the configured durable sink (Mongo in local dev).
	kvMu     sync.Mutex
	kvEvents []kvevents.KVEventRecord
	kvCap    int
	kvSubs   map[chan kvevents.KVEventRecord]struct{}
}

// KVEventLog can provide persisted decoded KV events for /kv-events/recent.
type KVEventLog interface {
	RecentKVEvents(context.Context, int) ([]kvevents.KVEventRecord, error)
}

// NewService constructs a Service.
func NewService(store *config.Store, idx *residency.Manager, tok *tokenizer.Client, nowFn func() time.Time) *Service {
	if nowFn == nil {
		nowFn = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		Store: store, Index: idx, Tokenizer: tok, now: nowFn,
		listeners: map[string]*kvevents.Listener{},
		ctx:       ctx, cancel: cancel, decCap: 200,
		kvCap: 200, kvSubs: map[chan kvevents.KVEventRecord]struct{}{},
	}
}

// RecordKVEvent implements kvevents.EventSink. It fans a decoded event to the
// durable sink (if configured), keeps a small in-memory recent buffer, and
// notifies live SSE subscribers without blocking the ZMQ ingest path.
func (s *Service) RecordKVEvent(rec kvevents.KVEventRecord) {
	if s.EventSink != nil {
		s.EventSink.RecordKVEvent(rec)
	}

	s.kvMu.Lock()
	s.kvEvents = append(s.kvEvents, rec)
	if len(s.kvEvents) > s.kvCap {
		s.kvEvents = s.kvEvents[len(s.kvEvents)-s.kvCap:]
	}
	for ch := range s.kvSubs {
		select {
		case ch <- rec:
		default:
		}
	}
	s.kvMu.Unlock()
}

// RecentKVEvents returns the most recent decoded KV events, oldest first. When
// a durable event log is configured, it is merged with the in-memory live ring
// so restarts do not make the Streams page look empty.
func (s *Service) RecentKVEvents(ctx context.Context, limit int) []kvevents.KVEventRecord {
	if limit <= 0 || limit > s.kvCap {
		limit = s.kvCap
	}
	var events []kvevents.KVEventRecord
	if s.EventLog != nil {
		if persisted, err := s.EventLog.RecentKVEvents(ctx, limit); err == nil {
			events = append(events, persisted...)
		}
	}
	s.kvMu.Lock()
	start := len(s.kvEvents) - limit
	if start < 0 {
		start = 0
	}
	events = append(events, s.kvEvents[start:]...)
	s.kvMu.Unlock()

	seen := map[string]struct{}{}
	out := make([]kvevents.KVEventRecord, 0, len(events))
	for _, ev := range events {
		k := kvEventKey(ev)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ObservedAt.Before(out[j].ObservedAt)
	})
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func kvEventKey(ev kvevents.KVEventRecord) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d", ev.EngineID, ev.Seq, ev.Kind, ev.ObservedAt.Format(time.RFC3339Nano), ev.GroupIdx)
}

// SubscribeKVEvents registers a live KV-event subscriber. Slow subscribers drop
// events rather than backpressuring ZMQ ingest.
func (s *Service) SubscribeKVEvents() (<-chan kvevents.KVEventRecord, func()) {
	ch := make(chan kvevents.KVEventRecord, 256)
	s.kvMu.Lock()
	s.kvSubs[ch] = struct{}{}
	s.kvMu.Unlock()
	return ch, func() {
		s.kvMu.Lock()
		delete(s.kvSubs, ch)
		close(ch)
		s.kvMu.Unlock()
	}
}

// ResolveIngest implements kvevents.IndexResolver. It maps an ingesting engine
// + model to the residency index/namespace via the model's profile.
func (s *Service) ResolveIngest(model, engineID string) (*residency.Index, string, uint64, int, bool) {
	prof, ok := s.Store.ResolveProfile(model)
	if !ok {
		return nil, "", 0, 0, false
	}
	ns := prof.Namespace()
	seed := residency.SeedNamespace(prof.HashSeed + "|" + ns)
	return s.Index.Index(ns), ns, seed, prof.BlockSize, true
}

// SyncListeners reconciles running listeners with the configured engines:
// starts listeners for new engines, stops listeners for removed ones.
func (s *Service) SyncListeners() {
	s.mu.Lock()
	defer s.mu.Unlock()

	want := map[string]config.Engine{}
	for _, e := range s.Store.ListEngines() {
		if e.KVEventEndpoint == "" {
			continue
		}
		want[e.EngineID] = e
	}

	// Stop listeners no longer wanted.
	for id, l := range s.listeners {
		if _, ok := want[id]; !ok {
			l.Stop()
			delete(s.listeners, id)
		}
	}
	// Start listeners for new engines.
	for id, e := range want {
		if _, running := s.listeners[id]; running {
			continue
		}
		model := ""
		if len(e.ServedModels) > 0 {
			model = e.ServedModels[0]
		}
		l := kvevents.NewListener(e.EngineID, model, e.KVEventEndpoint, e.Topic, s, s.now, s)
		l.SetReplayEndpoint(e.ReplayEndpoint)
		l.Start(s.ctx)
		s.listeners[id] = l
	}
}

// StreamHealth returns health snapshots for all listeners.
func (s *Service) StreamHealth() []kvevents.StreamHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]kvevents.StreamHealth, 0, len(s.listeners))
	for _, l := range s.listeners {
		out = append(out, l.Health())
	}
	return out
}

// StreamFreshForEngines reports whether the residency index can be trusted for
// admission judgment over the given engines. The index view is "fresh" when at
// least one serving engine's listener is connected with no sequence gaps. A
// disconnected/gapped/absent stream means we may be missing residency updates,
// so misses cannot be trusted and the caller must fall back (never 429 on a low
// hit). An idle-but-connected stream is fresh: no events means nothing changed,
// so the index is authoritative. The connection itself — not a per-prefix
// timestamp — is the staleness signal, so a genuine cache MISS on a healthy
// stream is correctly judged as a real miss (eligible for 429).
func (s *Service) StreamFreshForEngines(engines []config.Engine) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range engines {
		l, ok := s.listeners[e.EngineID]
		if !ok {
			continue
		}
		h := l.Health()
		if h.Connected && h.GapsTotal == 0 {
			return true
		}
	}
	return false
}

// Shutdown stops all listeners.
func (s *Service) Shutdown() {
	s.cancel()
	s.mu.Lock()
	for _, l := range s.listeners {
		l.Stop()
	}
	s.mu.Unlock()
}

// recordDecision appends to the ring buffer.
func (s *Service) recordDecision(r RouteRecord) {
	s.decMu.Lock()
	defer s.decMu.Unlock()
	s.decisions = append(s.decisions, r)
	if len(s.decisions) > s.decCap {
		s.decisions = s.decisions[len(s.decisions)-s.decCap:]
	}
}

// Decisions returns recent route records (most recent last).
func (s *Service) Decisions() []RouteRecord {
	s.decMu.Lock()
	defer s.decMu.Unlock()
	out := make([]RouteRecord, len(s.decisions))
	copy(out, s.decisions)
	return out
}
