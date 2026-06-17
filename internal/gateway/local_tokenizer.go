package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ucloud/kv-indexer/internal/admission"
	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/normalize"
	"github.com/ucloud/kv-indexer/internal/tokenizer"
	"github.com/ucloud/kv-indexer/internal/types"
)

const maxGatewayBodyBytes = 256 << 20

type localTokenizerState struct {
	ZipSHA256          string
	ChatTemplateSHA256 string
	UpdatedAt          time.Time
}

type routeResponse struct {
	Decision   admission.Decision `json:"decision"`
	Reason     admission.Reason   `json:"reason"`
	HTTPStatus int                `json:"http_status"`
	Target     *routeTarget       `json:"target,omitempty"`
	Config     routeConfigInfo    `json:"config"`
	Cache      admission.HitInfo  `json:"cache"`
	Fallback   bool               `json:"fallback"`
	Protocol   types.Protocol     `json:"protocol"`
	Error      *rejectError       `json:"error,omitempty"`
}

type routeTarget struct {
	ClusterID string `json:"cluster_id,omitempty"`
	EngineID  string `json:"engine_id"`
	Endpoint  string `json:"endpoint,omitempty"`
	DPRank    int    `json:"dp_rank"`
}

type routeConfigInfo struct {
	ModelProfileVersion int      `json:"model_profile_version"`
	Namespace           string   `json:"namespace"`
	EvaluatedRuleIDs    []string `json:"evaluated_rule_ids"`
	MatchedRuleID       string   `json:"matched_rule_id,omitempty"`
	MatchedRuleName     string   `json:"matched_rule_name,omitempty"`
	MatchedRulePriority int      `json:"matched_rule_priority,omitempty"`
	ConfigVersion       int      `json:"config_version"`
}

type rejectError struct {
	Type                string  `json:"type"`
	InputTokens         int     `json:"input_tokens"`
	BestHitTokens       int     `json:"best_hit_tokens"`
	HitRatio            float64 `json:"hit_ratio"`
	MinRequiredHitRatio float64 `json:"min_required_hit_ratio"`
}

type tokenizePreviewRequest struct {
	Model    string          `json:"model"`
	Protocol string          `json:"protocol"`
	Raw      json.RawMessage `json:"raw"`
}

type tokenizePreviewResponse struct {
	Model       string   `json:"model"`
	Namespace   string   `json:"namespace"`
	BlockSize   int      `json:"block_size"`
	Count       int      `json:"count"`
	Tokens      []int32  `json:"tokens"`
	RequestKeys []uint64 `json:"request_keys"`
}

type queryPrefixRequest struct {
	Model string `json:"model"`
}

func (g *Gateway) handleAdmission(proto types.Protocol) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readRawBody(r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		target, ok := g.selectedOneTarget(w, r)
		if !ok {
			return
		}
		if target.Virtual {
			g.handleVirtualAdmissionTarget(w, r, target, proto, body)
			return
		}
		b := target.Backend

		model := modelFromBody(body)
		if model == "" {
			log.Printf("gateway: admission proxy backend=%s cluster=%s path=%s reason=missing_model", b.ID, b.Cluster, r.URL.Path)
			g.proxyOneBody(w, r, body, r.Header.Get("Content-Type"))
			return
		}
		prof, hasProfile, err := g.fetchProfile(r.Context(), b, model)
		if err != nil {
			log.Printf("gateway: admission profile_error backend=%s cluster=%s path=%s model=%s error=%v", b.ID, b.Cluster, r.URL.Path, model, err)
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("backend %s (%s): profiles: %v", b.ID, b.Cluster, err))
			return
		}
		if !hasProfile || prof.TokenizerModeOrDefault() != config.TokenizerModeLocal {
			mode := "missing_profile"
			if hasProfile {
				mode = string(prof.TokenizerModeOrDefault())
			}
			log.Printf("gateway: admission proxy backend=%s cluster=%s path=%s model=%s tokenizer_mode=%s has_profile=%t", b.ID, b.Cluster, r.URL.Path, model, mode, hasProfile)
			g.proxyOneBody(w, r, body, r.Header.Get("Content-Type"))
			return
		}

		if g.localTokenizerURL == "" {
			log.Printf("gateway: local admission error backend=%s cluster=%s path=%s model=%s reason=missing_local_tokenizer_url", b.ID, b.Cluster, r.URL.Path, model)
			writeErr(w, http.StatusInternalServerError, "gateway local tokenizer url is not configured")
			return
		}
		rr, err := normalizeByProtocol(string(prof.Framework), proto, body)
		if err != nil {
			log.Printf("gateway: local admission normalize_error backend=%s cluster=%s path=%s model=%s framework=%s error=%v", b.ID, b.Cluster, r.URL.Path, model, prof.Framework, err)
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		tctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		tok, err := g.tokenizeLocalChat(tctx, b.Cluster, prof, rr)
		cancel()
		if err != nil {
			log.Printf("gateway: local admission tokenize_error backend=%s cluster=%s path=%s model=%s profile_model=%s error=%v", b.ID, b.Cluster, r.URL.Path, rr.Model, prof.ModelID, err)
			writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
			return
		}

		policies, err := g.fetchPolicies(r.Context(), b)
		if err != nil {
			log.Printf("gateway: local admission policies_error backend=%s cluster=%s path=%s model=%s error=%v", b.ID, b.Cluster, r.URL.Path, rr.Model, err)
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("backend %s (%s): policies: %v", b.ID, b.Cluster, err))
			return
		}
		engines, err := g.fetchEngines(r.Context(), b)
		if err != nil {
			log.Printf("gateway: local admission engines_error backend=%s cluster=%s path=%s model=%s error=%v", b.ID, b.Cluster, r.URL.Path, rr.Model, err)
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("backend %s (%s): engines: %v", b.ID, b.Cluster, err))
			return
		}
		version := g.fetchConfigVersionBestEffort(r.Context(), b)
		resp := g.evaluateLocal(rr, prof, tokenOnlyPolicies(policies), enginesForProfile(engines, rr.Model, prof), tok.Count, version, b.Cluster)
		g.recordAdmissionDecision(b, rr, resp)
		targetEngine := ""
		if resp.Target != nil {
			targetEngine = resp.Target.EngineID
		}
		log.Printf("gateway: local admission backend=%s cluster=%s path=%s model=%s profile_model=%s config_version=%d tokens=%d decision=%s reason=%s status=%d fallback=%t target=%s matched_rule=%s evaluated_rules=%d",
			b.ID, b.Cluster, r.URL.Path, rr.Model, prof.ModelID, version, tok.Count,
			resp.Decision, resp.Reason, resp.HTTPStatus, resp.Fallback, targetEngine,
			resp.Config.MatchedRuleID, len(resp.Config.EvaluatedRuleIDs))
		writeJSON(w, resp.HTTPStatus, resp)
	}
}

func (g *Gateway) handleVirtualAdmissionTarget(w http.ResponseWriter, r *http.Request, target gatewayTarget, proto types.Protocol, body []byte) {
	model := modelFromBody(body)
	if model == "" {
		writeErr(w, http.StatusBadRequest, "model required for virtual cluster admission")
		return
	}
	vc, err := g.store.GetVirtualConfig(r.Context(), target.id())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "virtual config: "+err.Error())
		return
	}
	prof, hasProfile := resolveProfile(snapshotProfiles(vc.Snapshot), model)
	if !hasProfile {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("virtual profile not found for model %s", model))
		return
	}
	if prof.TokenizerModeOrDefault() != config.TokenizerModeLocal {
		writeErr(w, http.StatusBadRequest, "virtual model profiles require tokenizer_mode=local")
		return
	}
	if g.localTokenizerURL == "" {
		writeErr(w, http.StatusInternalServerError, "gateway local tokenizer url is not configured")
		return
	}
	rr, err := normalizeByProtocol(string(prof.Framework), proto, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	tok, err := g.tokenizeLocalChat(tctx, target.cluster(), prof, rr)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
		return
	}
	resp := g.evaluateLocal(rr, prof, tokenOnlyPolicies(snapshotPolicies(vc.Snapshot)), nil, tok.Count, vc.Snapshot.Version, target.cluster())
	g.recordVirtualDecision(target.Connection, rr, resp)
	writeJSON(w, resp.HTTPStatus, resp)
}

func (g *Gateway) handleQueryPrefix(w http.ResponseWriter, r *http.Request) {
	body, err := readRawBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := g.selectedOneTarget(w, r)
	if !ok {
		return
	}
	if target.Virtual {
		writeErr(w, http.StatusBadRequest, "virtual clusters do not support KV-cache prefix residency")
		return
	}
	b := target.Backend
	var req queryPrefixRequest
	if json.Unmarshal(body, &req) == nil && req.Model != "" {
		prof, hasProfile, err := g.fetchProfile(r.Context(), b, req.Model)
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("backend %s (%s): profiles: %v", b.ID, b.Cluster, err))
			return
		}
		if hasProfile && prof.TokenizerModeOrDefault() == config.TokenizerModeLocal {
			writeErr(w, http.StatusBadRequest, "tokenizer_mode=local uses gateway token-count admission and does not query KV-cache prefix residency")
			return
		}
	}
	g.proxyOneBody(w, r, body, r.Header.Get("Content-Type"))
}

func (g *Gateway) handleTokenizePreview(w http.ResponseWriter, r *http.Request) {
	body, err := readRawBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := g.selectedOneTarget(w, r)
	if !ok {
		return
	}
	var req tokenizePreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if target.Virtual {
		g.handleVirtualTokenizePreview(w, r, target, req)
		return
	}
	b := target.Backend
	prof, hasProfile, err := g.fetchProfile(r.Context(), b, req.Model)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("backend %s (%s): profiles: %v", b.ID, b.Cluster, err))
		return
	}
	if !hasProfile || prof.TokenizerModeOrDefault() != config.TokenizerModeLocal {
		g.proxyOneBody(w, r, body, r.Header.Get("Content-Type"))
		return
	}
	if g.localTokenizerURL == "" {
		writeErr(w, http.StatusInternalServerError, "gateway local tokenizer url is not configured")
		return
	}
	rr, err := normalizeByProtocol(string(prof.Framework), types.Protocol(req.Protocol), req.Raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	tok, err := g.tokenizeLocalChat(tctx, b.Cluster, prof, rr)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
		return
	}
	blockSize := prof.BlockSize
	if blockSize <= 0 {
		blockSize = 16
	}
	writeJSON(w, http.StatusOK, tokenizePreviewResponse{
		Model:       prof.ModelID,
		Namespace:   prof.Namespace(),
		BlockSize:   blockSize,
		Count:       tok.Count,
		Tokens:      tok.Tokens,
		RequestKeys: []uint64{},
	})
}

func (g *Gateway) handleVirtualTokenizePreview(w http.ResponseWriter, r *http.Request, target gatewayTarget, req tokenizePreviewRequest) {
	if req.Model == "" {
		writeErr(w, http.StatusBadRequest, "model required for virtual tokenizer preview")
		return
	}
	vc, err := g.store.GetVirtualConfig(r.Context(), target.id())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "virtual config: "+err.Error())
		return
	}
	prof, hasProfile := resolveProfile(snapshotProfiles(vc.Snapshot), req.Model)
	if !hasProfile {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("virtual profile not found for model %s", req.Model))
		return
	}
	if prof.TokenizerModeOrDefault() != config.TokenizerModeLocal {
		writeErr(w, http.StatusBadRequest, "virtual model profiles require tokenizer_mode=local")
		return
	}
	if g.localTokenizerURL == "" {
		writeErr(w, http.StatusInternalServerError, "gateway local tokenizer url is not configured")
		return
	}
	rr, err := normalizeByProtocol(string(prof.Framework), types.Protocol(req.Protocol), req.Raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	tok, err := g.tokenizeLocalChat(tctx, target.cluster(), prof, rr)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
		return
	}
	blockSize := prof.BlockSize
	if blockSize <= 0 {
		blockSize = 16
	}
	writeJSON(w, http.StatusOK, tokenizePreviewResponse{
		Model:       prof.ModelID,
		Namespace:   prof.Namespace(),
		BlockSize:   blockSize,
		Count:       tok.Count,
		Tokens:      tok.Tokens,
		RequestKeys: []uint64{},
	})
}

func (g *Gateway) handleModelProfileUpsert(w http.ResponseWriter, r *http.Request) {
	body, err := readRawBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target, ok := g.selectedOneTarget(w, r)
	if !ok {
		return
	}

	prof, zipBytes, zipName, chatTemplate, err := parseProfilePayload(body, r.Header.Get("Content-Type"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if prof.ModelID == "" {
		writeErr(w, http.StatusBadRequest, "model_id required")
		return
	}
	if target.Virtual && prof.TokenizerModeOrDefault() != config.TokenizerModeLocal {
		writeErr(w, http.StatusBadRequest, "virtual model profiles require tokenizer_mode=local")
		return
	}
	if prof.TokenizerModeOrDefault() == config.TokenizerModeLocal {
		log.Printf(
			"gateway local tokenizer upload cluster=%s model=%s tokenizer_zip_bytes=%d chat_template_bytes=%d zip_name=%q",
			target.cluster(),
			prof.ModelID,
			len(zipBytes),
			len([]byte(chatTemplate)),
			zipName,
		)
	}

	if prof.TokenizerModeOrDefault() == config.TokenizerModeLocal {
		if g.localTokenizerURL == "" {
			writeErr(w, http.StatusInternalServerError, "gateway local tokenizer url is not configured")
			return
		}
		if g.store == nil {
			writeErr(w, http.StatusInternalServerError, "gateway store is required for local tokenizer assets")
			return
		}
		var existing TokenizerAsset
		hasExisting := false
		if asset, err := g.store.GetTokenizerAsset(r.Context(), target.cluster(), prof.ModelID); err == nil {
			existing = asset
			hasExisting = true
		} else if !errors.Is(err, ErrTokenizerAssetNotFound) {
			writeErr(w, http.StatusBadGateway, "get tokenizer asset: "+err.Error())
			return
		}
		if chatTemplate == "" && hasExisting {
			chatTemplate = existing.ChatTemplate
		}
		if len(zipBytes) == 0 && hasExisting && !existing.ZipFileID.IsZero() {
			var buf bytes.Buffer
			if err := g.store.ReadTokenizerZip(r.Context(), existing, &buf); err != nil {
				writeErr(w, http.StatusBadGateway, "read tokenizer asset: "+err.Error())
				return
			}
			zipBytes = buf.Bytes()
		}
		if len(zipBytes) == 0 {
			sourceBackendID := strings.TrimSpace(r.URL.Query().Get("source_backend"))
			if sourceBackendID != "" {
				sourceTarget, ok := g.targetByBackendID(sourceBackendID)
				if !ok {
					writeErr(w, http.StatusBadRequest, fmt.Sprintf("source_backend %q not found", sourceBackendID))
					return
				}
				if sourceTarget.cluster() != target.cluster() {
					sourceAsset, err := g.store.GetTokenizerAsset(r.Context(), sourceTarget.cluster(), prof.ModelID)
					if err == nil {
						if chatTemplate == "" {
							chatTemplate = sourceAsset.ChatTemplate
						}
						if !sourceAsset.ZipFileID.IsZero() {
							var buf bytes.Buffer
							if err := g.store.ReadTokenizerZip(r.Context(), sourceAsset, &buf); err != nil {
								writeErr(w, http.StatusBadGateway, "read source tokenizer asset: "+err.Error())
								return
							}
							zipBytes = buf.Bytes()
						}
					} else if !errors.Is(err, ErrTokenizerAssetNotFound) {
						writeErr(w, http.StatusBadGateway, "get source tokenizer asset: "+err.Error())
						return
					}
				}
			}
		}
		if len(zipBytes) == 0 {
			writeErr(w, http.StatusBadRequest, "local tokenizer profile requires tokenizer_zip on first upload")
			return
		}
		reg, err := g.registerLocalTokenizer(r.Context(), prof.ModelID, zipBytes, zipName, chatTemplate)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "local tokenizer register: "+err.Error())
			return
		}
		prof.TokenizerEndpoint = ""
		prof.TokenizerMode = config.TokenizerModeLocal
		prof.ChatTemplateSHA256 = reg.ChatTemplateSHA256
		asset, err := g.store.PutTokenizerAsset(r.Context(), TokenizerAssetInput{
			Cluster:            target.cluster(),
			ModelID:            prof.ModelID,
			TokenizerZip:       zipBytes,
			TokenizerZipName:   zipName,
			ChatTemplate:       chatTemplate,
			ChatTemplateSHA256: reg.ChatTemplateSHA256,
		})
		if err != nil {
			writeErr(w, http.StatusBadGateway, "store tokenizer asset: "+err.Error())
			return
		}
		g.markLocalTokenizerLoaded(target.cluster(), prof.ModelID, localStateFromAsset(asset))
	} else {
		prof.TokenizerMode = config.TokenizerModeRemote
		prof.ChatTemplateSHA256 = ""
	}

	if target.Virtual {
		stored, err := g.store.UpsertVirtualModelProfile(r.Context(), target.id(), prof)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "store virtual model profile: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stored)
		return
	}

	out, err := json.Marshal(prof)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	g.proxyOneBody(w, r, out, "application/json")
}

func (g *Gateway) evaluateLocal(rr *types.RouteRequest, prof config.ModelProfile, rules []config.Policy, engines []config.Engine, inputTokens int, configVersion int, fallbackCluster string) routeResponse {
	tenant := rr.TenantID
	if tenant == "" {
		tenant = "default"
	}
	clusterID := fallbackCluster
	if len(engines) > 0 && engines[0].ClusterID != "" {
		clusterID = engines[0].ClusterID
	}
	blockSize := prof.BlockSize
	if blockSize <= 0 {
		blockSize = 16
	}
	result := admission.Evaluate(admission.Input{
		ClusterID:     clusterID,
		ModelID:       rr.Model,
		TenantID:      tenant,
		InputTokens:   inputTokens,
		BlockSize:     blockSize,
		Rules:         rules,
		Fresh:         true,
		Tokenized:     true,
		HashSupported: true,
		HasCandidates: len(engines) > 0,
	})
	resp := routeResponse{
		Decision:   result.Decision,
		Reason:     result.Reason,
		HTTPStatus: result.HTTPStatus,
		Cache:      result.Hit,
		Fallback:   result.Fallback,
		Protocol:   rr.Protocol,
		Config: routeConfigInfo{
			ModelProfileVersion: prof.Version,
			Namespace:           prof.Namespace(),
			EvaluatedRuleIDs:    result.EvaluatedRuleIDs,
			MatchedRuleID:       result.MatchedRuleID,
			MatchedRuleName:     result.MatchedRuleName,
			MatchedRulePriority: result.MatchedRulePriority,
			ConfigVersion:       configVersion,
		},
	}
	if result.Decision == admission.DecisionReject {
		resp.Error = &rejectError{
			Type:                string(result.Reason),
			InputTokens:         result.Hit.InputTokens,
			BestHitTokens:       result.Hit.BestHitTokens,
			HitRatio:            result.Hit.HitRatio,
			MinRequiredHitRatio: result.MinRequiredHitRatio,
		}
	} else if target := pickLocalTarget(engines); target != nil {
		resp.Target = target
	}
	return resp
}

func (g *Gateway) recordAdmissionDecision(b Backend, rr *types.RouteRequest, resp routeResponse) {
	tenant := rr.TenantID
	if tenant == "" {
		tenant = "default"
	}
	rec := localDecisionRecord{
		Timestamp:   g.now(),
		Protocol:    rr.Protocol,
		Model:       rr.Model,
		TenantID:    tenant,
		Decision:    resp.Decision,
		Reason:      resp.Reason,
		HTTPStatus:  resp.HTTPStatus,
		InputTokens: resp.Cache.InputTokens,
		HitRatio:    resp.Cache.HitRatio,
		BestHit:     resp.Cache.BestHitTokens,
		Fallback:    resp.Fallback,
		ConfigVer:   resp.Config.ConfigVersion,
		Namespace:   resp.Config.Namespace,
		Cluster:     b.Cluster,
		Backend:     b.ID,
		Source:      "gateway_local_tokenizer",
	}
	if resp.Target != nil {
		rec.TargetEngine = resp.Target.EngineID
	}
	g.recordLocalDecision(rec)
}

func (g *Gateway) recordVirtualDecision(c Connection, rr *types.RouteRequest, resp routeResponse) {
	tenant := rr.TenantID
	if tenant == "" {
		tenant = "default"
	}
	rec := localDecisionRecord{
		Timestamp:   g.now(),
		Protocol:    rr.Protocol,
		Model:       rr.Model,
		TenantID:    tenant,
		Decision:    resp.Decision,
		Reason:      resp.Reason,
		HTTPStatus:  resp.HTTPStatus,
		InputTokens: resp.Cache.InputTokens,
		HitRatio:    resp.Cache.HitRatio,
		BestHit:     resp.Cache.BestHitTokens,
		Fallback:    resp.Fallback,
		ConfigVer:   resp.Config.ConfigVersion,
		Namespace:   resp.Config.Namespace,
		Cluster:     c.Cluster,
		Backend:     c.ID,
		Source:      "gateway_virtual_tokenizer",
	}
	g.recordLocalDecision(rec)
}

func snapshotProfiles(snap config.Snapshot) []config.ModelProfile {
	out := make([]config.ModelProfile, 0, len(snap.Profiles))
	for _, p := range snap.Profiles {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

func snapshotPolicies(snap config.Snapshot) []config.Policy {
	out := make([]config.Policy, 0, len(snap.Policies))
	for _, p := range snap.Policies {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

func (g *Gateway) selectedOne(w http.ResponseWriter, r *http.Request) (Backend, bool) {
	backends := g.selected(r)
	if len(backends) == 0 {
		writeErr(w, http.StatusNotFound, "no backend matches cluster/backend selector; pass ?cluster= or ?backend=")
		return Backend{}, false
	}
	if len(backends) > 1 {
		ids := make([]string, len(backends))
		for i, b := range backends {
			ids[i] = b.ID
		}
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("ambiguous target (%d backends: %v); pass ?backend= to disambiguate", len(backends), ids))
		return Backend{}, false
	}
	return backends[0], true
}

func (g *Gateway) fetchProfile(ctx context.Context, b Backend, model string) (config.ModelProfile, bool, error) {
	var profiles []config.ModelProfile
	if err := g.getJSON(ctx, b, "/model-profiles", "", &profiles); err != nil {
		return config.ModelProfile{}, false, err
	}
	prof, ok := resolveProfile(profiles, model)
	return prof, ok, nil
}

func (g *Gateway) fetchPolicies(ctx context.Context, b Backend) ([]config.Policy, error) {
	var out []config.Policy
	return out, g.getJSON(ctx, b, "/policies", "", &out)
}

func (g *Gateway) fetchEngines(ctx context.Context, b Backend) ([]config.Engine, error) {
	var out []config.Engine
	return out, g.getJSON(ctx, b, "/engines", "", &out)
}

func (g *Gateway) fetchConfigVersionBestEffort(ctx context.Context, b Backend) int {
	var out struct {
		ConfigVersion int `json:"config_version"`
	}
	if err := g.getJSON(ctx, b, "/config/versions", "", &out); err != nil {
		return 0
	}
	return out.ConfigVersion
}

func (g *Gateway) getJSON(ctx context.Context, b Backend, path, rawQuery string, out any) error {
	target := b.URL + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	authorize(req, b)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, truncate(body))
	}
	if len(body) == 0 || string(body) == "null" {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func resolveProfile(profiles []config.ModelProfile, model string) (config.ModelProfile, bool) {
	for _, p := range profiles {
		if p.ModelID == model {
			return p, true
		}
	}
	for _, p := range profiles {
		for _, alias := range p.Aliases {
			if alias == model {
				return p, true
			}
		}
	}
	return config.ModelProfile{}, false
}

func enginesForProfile(engines []config.Engine, requestModel string, prof config.ModelProfile) []config.Engine {
	var out []config.Engine
	for _, e := range engines {
		if !e.Enabled || e.Draining {
			continue
		}
		for _, m := range e.ServedModels {
			if m == requestModel || m == prof.ModelID {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func pickLocalTarget(engines []config.Engine) *routeTarget {
	if len(engines) == 0 {
		return nil
	}
	e := engines[0]
	return &routeTarget{ClusterID: e.ClusterID, EngineID: e.EngineID, Endpoint: e.APIEndpoint}
}

func tokenOnlyPolicies(in []config.Policy) []config.Policy {
	var out []config.Policy
	for _, p := range in {
		if p.Action.TypeOrDefault() == config.ActionRequireCacheHit {
			continue
		}
		ok := true
		for _, c := range p.Conditions {
			if !isTokenOnlyCondition(c.Field) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, p)
		}
	}
	return out
}

func isTokenOnlyCondition(field string) bool {
	switch field {
	case "",
		config.ConditionFieldClusterID,
		config.ConditionFieldModelID,
		config.ConditionFieldTenantID,
		config.ConditionFieldInputTokens,
		config.ConditionFieldTokenized,
		config.ConditionFieldHashSupported,
		config.ConditionFieldHasCandidates:
		return true
	default:
		return false
	}
}

func normalizeByProtocol(framework string, proto types.Protocol, raw []byte) (*types.RouteRequest, error) {
	adapter := normalize.AdapterFor(framework)
	switch proto {
	case types.ProtocolOpenAIResponses:
		return adapter.FromOpenAIResponses(raw)
	case types.ProtocolAnthropic:
		return adapter.FromAnthropic(raw)
	default:
		return adapter.FromOpenAIChat(raw)
	}
}

func modelFromBody(body []byte) string {
	var peek struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &peek)
	return peek.Model
}

func readRawBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxGatewayBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxGatewayBodyBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxGatewayBodyBytes)
	}
	return body, nil
}

type localModelResp struct {
	ModelID            string `json:"model_id"`
	ChatTemplateSHA256 string `json:"chat_template_sha256"`
}

func (g *Gateway) ensureLocalTokenizer(ctx context.Context, cluster string, prof config.ModelProfile) error {
	var asset TokenizerAsset
	if g.store == nil {
		return fmt.Errorf("gateway store is required for local tokenizer assets")
	}
	a, err := g.store.GetTokenizerAsset(ctx, cluster, prof.ModelID)
	if err != nil {
		if errors.Is(err, ErrTokenizerAssetNotFound) {
			return fmt.Errorf("no tokenizer asset for model %s in cluster %s", prof.ModelID, cluster)
		}
		return err
	}
	asset = a
	if asset.ZipFileID.IsZero() {
		return fmt.Errorf("tokenizer asset for model %s in cluster %s has no zip", prof.ModelID, cluster)
	}
	state := localStateFromAsset(asset)
	if g.localTokenizerLoaded(cluster, prof.ModelID, state) {
		return nil
	}

	var buf bytes.Buffer
	if err := g.store.ReadTokenizerZip(ctx, asset, &buf); err != nil {
		return err
	}
	resp, err := g.registerLocalTokenizer(ctx, prof.ModelID, buf.Bytes(), "", asset.ChatTemplate)
	if err != nil {
		return err
	}
	if resp.ChatTemplateSHA256 != "" {
		state.ChatTemplateSHA256 = resp.ChatTemplateSHA256
	}
	g.markLocalTokenizerLoaded(cluster, prof.ModelID, state)
	return nil
}

func (g *Gateway) tokenizeLocalChat(ctx context.Context, cluster string, prof config.ModelProfile, rr *types.RouteRequest) (*tokenizer.Result, error) {
	if err := g.ensureLocalTokenizer(ctx, cluster, prof); err != nil {
		return nil, err
	}
	res, err := g.tokenizer.TokenizeLocalChat(ctx, g.localTokenizerURL, prof.ModelID, rr.Messages, rr.Tools)
	if err == nil {
		return res, nil
	}
	g.clearLocalTokenizerLoaded(cluster, prof.ModelID)
	if e := g.ensureLocalTokenizer(ctx, cluster, prof); e != nil {
		return nil, fmt.Errorf("%v; reload failed: %w", err, e)
	}
	return g.tokenizer.TokenizeLocalChat(ctx, g.localTokenizerURL, prof.ModelID, rr.Messages, rr.Tools)
}

func (g *Gateway) localTokenizerLoaded(cluster, modelID string, want localTokenizerState) bool {
	g.localMu.Lock()
	defer g.localMu.Unlock()
	got, ok := g.localModels[cluster+"\x00"+modelID]
	return ok && got.ZipSHA256 == want.ZipSHA256 &&
		got.ChatTemplateSHA256 == want.ChatTemplateSHA256 &&
		got.UpdatedAt.Equal(want.UpdatedAt)
}

func (g *Gateway) markLocalTokenizerLoaded(cluster, modelID string, state localTokenizerState) {
	g.localMu.Lock()
	defer g.localMu.Unlock()
	g.localModels[cluster+"\x00"+modelID] = state
}

func (g *Gateway) clearLocalTokenizerLoaded(cluster, modelID string) {
	g.localMu.Lock()
	defer g.localMu.Unlock()
	delete(g.localModels, cluster+"\x00"+modelID)
}

func localStateFromAsset(asset TokenizerAsset) localTokenizerState {
	return localTokenizerState{
		ZipSHA256:          asset.ZipSHA256,
		ChatTemplateSHA256: asset.ChatTemplateSHA256,
		UpdatedAt:          asset.UpdatedAt,
	}
}

func (g *Gateway) registerLocalTokenizer(ctx context.Context, modelID string, zipBytes []byte, zipName, chatTemplate string) (localModelResp, error) {
	if len(zipBytes) == 0 {
		return localModelResp{}, fmt.Errorf("tokenizer_zip required")
	}
	return g.registerLocalTokenizerMultipart(ctx, modelID, zipBytes, zipName, chatTemplate)
}

func (g *Gateway) registerLocalTokenizerMultipart(ctx context.Context, modelID string, zipBytes []byte, zipName, chatTemplate string) (localModelResp, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model_id", modelID)
	if chatTemplate != "" {
		_ = mw.WriteField("chat_template", chatTemplate)
	}
	if zipName == "" {
		zipName = safeAssetFilename(modelID) + ".zip"
	}
	part, err := mw.CreateFormFile("tokenizer_zip", zipName)
	if err != nil {
		return localModelResp{}, err
	}
	if _, err := part.Write(zipBytes); err != nil {
		return localModelResp{}, err
	}
	if err := mw.Close(); err != nil {
		return localModelResp{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, localTokenizerEndpoint(g.localTokenizerURL, "/models"), &body)
	if err != nil {
		return localModelResp{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return g.doLocalModelRegister(req)
}

func (g *Gateway) doLocalModelRegister(req *http.Request) (localModelResp, error) {
	resp, err := g.client.Do(req)
	if err != nil {
		return localModelResp{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return localModelResp{}, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(body))
	}
	var out localModelResp
	if err := json.Unmarshal(body, &out); err != nil {
		return localModelResp{}, err
	}
	return out, nil
}

func localTokenizerEndpoint(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, path) {
		return base
	}
	return base + path
}

func parseProfilePayload(body []byte, contentType string) (config.ModelProfile, []byte, string, string, error) {
	mt, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mt, "multipart/") {
		return parseProfileMultipart(body, params["boundary"])
	}
	var in struct {
		config.ModelProfile
		ChatTemplate string `json:"chat_template,omitempty"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return config.ModelProfile{}, nil, "", "", err
	}
	return in.ModelProfile, nil, "", in.ChatTemplate, nil
}

func parseProfileMultipart(body []byte, boundary string) (config.ModelProfile, []byte, string, string, error) {
	if boundary == "" {
		return config.ModelProfile{}, nil, "", "", fmt.Errorf("multipart boundary required")
	}
	form, err := multipart.NewReader(bytes.NewReader(body), boundary).ReadForm(maxGatewayBodyBytes)
	if err != nil {
		return config.ModelProfile{}, nil, "", "", err
	}
	defer form.RemoveAll()
	prof := config.ModelProfile{
		ModelID:            formValue(form, "model_id"),
		Aliases:            formList(form, "aliases"),
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
	zipBytes, zipName, err := firstFileBytes(form, "tokenizer_zip")
	if err != nil {
		return config.ModelProfile{}, nil, "", "", err
	}
	chatTemplate := formValue(form, "chat_template")
	if b, _, err := firstFileBytes(form, "chat_template_file"); err != nil {
		return config.ModelProfile{}, nil, "", "", err
	} else if len(b) > 0 {
		chatTemplate = string(b)
	}
	return prof, zipBytes, zipName, chatTemplate, nil
}

func formValue(form *multipart.Form, key string) string {
	if form == nil || len(form.Value[key]) == 0 {
		return ""
	}
	return form.Value[key][0]
}

func formList(form *multipart.Form, key string) []string {
	raw := strings.TrimSpace(formValue(form, key))
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var out []string
		if json.Unmarshal([]byte(raw), &out) == nil {
			return out
		}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func formInt(form *multipart.Form, key string) int {
	n, _ := strconv.Atoi(formValue(form, key))
	return n
}

func formBool(form *multipart.Form, key string) bool {
	v, _ := strconv.ParseBool(formValue(form, key))
	return v
}

func firstFileBytes(form *multipart.Form, key string) ([]byte, string, error) {
	if form == nil || len(form.File[key]) == 0 {
		return nil, "", nil
	}
	fh := form.File[key][0]
	f, err := fh.Open()
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxGatewayBodyBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > maxGatewayBodyBytes {
		return nil, "", fmt.Errorf("%s exceeds %d bytes", key, maxGatewayBodyBytes)
	}
	if len(b) == 0 {
		return nil, "", fmt.Errorf("%s upload %q is empty", key, fh.Filename)
	}
	return b, fh.Filename, nil
}
