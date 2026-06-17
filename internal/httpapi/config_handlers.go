package httpapi

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

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
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		s.handleCreateModelProfileMultipart(w, r)
		return
	}
	var in struct {
		config.ModelProfile
		ChatTemplate string `json:"chat_template,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p := in.ModelProfile
	if p.ModelID == "" {
		writeErr(w, http.StatusBadRequest, "model_id required")
		return
	}
	stored := s.Store.UpsertModelProfile(p)
	writeJSON(w, http.StatusOK, stored)
}

func (s *Service) handleCreateModelProfileMultipart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	form := r.MultipartForm
	p := config.ModelProfile{
		ModelID:            formValue(form, "model_id"),
		Framework:          config.Framework(formValue(form, "framework")),
		TokenizerEndpoint:  formValue(form, "tokenizer_endpoint"),
		TokenizerMode:      config.TokenizerMode(formValue(form, "tokenizer_mode")),
		ChatTemplateSHA256: formValue(form, "chat_template_sha256"),
		HashProfile:        formValue(form, "hash_profile"),
		BlockSize:          formInt(form, "block_size"),
		HashSeed:           formValue(form, "hash_seed"),
		SupportsLoRA:       formBool(form, "supports_lora"),
		SupportsMultimodal: formBool(form, "supports_multimodal"),
		SupportsCacheSalt:  formBool(form, "supports_cache_salt"),
	}
	if p.ModelID == "" {
		writeErr(w, http.StatusBadRequest, "model_id required")
		return
	}
	stored := s.Store.UpsertModelProfile(p)
	writeJSON(w, http.StatusOK, stored)
}

func (s *Service) handleDeleteModelProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.Store.RemoveModelProfile(id) {
		writeErr(w, http.StatusNotFound, "model profile not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config_version": s.Store.Version()})
}

func formValue(form *multipart.Form, key string) string {
	if form == nil || len(form.Value[key]) == 0 {
		return ""
	}
	return form.Value[key][0]
}

func formInt(form *multipart.Form, key string) int {
	n, _ := strconv.Atoi(formValue(form, key))
	return n
}

func formBool(form *multipart.Form, key string) bool {
	v, _ := strconv.ParseBool(formValue(form, key))
	return v
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
	if p.RuleID == "" {
		writeErr(w, http.StatusBadRequest, "rule_id required")
		return
	}
	s.Store.UpsertPolicy(p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Service) handlePatchPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Patch is a full replacement of editable rule fields. The path id remains
	// authoritative, so callers cannot rename a rule through PATCH.
	var p config.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ok := s.Store.PatchPolicy(id, func(dst *config.Policy) {
		dst.Name = p.Name
		dst.Priority = p.Priority
		dst.Conditions = p.Conditions
		dst.Action = p.Action
		dst.Enabled = p.Enabled
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
