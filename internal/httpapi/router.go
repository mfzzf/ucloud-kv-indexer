package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/ucloud/kv-indexer/internal/types"
)

// Router builds the HTTP mux with all endpoints registered.
func (s *Service) Router() http.Handler {
	mux := http.NewServeMux()

	// Admission judgment endpoints, one per inbound protocol.
	mux.HandleFunc("POST /route", s.handleRoute(types.ProtocolOpenAIChat))
	mux.HandleFunc("POST /v1/chat/completions", s.handleRoute(types.ProtocolOpenAIChat))
	mux.HandleFunc("POST /v1/responses", s.handleRoute(types.ProtocolOpenAIResponses))
	mux.HandleFunc("POST /v1/messages", s.handleRoute(types.ProtocolAnthropic))

	// Query + previews.
	mux.HandleFunc("POST /query-prefix", s.handleQueryPrefix)
	mux.HandleFunc("POST /tokenize/preview", s.handleTokenizePreview)
	mux.HandleFunc("POST /config/effective-policy/preview", s.handleEffectivePolicyPreview)

	// Clusters.
	mux.HandleFunc("GET /clusters", s.handleListClusters)
	mux.HandleFunc("POST /clusters", s.handleCreateCluster)
	mux.HandleFunc("PATCH /clusters/{id}", s.handlePatchCluster)

	// Engines.
	mux.HandleFunc("GET /engines", s.handleListEngines)
	mux.HandleFunc("POST /engines/register", s.handleRegisterEngine)
	mux.HandleFunc("POST /engines/unregister", s.handleUnregisterEngine)
	mux.HandleFunc("PATCH /engines/{id}", s.handlePatchEngine)

	// Model profiles.
	mux.HandleFunc("GET /model-profiles", s.handleListModelProfiles)
	mux.HandleFunc("POST /model-profiles", s.handleCreateModelProfile)

	// Policies.
	mux.HandleFunc("GET /policies", s.handleListPolicies)
	mux.HandleFunc("POST /policies", s.handleCreatePolicy)
	mux.HandleFunc("PATCH /policies/{id}", s.handlePatchPolicy)

	// Observability.
	mux.HandleFunc("GET /event-streams", s.handleEventStreams)
	mux.HandleFunc("GET /decisions", s.handleDecisions)
	mux.HandleFunc("GET /config/audit-log", s.handleAudit)
	mux.HandleFunc("GET /config/versions", s.handleConfigVersion)
	mux.HandleFunc("GET /index/stats", s.handleIndexStats)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// CORS is outermost (so OPTIONS preflight never needs the token); auth sits
	// just inside it and gates every route except /healthz (liveness probe).
	return withCORS(s.withAuth(mux))
}

// withAuth enforces "Authorization: Bearer <AuthToken>" when AuthToken is set.
// /healthz is always exempt so liveness/health probes (and the gateway's
// reachability check) work without the secret. A constant-time compare avoids
// leaking the token via timing. No-op when AuthToken is empty (loopback dev).
func (s *Service) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AuthToken == "" || r.Method == http.MethodOptions || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, prefix)), []byte(s.AuthToken)) != 1 {
			writeErr(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS allows the Next.js dev/prod frontend to call the API cross-origin.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
