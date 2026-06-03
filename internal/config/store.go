package config

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Persister abstracts where the config snapshot lives. The Store calls Save
// after every mutation and Load once at startup. Implementations: FilePersister
// (JSON file) and SQLitePersister (a pure-Go SQLite DB). The default kvindexer
// store is `memory` (no persister at all — config is reseeded from the
// bootstrap YAML on every boot).
type Persister interface {
	// Save persists the snapshot. Best-effort: errors are logged by the impl,
	// not surfaced into the hot mutation path.
	Save(Snapshot)
	// Load returns the stored snapshot. ok=false means "nothing stored yet"
	// (fresh instance) and is not an error. err is for real failures.
	Load() (snap Snapshot, ok bool, err error)
}

// Store is the in-memory, thread-safe configuration store. Mutations bump the
// global version and are persisted through the configured Persister.
type Store struct {
	mu sync.RWMutex

	clusters map[string]*Cluster
	engines  map[string]*Engine
	// profiles keyed by modelID -> current ModelProfile (latest version).
	profiles map[string]*ModelProfile
	policies map[string]*Policy

	version int
	audit   []AuditEntry

	now       func() time.Time
	persister Persister
}

// NewStore builds an empty store backed by a JSON file at snapPath (empty path
// = no persistence). Kept for backward compatibility; prefer NewStoreWith.
func NewStore(snapPath string, nowFn func() time.Time) *Store {
	var p Persister
	if snapPath != "" {
		p = NewFilePersister(snapPath)
	}
	return NewStoreWith(p, nowFn)
}

// NewStoreWith builds an empty store backed by the given Persister (nil = no
// persistence). nowFn may be nil (defaults to time.Now).
func NewStoreWith(p Persister, nowFn func() time.Time) *Store {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Store{
		clusters:  map[string]*Cluster{},
		engines:   map[string]*Engine{},
		profiles:  map[string]*ModelProfile{},
		policies:  map[string]*Policy{},
		now:       nowFn,
		persister: p,
	}
}

// Version returns the current config version.
func (s *Store) Version() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) bump(a AuditEntry) {
	s.version++
	a.Version = s.version
	a.Timestamp = s.now()
	s.audit = append(s.audit, a)
}

// ---- Clusters ----

func (s *Store) UpsertCluster(c Cluster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := c
	s.clusters[c.ClusterID] = &cp
	s.bump(AuditEntry{Action: "upsert", Entity: "cluster", EntityID: c.ClusterID})
	s.persistLocked()
}

func (s *Store) ListClusters() []Cluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Cluster, 0, len(s.clusters))
	for _, c := range s.clusters {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClusterID < out[j].ClusterID })
	return out
}

// PatchCluster applies hot-updatable fields. Returns false if not found.
func (s *Store) PatchCluster(id string, fn func(*Cluster)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clusters[id]
	if !ok {
		return false
	}
	fn(c)
	s.bump(AuditEntry{Action: "patch", Entity: "cluster", EntityID: id})
	s.persistLocked()
	return true
}

// ---- Engines ----

func (s *Store) UpsertEngine(e Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := e
	s.engines[e.EngineID] = &cp
	s.bump(AuditEntry{Action: "upsert", Entity: "engine", EntityID: e.EngineID})
	s.persistLocked()
}

func (s *Store) RemoveEngine(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engines[id]; !ok {
		return false
	}
	delete(s.engines, id)
	s.bump(AuditEntry{Action: "remove", Entity: "engine", EntityID: id})
	s.persistLocked()
	return true
}

func (s *Store) ListEngines() []Engine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Engine, 0, len(s.engines))
	for _, e := range s.engines {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EngineID < out[j].EngineID })
	return out
}

func (s *Store) GetEngine(id string) (Engine, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.engines[id]
	if !ok {
		return Engine{}, false
	}
	return *e, true
}

func (s *Store) PatchEngine(id string, fn func(*Engine)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.engines[id]
	if !ok {
		return false
	}
	fn(e)
	s.bump(AuditEntry{Action: "patch", Entity: "engine", EntityID: id})
	s.persistLocked()
	return true
}

// EnginesForModel returns enabled, non-draining engines (in enabled,
// non-maintenance clusters) that serve the given model.
func (s *Store) EnginesForModel(model string) []Engine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Engine
	for _, e := range s.engines {
		if !e.Enabled || e.Draining {
			continue
		}
		if c, ok := s.clusters[e.ClusterID]; ok {
			if !c.Enabled || c.MaintenanceMode {
				continue
			}
		}
		for _, m := range e.ServedModels {
			if m == model {
				out = append(out, *e)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EngineID < out[j].EngineID })
	return out
}

// ---- Model Profiles ----

// affectsHashNamespace reports whether the new profile differs from the old in
// any token/hash-affecting field (forcing a version bump).
func affectsHashNamespace(old, neu ModelProfile) bool {
	return old.Framework != neu.Framework ||
		old.TokenizerEndpoint != neu.TokenizerEndpoint ||
		old.HashProfile != neu.HashProfile ||
		old.BlockSize != neu.BlockSize ||
		old.HashSeed != neu.HashSeed ||
		old.SupportsLoRA != neu.SupportsLoRA ||
		old.SupportsMultimodal != neu.SupportsMultimodal ||
		old.SupportsCacheSalt != neu.SupportsCacheSalt
}

// UpsertModelProfile stores a profile. If a profile already exists and a
// token/hash-affecting field changed, Version is auto-bumped and the audit
// entry is flagged. Returns the stored profile (with possibly bumped version).
func (s *Store) UpsertModelProfile(p ModelProfile) ModelProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	bump := false
	if old, ok := s.profiles[p.ModelID]; ok {
		if affectsHashNamespace(*old, p) {
			p.Version = old.Version + 1
			bump = true
		} else if p.Version == 0 {
			p.Version = old.Version
		}
	} else if p.Version == 0 {
		p.Version = 1
	}
	cp := p
	s.profiles[p.ModelID] = &cp
	s.bump(AuditEntry{Action: "upsert", Entity: "model_profile", EntityID: p.ModelID, VersionBump: bump,
		Detail: fmt.Sprintf("version=%d", p.Version)})
	s.persistLocked()
	return cp
}

func (s *Store) ListModelProfiles() []ModelProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ModelProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}

// ResolveProfile finds a profile by model_id or alias.
func (s *Store) ResolveProfile(model string) (ModelProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.profiles[model]; ok {
		return *p, true
	}
	for _, p := range s.profiles {
		for _, a := range p.Aliases {
			if a == model {
				return *p, true
			}
		}
	}
	return ModelProfile{}, false
}

// ---- Policies ----

func (s *Store) UpsertPolicy(p Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := p
	s.policies[p.PolicyID] = &cp
	s.bump(AuditEntry{Action: "upsert", Entity: "policy", EntityID: p.PolicyID})
	s.persistLocked()
}

func (s *Store) RemovePolicy(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.policies[id]; !ok {
		return false
	}
	delete(s.policies, id)
	s.bump(AuditEntry{Action: "remove", Entity: "policy", EntityID: id})
	s.persistLocked()
	return true
}

func (s *Store) ListPolicies() []Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Policy, 0, len(s.policies))
	for _, p := range s.policies {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PolicyID < out[j].PolicyID })
	return out
}

func (s *Store) PatchPolicy(id string, fn func(*Policy)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.policies[id]
	if !ok {
		return false
	}
	fn(p)
	s.bump(AuditEntry{Action: "patch", Entity: "policy", EntityID: id})
	s.persistLocked()
	return true
}

// specificity scores a scope: more specific (more fields set) = higher.
func specificity(sc Scope) int {
	n := 0
	if sc.ClusterID != "" {
		n += 1
	}
	if sc.ModelID != "" {
		n += 2
	}
	if sc.TenantID != "" {
		n += 4
	}
	return n
}

// matches reports whether a policy scope applies to the given dimensions.
// Empty scope fields are wildcards.
func (sc Scope) matches(cluster, model, tenant string) bool {
	if sc.ClusterID != "" && sc.ClusterID != cluster {
		return false
	}
	if sc.ModelID != "" && sc.ModelID != model {
		return false
	}
	if sc.TenantID != "" && sc.TenantID != tenant {
		return false
	}
	return true
}

// EffectivePolicy merges all matching policies over the global default, from
// least to most specific. Each non-nil field overrides. Returns the resolved
// policy and the ordered list of contributing policy IDs.
func (s *Store) EffectivePolicy(cluster, model, tenant string) EffectivePolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	eff := DefaultEffectivePolicy()

	var matched []*Policy
	for _, p := range s.policies {
		if p.Scope.matches(cluster, model, tenant) {
			matched = append(matched, p)
		}
	}
	// Sort ascending by specificity; ties broken by policy ID for determinism.
	sort.Slice(matched, func(i, j int) bool {
		si, sj := specificity(matched[i].Scope), specificity(matched[j].Scope)
		if si != sj {
			return si < sj
		}
		return matched[i].PolicyID < matched[j].PolicyID
	})

	for _, p := range matched {
		applyOverride(&eff, p)
		eff.SourcePolicyIDs = append(eff.SourcePolicyIDs, p.PolicyID)
	}
	return eff
}

func applyOverride(eff *EffectivePolicy, p *Policy) {
	if p.LongPromptThresholdTokens != nil {
		eff.LongPromptThresholdTokens = *p.LongPromptThresholdTokens
	}
	if p.HardLongPromptThresholdTokens != nil {
		eff.HardLongPromptThresholdTokens = *p.HardLongPromptThresholdTokens
	}
	if p.MinHitRatioForLongPrompt != nil {
		eff.MinHitRatioForLongPrompt = *p.MinHitRatioForLongPrompt
	}
	if p.EventFreshnessTTLMs != nil {
		eff.EventFreshnessTTLMs = *p.EventFreshnessTTLMs
	}
	if p.StaleEventBehavior != nil {
		eff.StaleEventBehavior = *p.StaleEventBehavior
	}
	if p.LowHitRejectStatus != nil {
		eff.LowHitRejectStatus = *p.LowHitRejectStatus
	}
	if p.GPUHitWeight != nil {
		eff.GPUHitWeight = *p.GPUHitWeight
	}
	if p.CPUHitWeight != nil {
		eff.CPUHitWeight = *p.CPUHitWeight
	}
	if p.DiskHitWeight != nil {
		eff.DiskHitWeight = *p.DiskHitWeight
	}
	if p.Enabled != nil {
		eff.Enabled = *p.Enabled
	}
}

// ---- Audit ----

// Audit returns a copy of the audit log (most recent last).
func (s *Store) Audit() []AuditEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEntry, len(s.audit))
	copy(out, s.audit)
	return out
}

// ---- Persistence ----

// Snapshot is the full serializable config state. Persisters round-trip this.
type Snapshot struct {
	Version  int             `json:"version" bson:"version"`
	Clusters []*Cluster      `json:"clusters" bson:"clusters"`
	Engines  []*Engine       `json:"engines" bson:"engines"`
	Profiles []*ModelProfile `json:"profiles" bson:"profiles"`
	Policies []*Policy       `json:"policies" bson:"policies"`
	Audit    []AuditEntry    `json:"audit" bson:"audit"`
}

// snapshotLocked builds a Snapshot from current state. Caller holds at least RLock.
func (s *Store) snapshotLocked() Snapshot {
	snap := Snapshot{Version: s.version, Audit: s.audit}
	for _, c := range s.clusters {
		snap.Clusters = append(snap.Clusters, c)
	}
	for _, e := range s.engines {
		snap.Engines = append(snap.Engines, e)
	}
	for _, p := range s.profiles {
		snap.Profiles = append(snap.Profiles, p)
	}
	for _, p := range s.policies {
		snap.Policies = append(snap.Policies, p)
	}
	return snap
}

// persistLocked hands the current snapshot to the persister. Caller holds the
// write lock. The persister itself is responsible for not blocking unduly.
func (s *Store) persistLocked() {
	if s.persister == nil {
		return
	}
	s.persister.Save(s.snapshotLocked())
}

// Load restores state from the persister if anything is stored. A fresh
// (empty) backend is not an error.
func (s *Store) Load() error {
	if s.persister == nil {
		return nil
	}
	snap, ok, err := s.persister.Load()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	s.applySnapshot(snap)
	return nil
}

// applySnapshot replaces in-memory state with the given snapshot.
func (s *Store) applySnapshot(snap Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = snap.Version
	s.audit = snap.Audit
	s.clusters = map[string]*Cluster{}
	s.engines = map[string]*Engine{}
	s.profiles = map[string]*ModelProfile{}
	s.policies = map[string]*Policy{}
	for _, c := range snap.Clusters {
		s.clusters[c.ClusterID] = c
	}
	for _, e := range snap.Engines {
		s.engines[e.EngineID] = e
	}
	for _, p := range snap.Profiles {
		s.profiles[p.ModelID] = p
	}
	for _, p := range snap.Policies {
		s.policies[p.PolicyID] = p
	}
}
