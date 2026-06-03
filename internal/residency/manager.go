// Package residency: manager.go holds the multi-namespace index manager.
package residency

import (
	"sync"
	"time"
)

// Manager owns one Index per profile namespace, giving request_key isolation
// across model-profile versions / hash profiles.
type Manager struct {
	mu      sync.RWMutex
	indexes map[string]*Index
	now     func() time.Time
	// lastEventNano tracks the newest event time per (namespace) for freshness.
	lastEventNano map[string]int64
}

// NewManager creates an empty manager.
func NewManager(nowFn func() time.Time) *Manager {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Manager{
		indexes:       map[string]*Index{},
		now:           nowFn,
		lastEventNano: map[string]int64{},
	}
}

// Index returns (creating if needed) the index for a namespace.
func (m *Manager) Index(namespace string) *Index {
	m.mu.RLock()
	ix := m.indexes[namespace]
	m.mu.RUnlock()
	if ix != nil {
		return ix
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ix = m.indexes[namespace]; ix == nil {
		ix = NewIndex(m.now)
		m.indexes[namespace] = ix
	}
	return ix
}

// MarkEvent records the latest event timestamp for a namespace.
func (m *Manager) MarkEvent(namespace string, nano int64) {
	m.mu.Lock()
	if nano > m.lastEventNano[namespace] {
		m.lastEventNano[namespace] = nano
	}
	m.mu.Unlock()
}

// LastEventNano returns the newest event time seen for a namespace (0 if none).
func (m *Manager) LastEventNano(namespace string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastEventNano[namespace]
}

// Namespaces returns the known namespaces (for observability).
func (m *Manager) Namespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.indexes))
	for ns := range m.indexes {
		out = append(out, ns)
	}
	return out
}
