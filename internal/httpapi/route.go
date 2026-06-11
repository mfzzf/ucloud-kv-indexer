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
	"github.com/ucloud/kv-indexer/internal/tokenizer"
	"github.com/ucloud/kv-indexer/internal/types"
)

// RouteRecord is a stored decision for the Live Decisions page.
type RouteRecord struct {
	Timestamp    time.Time          `json:"timestamp"`
	Protocol     types.Protocol     `json:"protocol"`
	Model        string             `json:"model"`
	TenantID     string             `json:"tenant_id"`
	Decision     admission.Decision `json:"decision"`
	Reason       admission.Reason   `json:"reason"`
	HTTPStatus   int                `json:"http_status"`
	InputTokens  int                `json:"input_tokens"`
	HitRatio     float64            `json:"hit_ratio"`
	BestHit      int                `json:"best_hit_tokens"`
	TargetEngine string             `json:"target_engine,omitempty"`
	Fallback     bool               `json:"fallback"`
	ConfigVer    int                `json:"config_version"`
	Namespace    string             `json:"namespace"`
}

// RouteResponse is the /route response body.
type RouteResponse struct {
	Decision   admission.Decision `json:"decision"`
	Reason     admission.Reason   `json:"reason"`
	HTTPStatus int                `json:"http_status"`
	Target     *Target            `json:"target,omitempty"`
	Config     ConfigInfo         `json:"config"`
	Cache      admission.HitInfo  `json:"cache"`
	Fallback   bool               `json:"fallback"`
	Protocol   types.Protocol     `json:"protocol"`
	// Error is populated only on reject (429).
	Error *RejectError `json:"error,omitempty"`
}

// Target is the chosen engine (informational; this service judges, the gateway
// routes). We surface the best-cache-hit engine as the suggested target.
type Target struct {
	ClusterID string `json:"cluster_id,omitempty"`
	EngineID  string `json:"engine_id"`
	Endpoint  string `json:"endpoint,omitempty"`
	DPRank    int    `json:"dp_rank"`
}

// ConfigInfo echoes the resolved config provenance.
type ConfigInfo struct {
	ModelProfileVersion int      `json:"model_profile_version"`
	Namespace           string   `json:"namespace"`
	EvaluatedRuleIDs    []string `json:"evaluated_rule_ids"`
	MatchedRuleID       string   `json:"matched_rule_id,omitempty"`
	MatchedRuleName     string   `json:"matched_rule_name,omitempty"`
	MatchedRulePriority int      `json:"matched_rule_priority,omitempty"`
	ConfigVersion       int      `json:"config_version"`
}

// RejectError is the structured 429 reason.
type RejectError struct {
	Type                string  `json:"type"`
	InputTokens         int     `json:"input_tokens"`
	BestHitTokens       int     `json:"best_hit_tokens"`
	HitRatio            float64 `json:"hit_ratio"`
	MinRequiredHitRatio float64 `json:"min_required_hit_ratio"`
}

// handleRoute is the admission judgment entrypoint. The inbound protocol is
// selected by the URL path. It normalizes (with a framework-specific adapter),
// tokenizes via the engine endpoint, computes prefix request_keys, queries
// residency, and applies the policy.
func (s *Service) handleRoute(proto types.Protocol) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		rr, err := s.normalizeRequest(proto, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		resp := s.evaluate(r.Context(), rr)
		status := resp.toHTTPStatus()
		writeJSON(w, status, resp)
	}
}

// frameworkForBody peeks the top-level "model" field (present in all three
// protocols), resolves its profile, and returns the engine framework so the
// right normalize adapter is used. Unknown model/framework -> "" (AdapterFor
// then defaults to SGLang).
func (s *Service) frameworkForBody(body []byte) string {
	var peek struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &peek); err != nil || peek.Model == "" {
		return ""
	}
	if prof, ok := s.Store.ResolveProfile(peek.Model); ok {
		return string(prof.Framework)
	}
	return ""
}

// normalizeRequest converts an inbound body to a RouteRequest using the adapter
// matching the resolved engine framework (vLLM and SGLang disagree on the
// Anthropic conversion; Chat/Responses are framework-independent).
func (s *Service) normalizeRequest(proto types.Protocol, body []byte) (*types.RouteRequest, error) {
	adapter := normalize.AdapterFor(s.frameworkForBody(body))
	switch proto {
	case types.ProtocolOpenAIResponses:
		return adapter.FromOpenAIResponses(body)
	case types.ProtocolAnthropic:
		return adapter.FromAnthropic(body)
	default:
		return adapter.FromOpenAIChat(body)
	}
}

func (rr RouteResponse) toHTTPStatus() int {
	if rr.HTTPStatus > 0 {
		return rr.HTTPStatus
	}
	return http.StatusOK
}

// evaluate runs the full pipeline and returns a response (also records it).
func (s *Service) evaluate(ctx context.Context, rr *types.RouteRequest) RouteResponse {
	tenant := rr.TenantID
	if tenant == "" {
		tenant = "default"
	}

	prof, hasProfile := s.Store.ResolveProfile(rr.Model)
	engines := s.Store.EnginesForModel(rr.Model)
	clusterID := ""
	if len(engines) > 0 {
		clusterID = engines[0].ClusterID
	}
	rules := s.Store.ListPolicies()

	resp := RouteResponse{Protocol: rr.Protocol}
	resp.Config = ConfigInfo{
		ConfigVersion: s.Store.Version(),
	}

	// If no profile is configured we cannot tokenize/hash; fall back to accept
	// (never 429 under uncertainty).
	if !hasProfile {
		in := admission.Input{ClusterID: clusterID, ModelID: rr.Model, TenantID: tenant,
			InputTokens: 0, BlockSize: 16, Rules: rules,
			Tokenized: false, HashSupported: true, Fresh: false, HasCandidates: len(engines) > 0}
		ar := admission.Evaluate(in)
		return s.finish(rr, resp, ar, prof, "", engines)
	}
	resp.Config.ModelProfileVersion = prof.Version
	ns := prof.Namespace()
	resp.Config.Namespace = ns

	// Tokenize via the engine endpoint. Gateway-local tokenizer profiles are
	// handled by kvgateway before requests reach this backend.
	tokEndpoint := profileTokenizerEndpoint(prof, engines)
	hashSupported := prof.SupportsMultimodal || !rr.HasMultimodalContent()

	var tokenized bool
	var inputTokens, blockSize int
	var reqKeys []residency.RequestKey
	var query *residency.QueryResult
	// Freshness is a property of the event stream (are we receiving updates?),
	// not of the query result. A genuine miss on a healthy stream is a real
	// miss; only a disconnected/gapped stream forces fallback.
	fresh := s.StreamFreshForEngines(engines)
	blockSize = prof.BlockSize
	if blockSize <= 0 {
		blockSize = 16
	}

	if tokEndpoint != "" {
		tctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		res, terr := s.tokenize(tctx, prof, tokEndpoint, rr)
		cancel()
		if terr == nil && res != nil && len(res.Tokens) > 0 {
			tokenized = true
			inputTokens = res.Count
			seed := residency.SeedNamespace(prof.HashSeed + "|" + ns)
			reqKeys = residency.RequestKeysFromTokens(seed, res.Tokens, blockSize)
			ix := s.Index.Index(ns)
			query = ix.Query(reqKeys, blockSize)
		}
	}

	in := admission.Input{
		ClusterID:     clusterID,
		ModelID:       rr.Model,
		TenantID:      tenant,
		InputTokens:   inputTokens,
		BlockSize:     blockSize,
		Rules:         rules,
		Query:         query,
		Fresh:         fresh,
		Tokenized:     tokenized,
		HashSupported: hashSupported,
		HasCandidates: len(engines) > 0,
	}
	ar := admission.Evaluate(in)
	return s.finish(rr, resp, ar, prof, ns, engines)
}

func (s *Service) finish(rr *types.RouteRequest, resp RouteResponse, ar admission.Result, prof config.ModelProfile, ns string, engines []config.Engine) RouteResponse {
	resp.Decision = ar.Decision
	resp.Reason = ar.Reason
	resp.HTTPStatus = ar.HTTPStatus
	resp.Cache = ar.Hit
	resp.Fallback = ar.Fallback
	resp.Config.EvaluatedRuleIDs = ar.EvaluatedRuleIDs
	resp.Config.MatchedRuleID = ar.MatchedRuleID
	resp.Config.MatchedRuleName = ar.MatchedRuleName
	resp.Config.MatchedRulePriority = ar.MatchedRulePriority

	if ar.Decision == admission.DecisionReject {
		resp.Error = &RejectError{
			Type:                string(ar.Reason),
			InputTokens:         ar.Hit.InputTokens,
			BestHitTokens:       ar.Hit.BestHitTokens,
			HitRatio:            ar.Hit.HitRatio,
			MinRequiredHitRatio: ar.MinRequiredHitRatio,
		}
	} else {
		// Suggest the best-hit engine as target if known, else first eligible.
		target := s.pickTarget(ar.Hit.InstanceID, engines)
		resp.Target = target
	}

	// Record for Live Decisions.
	rec := RouteRecord{
		Timestamp:   s.now(),
		Protocol:    rr.Protocol,
		Model:       rr.Model,
		TenantID:    rr.TenantID,
		Decision:    ar.Decision,
		Reason:      ar.Reason,
		HTTPStatus:  resp.toHTTPStatus(),
		InputTokens: ar.Hit.InputTokens,
		HitRatio:    ar.Hit.HitRatio,
		BestHit:     ar.Hit.BestHitTokens,
		Fallback:    ar.Fallback,
		ConfigVer:   s.Store.Version(),
		Namespace:   ns,
	}
	if resp.Target != nil {
		rec.TargetEngine = resp.Target.EngineID
	}
	s.recordDecision(rec)
	return resp
}

// pickTarget returns the engine matching instanceID, or the first eligible one.
func (s *Service) pickTarget(instanceID string, engines []config.Engine) *Target {
	if len(engines) == 0 {
		return nil
	}
	for _, e := range engines {
		if e.EngineID == instanceID {
			return &Target{ClusterID: e.ClusterID, EngineID: e.EngineID, Endpoint: e.APIEndpoint}
		}
	}
	e := engines[0]
	return &Target{ClusterID: e.ClusterID, EngineID: e.EngineID, Endpoint: e.APIEndpoint}
}

// profileTokenizerEndpoint resolves the tokenizer endpoint: profile override,
// else the first eligible engine's tokenizer endpoint.
func profileTokenizerEndpoint(prof config.ModelProfile, engines []config.Engine) string {
	if prof.TokenizerEndpoint != "" {
		return prof.TokenizerEndpoint
	}
	for _, e := range engines {
		if e.TokenizerEndpoint != "" {
			return e.TokenizerEndpoint
		}
	}
	return ""
}

// tokenize calls the engine /tokenize using the chat form (folded messages).
func (s *Service) tokenize(ctx context.Context, prof config.ModelProfile, endpoint string, rr *types.RouteRequest) (*tokenizeResult, error) {
	// We always use the chat form because the normalizer produces a unified
	// message list; the engine applies its own chat template.
	var (
		res *tokenizer.Result
		err error
	)
	if prof.Framework == config.FrameworkSGLang {
		res, err = s.Tokenizer.TokenizeChatSGLang(ctx, endpoint, prof.ModelID, rr.Messages, rr.Tools, nil)
	} else {
		res, err = s.Tokenizer.TokenizeChat(ctx, endpoint, prof.ModelID, rr.Messages, rr.Tools, nil)
	}
	if err != nil {
		return nil, err
	}
	return &tokenizeResult{Tokens: res.Tokens, Count: res.Count, MaxModelLen: res.MaxModelLen}, nil
}

type tokenizeResult struct {
	Tokens      []int32
	Count       int
	MaxModelLen int
}

// readBody reads and limits the request body.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
