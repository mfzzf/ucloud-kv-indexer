package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ucloud/kv-indexer/internal/openapi"
	"github.com/ucloud/kv-indexer/internal/types"
)

// Router builds the HTTP router with all endpoints registered.
func (s *Service) Router() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Admission judgment endpoints, one per inbound protocol.
	r.POST("/route", httpHandler(s.handleRoute(types.ProtocolOpenAIChat)))
	r.POST("/v1/chat/completions", httpHandler(s.handleRoute(types.ProtocolOpenAIChat)))
	r.POST("/v1/responses", httpHandler(s.handleRoute(types.ProtocolOpenAIResponses)))
	r.POST("/v1/messages", httpHandler(s.handleRoute(types.ProtocolAnthropic)))

	// Query + previews.
	r.POST("/query-prefix", httpHandler(s.handleQueryPrefix))
	r.POST("/tokenize/preview", httpHandler(s.handleTokenizePreview))
	r.POST("/config/effective-policy/preview", httpHandler(s.handleEffectivePolicyPreview))

	// Clusters.
	r.GET("/clusters", httpHandler(s.handleListClusters))
	r.POST("/clusters", httpHandler(s.handleCreateCluster))
	r.PATCH("/clusters/:id", httpHandler(s.handlePatchCluster, "id"))

	// Engines.
	r.GET("/engines", httpHandler(s.handleListEngines))
	r.POST("/engines/register", httpHandler(s.handleRegisterEngine))
	r.POST("/engines/unregister", httpHandler(s.handleUnregisterEngine))
	r.PATCH("/engines/:id", httpHandler(s.handlePatchEngine, "id"))

	// Model profiles.
	r.GET("/model-profiles", httpHandler(s.handleListModelProfiles))
	r.POST("/model-profiles", httpHandler(s.handleCreateModelProfile))

	// Policies.
	r.GET("/policies", httpHandler(s.handleListPolicies))
	r.POST("/policies", httpHandler(s.handleCreatePolicy))
	r.PATCH("/policies/:id", httpHandler(s.handlePatchPolicy, "id"))
	r.DELETE("/policies/:id", httpHandler(s.handleDeletePolicy, "id"))

	// Observability.
	r.GET("/event-streams", httpHandler(s.handleEventStreams))
	r.GET("/kv-events/recent", httpHandler(s.handleRecentKVEvents))
	r.GET("/kv-events/stream", httpHandler(s.handleKVEventStream))
	r.GET("/decisions", httpHandler(s.handleDecisions))
	r.GET("/config/audit-log", httpHandler(s.handleAudit))
	r.GET("/config/versions", httpHandler(s.handleConfigVersion))
	r.GET("/index/stats", httpHandler(s.handleIndexStats))
	r.GET("/healthz", httpHandler(s.handleHealthz))
	r.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, openapi.KVIndexerSpec())
	})

	// CORS is outermost (so OPTIONS preflight never needs the token); auth sits
	// just inside it and gates every route except /healthz (liveness probe).
	return withCORS(s.withAuth(r))
}

func httpHandler(h http.HandlerFunc, pathParams ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range pathParams {
			c.Request.SetPathValue(name, c.Param(name))
		}
		h(c.Writer, c.Request)
	}
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
