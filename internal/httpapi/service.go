// Package httpapi wires the config store, residency index, KV-event listeners,
// and tokenizer client into an HTTP service. It exposes the admission /route
// endpoint, the Mooncake/Dynamo-compatible /query-prefix, tokenize and
// effective-policy previews, full config CRUD, and observability endpoints.
package httpapi

import (
	"context"
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
		l := kvevents.NewListener(e.EngineID, model, e.KVEventEndpoint, e.Topic, s, s.now)
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
