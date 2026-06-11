package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ucloud/kv-indexer/internal/admission"
	"github.com/ucloud/kv-indexer/internal/config"
	"github.com/ucloud/kv-indexer/internal/normalize"
	"github.com/ucloud/kv-indexer/internal/residency"
	"github.com/ucloud/kv-indexer/internal/types"
)

// normalizeByProtocol dispatches normalization by protocol string, using the
// adapter for the given engine framework (vLLM/SGLang differ on Anthropic).
func normalizeByProtocol(framework, proto string, raw json.RawMessage) (*types.RouteRequest, error) {
	adapter := normalize.AdapterFor(framework)
	switch types.Protocol(proto) {
	case types.ProtocolOpenAIResponses:
		return adapter.FromOpenAIResponses(raw)
	case types.ProtocolAnthropic:
		return adapter.FromAnthropic(raw)
	default:
		return adapter.FromOpenAIChat(raw)
	}
}

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": msg}})
}

// ---- /query-prefix : Mooncake/Dynamo-compatible prefix hit query ----

// QueryPrefixRequest accepts either token_ids directly or a chat/prompt to
// tokenize first. model is required to resolve the profile/namespace.
type QueryPrefixRequest struct {
	Model     string  `json:"model"`
	TenantID  string  `json:"tenant_id,omitempty"`
	TokenIDs  []int32 `json:"token_ids,omitempty"`
	Prompt    string  `json:"prompt,omitempty"`
	BlockSize int     `json:"block_size,omitempty"`
}

// QueryPrefixResponse mirrors the Mooncake/Dynamo /query shape.
type QueryPrefixResponse struct {
	ModelName   string                            `json:"model_name"`
	BlockSize   int                               `json:"block_size"`
	Namespace   string                            `json:"namespace"`
	HashProfile string                            `json:"hash_profile"`
	Fresh       bool                              `json:"fresh"`
	Instances   map[string]*residency.InstanceHit `json:"instances"`
}

func (s *Service) handleQueryPrefix(w http.ResponseWriter, r *http.Request) {
	var req QueryPrefixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prof, ok := s.Store.ResolveProfile(req.Model)
	if !ok {
		writeErr(w, http.StatusNotFound, "no model profile for "+req.Model)
		return
	}
	if prof.TokenizerModeOrDefault() == config.TokenizerModeLocal {
		writeErr(w, http.StatusBadRequest, "tokenizer_mode=local is handled by gateway and does not query KV-cache prefix residency")
		return
	}
	blockSize := req.BlockSize
	if blockSize <= 0 {
		blockSize = prof.BlockSize
	}
	tokens := req.TokenIDs
	if len(tokens) == 0 && req.Prompt != "" {
		engines := s.Store.EnginesForModel(req.Model)
		ep := profileTokenizerEndpoint(prof, engines)
		if ep == "" {
			writeErr(w, http.StatusBadRequest, "no tokenizer endpoint and no token_ids provided")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		res, err := s.Tokenizer.TokenizeCompletion(ctx, ep, prof.ModelID, req.Prompt)
		cancel()
		if err != nil {
			writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
			return
		}
		tokens = res.Tokens
	}
	ns := prof.Namespace()
	seed := residency.SeedNamespace(prof.HashSeed + "|" + ns)
	reqKeys := residency.RequestKeysFromTokens(seed, tokens, blockSize)
	ix := s.Index.Index(ns)
	qr := ix.Query(reqKeys, blockSize)

	// Freshness reflects whether the serving engines' event streams are healthy
	// (connected, no gaps), not whether this particular prefix was recently
	// touched.
	fresh := s.StreamFreshForEngines(s.Store.EnginesForModel(req.Model))

	writeJSON(w, http.StatusOK, QueryPrefixResponse{
		ModelName:   prof.ModelID,
		BlockSize:   blockSize,
		Namespace:   ns,
		HashProfile: prof.HashProfile,
		Fresh:       fresh,
		Instances:   qr.Instances,
	})
}

// ---- /tokenize/preview : show tokens + request_keys for a request ----

type TokenizePreviewRequest struct {
	Model    string `json:"model"`
	Protocol string `json:"protocol"` // openai.chat | openai.responses | anthropic.messages
	// Raw is the original protocol body to normalize and tokenize.
	Raw json.RawMessage `json:"raw"`
}

type TokenizePreviewResponse struct {
	Model       string   `json:"model"`
	Namespace   string   `json:"namespace"`
	BlockSize   int      `json:"block_size"`
	Count       int      `json:"count"`
	Tokens      []int32  `json:"tokens"`
	RequestKeys []uint64 `json:"request_keys"`
}

func (s *Service) handleTokenizePreview(w http.ResponseWriter, r *http.Request) {
	var req TokenizePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prof, ok := s.Store.ResolveProfile(req.Model)
	if !ok {
		writeErr(w, http.StatusNotFound, "no model profile for "+req.Model)
		return
	}
	engines := s.Store.EnginesForModel(req.Model)
	ep := profileTokenizerEndpoint(prof, engines)
	if prof.TokenizerModeOrDefault() == config.TokenizerModeLocal {
		writeErr(w, http.StatusBadRequest, "tokenizer_mode=local preview is handled by gateway")
		return
	}
	if ep == "" {
		writeErr(w, http.StatusBadRequest, "no tokenizer endpoint configured")
		return
	}
	rr, err := normalizeByProtocol(string(prof.Framework), req.Protocol, req.Raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	res, err := s.tokenize(ctx, prof, ep, rr)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "tokenize: "+err.Error())
		return
	}
	blockSize := prof.BlockSize
	if blockSize <= 0 {
		blockSize = 16
	}
	ns := prof.Namespace()
	seed := residency.SeedNamespace(prof.HashSeed + "|" + ns)
	keys := residency.RequestKeysFromTokens(seed, res.Tokens, blockSize)
	out := make([]uint64, len(keys))
	for i, k := range keys {
		out[i] = uint64(k)
	}
	writeJSON(w, http.StatusOK, TokenizePreviewResponse{
		Model: prof.ModelID, Namespace: ns, BlockSize: blockSize,
		Count: res.Count, Tokens: res.Tokens, RequestKeys: out,
	})
}

// ---- /config/effective-policy/preview ----

type EffectivePolicyPreviewRequest struct {
	ClusterID     string   `json:"cluster_id,omitempty"`
	ModelID       string   `json:"model_id,omitempty"`
	TenantID      string   `json:"tenant_id,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	HitRatio      *float64 `json:"hit_ratio,omitempty"`
	Fresh         *bool    `json:"fresh,omitempty"`
	Tokenized     *bool    `json:"tokenized,omitempty"`
	HashSupported *bool    `json:"hash_supported,omitempty"`
	HasCandidates *bool    `json:"has_candidates,omitempty"`
}

type EffectivePolicyPreviewResponse struct {
	Rules  []config.Policy  `json:"rules"`
	Result admission.Result `json:"result"`
}

func (s *Service) handleEffectivePolicyPreview(w http.ResponseWriter, r *http.Request) {
	var req EffectivePolicyPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	fresh, tokenized, hashSupported, hasCandidates := true, true, true, true
	if req.Fresh != nil {
		fresh = *req.Fresh
	}
	if req.Tokenized != nil {
		tokenized = *req.Tokenized
	}
	if req.HashSupported != nil {
		hashSupported = *req.HashSupported
	}
	if req.HasCandidates != nil {
		hasCandidates = *req.HasCandidates
	}
	rules := s.Store.ListPolicies()
	var hitOverride *admission.HitInfo
	if req.HitRatio != nil {
		ratio := *req.HitRatio
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		eff := int(float64(req.InputTokens) * ratio)
		hitOverride = &admission.HitInfo{
			InputTokens:           req.InputTokens,
			BestHitTokens:         eff,
			HitRatio:              ratio,
			EffectiveCachedTokens: eff,
		}
	}
	result := admission.Evaluate(admission.Input{
		ClusterID:     req.ClusterID,
		ModelID:       req.ModelID,
		TenantID:      req.TenantID,
		InputTokens:   req.InputTokens,
		Rules:         rules,
		HitOverride:   hitOverride,
		Fresh:         fresh,
		Tokenized:     tokenized,
		HashSupported: hashSupported,
		HasCandidates: hasCandidates,
	})
	writeJSON(w, http.StatusOK, EffectivePolicyPreviewResponse{Rules: rules, Result: result})
}
