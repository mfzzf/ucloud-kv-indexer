package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/ucloud/kv-indexer/internal/config"
)

// ---- Clusters ----

func (s *Service) handleListClusters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.ListClusters())
}

func (s *Service) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var c config.Cluster
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if c.ClusterID == "" {
		writeErr(w, http.StatusBadRequest, "cluster_id required")
		return
	}
	s.Store.UpsertCluster(c)
	writeJSON(w, http.StatusOK, c)
}

// clusterPatch carries hot-updatable cluster fields (pointers = optional).
type clusterPatch struct {
	DisplayName     *string           `json:"display_name"`
	Enabled         *bool             `json:"enabled"`
	MaintenanceMode *bool             `json:"maintenance_mode"`
	Labels          map[string]string `json:"labels"`
}

func (s *Service) handlePatchCluster(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p clusterPatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ok := s.Store.PatchCluster(id, func(c *config.Cluster) {
		if p.DisplayName != nil {
			c.DisplayName = *p.DisplayName
		}
		if p.Enabled != nil {
			c.Enabled = *p.Enabled
		}
		if p.MaintenanceMode != nil {
			c.MaintenanceMode = *p.MaintenanceMode
		}
		if p.Labels != nil {
			c.Labels = p.Labels
		}
	})
	if !ok {
		writeErr(w, http.StatusNotFound, "cluster not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config_version": s.Store.Version()})
}

// ---- Engines ----

func (s *Service) handleListEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.ListEngines())
}

func (s *Service) handleRegisterEngine(w http.ResponseWriter, r *http.Request) {
	var e config.Engine
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if e.EngineID == "" {
		writeErr(w, http.StatusBadRequest, "engine_id required")
		return
	}
	if e.Topic == "" {
		e.Topic = "kv-events"
	}
	s.Store.UpsertEngine(e)
	s.SyncListeners()
	writeJSON(w, http.StatusOK, e)
}

func (s *Service) handleUnregisterEngine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EngineID string `json:"engine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.Store.RemoveEngine(req.EngineID) {
		writeErr(w, http.StatusNotFound, "engine not found")
		return
	}
	s.SyncListeners()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type enginePatch struct {
	Healthy    *bool             `json:"healthy"`
	Draining   *bool             `json:"draining"`
	Enabled    *bool             `json:"enabled"`
	QueueDepth *int              `json:"queue_depth"`
	MaxNumSeqs *int              `json:"max_num_seqs"`
	Labels     map[string]string `json:"labels"`
}

func (s *Service) handlePatchEngine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p enginePatch
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ok := s.Store.PatchEngine(id, func(e *config.Engine) {
		if p.Healthy != nil {
			e.Healthy = *p.Healthy
		}
		if p.Draining != nil {
			e.Draining = *p.Draining
		}
		if p.Enabled != nil {
			e.Enabled = *p.Enabled
		}
		if p.QueueDepth != nil {
			e.QueueDepth = *p.QueueDepth
		}
		if p.MaxNumSeqs != nil {
			e.MaxNumSeqs = *p.MaxNumSeqs
		}
		if p.Labels != nil {
			e.Labels = p.Labels
		}
	})
	if !ok {
		writeErr(w, http.StatusNotFound, "engine not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config_version": s.Store.Version()})
}

// ---- Model Profiles ----

func (s *Service) handleListModelProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.ListModelProfiles())
}

func (s *Service) handleCreateModelProfile(w http.ResponseWriter, r *http.Request) {
	var p config.ModelProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.ModelID == "" {
		writeErr(w, http.StatusBadRequest, "model_id required")
		return
	}
	stored := s.Store.UpsertModelProfile(p)
	writeJSON(w, http.StatusOK, stored)
}

// ---- Policies ----

func (s *Service) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.ListPolicies())
}

func (s *Service) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var p config.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.PolicyID == "" {
		writeErr(w, http.StatusBadRequest, "policy_id required")
		return
	}
	s.Store.UpsertPolicy(p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Service) handlePatchPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Patch by full replacement of provided fields: decode into a Policy and
	// copy non-nil pointer fields.
	var p config.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ok := s.Store.PatchPolicy(id, func(dst *config.Policy) {
		if p.Scope != (config.Scope{}) {
			dst.Scope = p.Scope
		}
		if p.LongPromptThresholdTokens != nil {
			dst.LongPromptThresholdTokens = p.LongPromptThresholdTokens
		}
		if p.HardLongPromptThresholdTokens != nil {
			dst.HardLongPromptThresholdTokens = p.HardLongPromptThresholdTokens
		}
		if p.MinHitRatioForLongPrompt != nil {
			dst.MinHitRatioForLongPrompt = p.MinHitRatioForLongPrompt
		}
		if p.EventFreshnessTTLMs != nil {
			dst.EventFreshnessTTLMs = p.EventFreshnessTTLMs
		}
		if p.StaleEventBehavior != nil {
			dst.StaleEventBehavior = p.StaleEventBehavior
		}
		if p.LowHitRejectStatus != nil {
			dst.LowHitRejectStatus = p.LowHitRejectStatus
		}
		if p.GPUHitWeight != nil {
			dst.GPUHitWeight = p.GPUHitWeight
		}
		if p.CPUHitWeight != nil {
			dst.CPUHitWeight = p.CPUHitWeight
		}
		if p.DiskHitWeight != nil {
			dst.DiskHitWeight = p.DiskHitWeight
		}
		if p.Enabled != nil {
			dst.Enabled = p.Enabled
		}
	})
	if !ok {
		writeErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config_version": s.Store.Version()})
}

func (s *Service) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.Store.RemovePolicy(id) {
		writeErr(w, http.StatusNotFound, "policy not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config_version": s.Store.Version()})
}

// ---- Observability ----

func (s *Service) handleEventStreams(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.StreamHealth())
}

func (s *Service) handleDecisions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Decisions())
}

func (s *Service) handleAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.Audit())
}

func (s *Service) handleConfigVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"config_version": s.Store.Version()})
}

func (s *Service) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Service) handleIndexStats(w http.ResponseWriter, r *http.Request) {
	type nsStat struct {
		Namespace   string `json:"namespace"`
		RequestKeys int    `json:"request_keys"`
		Bridges     int    `json:"bridges"`
		Engines     int    `json:"engines"`
		LastEvent   int64  `json:"last_event_unix"`
	}
	var out []nsStat
	for _, ns := range s.Index.Namespaces() {
		rk, br, en := s.Index.Index(ns).Stats()
		out = append(out, nsStat{ns, rk, br, en, s.Index.LastEventNano(ns) / 1e9})
	}
	writeJSON(w, http.StatusOK, out)
}
