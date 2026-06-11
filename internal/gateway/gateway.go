// Package gateway is a multi-cluster aggregation control plane for kvindexer.
//
// Each inference CLUSTER (a GPU pool + serving framework + model, e.g. "H20
// pool #1 running SGLang Qwen3") is served by one or more kvindexer backends
// (one process per deployment, sitting next to its inference engines so the ZMQ
// KV-event stream stays local — see the project README). A browser cannot and
// should not fan out to every backend itself, so this gateway does it:
//
//   - GET list endpoints (/engines, /clusters, /event-streams, /decisions, ...)
//     fan out to the selected backends, tag every returned object with its
//     origin (_cluster / _backend), and merge into one array.
//   - Write endpoints (POST/PATCH register/patch engine, policy, profile) are
//     proxied to exactly ONE backend, chosen via ?cluster= / ?backend=, because
//     you register an engine into a specific cluster.
//   - Admission/query endpoints (/v1/chat/completions, /query-prefix, ...) are
//     proxied to one selected backend.
//   - GET /clusters-health enumerates clusters and per-backend health (probed).
//
// The frontend talks only to this gateway and selects a cluster with ?cluster=.
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ucloud/kv-indexer/internal/openapi"
	"github.com/ucloud/kv-indexer/internal/tokenizer"
	"github.com/ucloud/kv-indexer/internal/types"
)

// Backend is one kvindexer instance, labelled with the cluster it serves.
type Backend struct {
	ID      string `json:"id"`      // logical backend id, unique across the gateway
	Cluster string `json:"cluster"` // cluster this backend serves, e.g. "h20-1"
	URL     string `json:"url"`     // base URL of the kvindexer, e.g. http://10.0.0.1:8090
	Token   string `json:"-"`       // bearer token attached to requests to this kvindexer
}

// Gateway federates a dynamic set of backends. The backend set is supplied by a
// provider func (backed by the SQL connection store) so admin CRUD edits take
// effect immediately without a restart.
type Gateway struct {
	provider  func() []Backend
	store     Store // optional: enables /admin/connections CRUD and local tokenizer assets
	client    *http.Client
	tokenizer *tokenizer.Client
	now       func() time.Time

	localTokenizerURL string
	localMu           sync.Mutex
	localModels       map[string]localTokenizerState
}

// New builds a gateway over a fixed backend list (kept for tests / static use).
func New(backends []Backend, now func() time.Time) *Gateway {
	return NewWithProvider(func() []Backend { return backends }, now)
}

// NewWithProvider builds a gateway whose active backend set is read from provider
// on every request (so connection-registry edits apply live).
func NewWithProvider(provider func() []Backend, now func() time.Time) *Gateway {
	return &Gateway{
		provider: provider,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		tokenizer:   tokenizer.New(),
		localModels: map[string]localTokenizerState{},
		now:         now,
	}
}

// NewWithStore builds a gateway backed by a SQL connection store: the active
// backend set is the store's enabled connections, and the /admin/connections
// CRUD endpoints manage them live.
func NewWithStore(store Store, now func() time.Time) *Gateway {
	g := NewWithProvider(store.Backends, now)
	g.store = store
	return g
}

// SetLocalTokenizerURL enables gateway-local tokenization for model profiles
// whose tokenizer_mode is local.
func (g *Gateway) SetLocalTokenizerURL(url string) {
	g.localTokenizerURL = url
}

// backends returns the current active backend set.
func (g *Gateway) backends() []Backend { return g.provider() }

// ---- backend selection ----

// selected returns the backends matching the request's ?cluster= / ?backend=
// filters. With neither filter (or cluster=all) it returns every backend.
func (g *Gateway) selected(r *http.Request) []Backend {
	cluster := r.URL.Query().Get("cluster")
	backendID := r.URL.Query().Get("backend")
	var out []Backend
	for _, b := range g.backends() {
		if backendID != "" && b.ID != backendID {
			continue
		}
		if cluster != "" && cluster != "all" && b.Cluster != cluster {
			continue
		}
		out = append(out, b)
	}
	return out
}

// ---- cluster listing + health ----

type backendHealth struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

type clusterInfo struct {
	Cluster  string          `json:"cluster"`
	Backends []backendHealth `json:"backends"`
}

func (g *Gateway) handleClustersHealth(w http.ResponseWriter, r *http.Request) {
	type res struct {
		idx     int
		bh      backendHealth
		cluster string
	}
	// Snapshot once: g.backends() may rebuild from the live registry, so calling
	// it twice could size results to N and then range over N±1 (a concurrent
	// /admin/connections edit) → index out of range.
	backends := g.backends()
	results := make([]res, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func(i int, b Backend) {
			defer wg.Done()
			bh := backendHealth{ID: b.ID, URL: b.URL}
			if err := g.probe(b); err != nil {
				bh.Healthy = false
				bh.Error = err.Error()
			} else {
				bh.Healthy = true
			}
			results[i] = res{idx: i, bh: bh, cluster: b.Cluster}
		}(i, b)
	}
	wg.Wait()

	// Group backends by cluster, preserving first-seen cluster order.
	order := []string{}
	byCluster := map[string]*clusterInfo{}
	for _, r := range results {
		ci, ok := byCluster[r.cluster]
		if !ok {
			ci = &clusterInfo{Cluster: r.cluster}
			byCluster[r.cluster] = ci
			order = append(order, r.cluster)
		}
		ci.Backends = append(ci.Backends, r.bh)
	}
	out := make([]clusterInfo, 0, len(order))
	for _, c := range order {
		out = append(out, *byCluster[c])
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) probe(b Backend) error {
	req, err := http.NewRequest(http.MethodGet, b.URL+"/healthz", nil)
	if err != nil {
		return err
	}
	authorize(req, b)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz %d", resp.StatusCode)
	}
	return nil
}

// ---- fan-out list merge ----

// fanoutList fans a GET to all selected backends, tags each returned array
// element with _cluster/_backend, and merges. Per-backend failures are logged
// and skipped so one dead cluster never blanks the whole console.
func (g *Gateway) fanoutList(path string, postMerge func([]map[string]any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backends := g.selected(r)
		type part struct {
			items []map[string]any
		}
		parts := make([]part, len(backends))
		var wg sync.WaitGroup
		for i, b := range backends {
			wg.Add(1)
			go func(i int, b Backend) {
				defer wg.Done()
				items, err := g.getList(b, path, r.URL.RawQuery)
				if err != nil {
					log.Printf("gateway: backend %s (%s) %s: %v", b.ID, b.Cluster, path, err)
					return
				}
				for _, it := range items {
					it["_cluster"] = b.Cluster
					it["_backend"] = b.ID
				}
				parts[i] = part{items: items}
			}(i, b)
		}
		wg.Wait()

		merged := []map[string]any{}
		for _, p := range parts {
			merged = append(merged, p.items...)
		}
		if postMerge != nil {
			postMerge(merged)
		}
		writeJSON(w, http.StatusOK, merged)
	}
}

func (g *Gateway) getList(b Backend, path, rawQuery string) ([]map[string]any, error) {
	target := b.URL + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	authorize(req, b)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(body))
	}
	if len(body) == 0 || string(body) == "null" {
		return nil, nil
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	return items, nil
}

// ---- config/versions aggregate (object, not array) ----

func (g *Gateway) handleConfigVersions(w http.ResponseWriter, r *http.Request) {
	backends := g.selected(r)
	type vres struct {
		Cluster       string `json:"cluster"`
		Backend       string `json:"backend"`
		ConfigVersion int    `json:"config_version"`
		Error         string `json:"error,omitempty"`
	}
	out := make([]vres, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func(i int, b Backend) {
			defer wg.Done()
			v := vres{Cluster: b.Cluster, Backend: b.ID}
			body, status, err := g.forward(b, http.MethodGet, "/config/versions", nil, "")
			if err != nil || status != http.StatusOK {
				v.Error = errString(err, status, body)
			} else {
				var m struct {
					ConfigVersion int `json:"config_version"`
				}
				if json.Unmarshal(body, &m) == nil {
					v.ConfigVersion = m.ConfigVersion
				}
			}
			out[i] = v
		}(i, b)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, out)
}

// ---- single-backend proxy (writes, admission, query) ----

// proxyOne forwards the request verbatim to exactly one selected backend.
// It requires the selection to resolve to a single backend; ambiguity or no
// match is a 400/404 so a write never silently lands in the wrong cluster.
func (g *Gateway) proxyOne(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	g.proxyOneBody(w, r, body, r.Header.Get("Content-Type"))
}

func (g *Gateway) proxyOneBody(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	backends := g.selected(r)
	if len(backends) == 0 {
		writeErr(w, http.StatusNotFound,
			"no backend matches cluster/backend selector; pass ?cluster= or ?backend=")
		return
	}
	if len(backends) > 1 {
		ids := make([]string, len(backends))
		for i, b := range backends {
			ids[i] = b.ID
		}
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("ambiguous target (%d backends: %v); pass ?backend= to disambiguate", len(backends), ids))
		return
	}
	b := backends[0]

	respBody, status, err := g.forward(b, r.Method, r.URL.Path, body, contentType)
	if err != nil {
		writeErr(w, http.StatusBadGateway,
			fmt.Sprintf("backend %s (%s): %v", b.ID, b.Cluster, err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-KVI-Backend", b.ID)
	w.Header().Set("X-KVI-Cluster", b.Cluster)
	w.WriteHeader(status)
	w.Write(respBody)
}

// proxyStreamOne forwards a long-lived streaming GET (SSE) to exactly one
// backend. The ordinary gateway client has a 30s timeout for JSON calls, so the
// streaming path uses a no-timeout client and flushes each chunk as it arrives.
func (g *Gateway) proxyStreamOne(w http.ResponseWriter, r *http.Request) {
	backends := g.selected(r)
	if len(backends) == 0 {
		writeErr(w, http.StatusNotFound,
			"no backend matches cluster/backend selector; pass ?cluster= or ?backend=")
		return
	}
	if len(backends) > 1 {
		ids := make([]string, len(backends))
		for i, b := range backends {
			ids[i] = b.ID
		}
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("ambiguous stream target (%d backends: %v); select one cluster/backend", len(backends), ids))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	b := backends[0]
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, b.URL+r.URL.Path, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	authorize(req, b)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway,
			fmt.Sprintf("backend %s (%s): %v", b.ID, b.Cluster, err))
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-KVI-Backend", b.ID)
	w.Header().Set("X-KVI-Cluster", b.Cluster)
	w.WriteHeader(resp.StatusCode)
	flusher.Flush()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			return
		}
	}
}

func (g *Gateway) forward(b Backend, method, path string, body []byte, contentType string) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, b.URL+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	authorize(req, b)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

// authorize attaches the backend's bearer token to a request bound for that
// kvindexer. No-op when the connection has no token (loopback dev).
func authorize(req *http.Request, b Backend) {
	if b.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.Token)
	}
}

// ---- router ----

// Router wires the federation endpoints + CORS.
func (g *Gateway) Router() http.Handler {
	return withCORS(g.ginRouter())
}

func (g *Gateway) ginRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Gateway-native.
	r.GET("/clusters-health", httpHandler(g.handleClustersHealth))
	r.GET("/healthz", func(c *gin.Context) {
		writeJSON(c.Writer, http.StatusOK, map[string]any{"status": "ok", "backends": len(g.backends())})
	})
	r.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, openapi.GatewaySpec())
	})

	// Connection registry admin (only when a ConnStore is attached). These manage which
	// kvindexers the gateway federates — the inverse-topology control surface.
	if g.store != nil {
		r.GET("/admin/connections", httpHandler(g.handleListConnections))
		r.POST("/admin/connections", httpHandler(g.handleUpsertConnection))
		r.DELETE("/admin/connections/:id", httpHandler(g.handleDeleteConnection, "id"))
	}

	// Fan-out GET (array merge, cluster-tagged).
	r.GET("/clusters", httpHandler(g.fanoutList("/clusters", nil)))
	r.GET("/engines", httpHandler(g.fanoutList("/engines", nil)))
	r.GET("/model-profiles", httpHandler(g.fanoutList("/model-profiles", nil)))
	r.GET("/policies", httpHandler(g.fanoutList("/policies", nil)))
	r.GET("/event-streams", httpHandler(g.fanoutList("/event-streams", nil)))
	r.GET("/kv-events/recent", httpHandler(g.fanoutList("/kv-events/recent", sortByObservedAtAsc)))
	r.GET("/kv-events/stream", httpHandler(g.proxyStreamOne))
	r.GET("/index/stats", httpHandler(g.fanoutList("/index/stats", nil)))
	r.GET("/decisions", httpHandler(g.fanoutList("/decisions", sortByTimestampAsc)))
	r.GET("/config/audit-log", httpHandler(g.fanoutList("/config/audit-log", sortByVersionAsc)))

	// Aggregate object.
	r.GET("/config/versions", httpHandler(g.handleConfigVersions))

	// Single-backend writes (cluster/backend-targeted).
	r.POST("/clusters", httpHandler(g.proxyOne))
	r.PATCH("/clusters/:id", httpHandler(g.proxyOne, "id"))
	r.POST("/engines/register", httpHandler(g.proxyOne))
	r.POST("/engines/unregister", httpHandler(g.proxyOne))
	r.PATCH("/engines/:id", httpHandler(g.proxyOne, "id"))
	r.POST("/model-profiles", httpHandler(g.handleModelProfileUpsert))
	r.POST("/policies", httpHandler(g.proxyOne))
	r.PATCH("/policies/:id", httpHandler(g.proxyOne, "id"))
	r.DELETE("/policies/:id", httpHandler(g.proxyOne, "id"))

	// Single-backend admission + query (cluster-targeted).
	r.POST("/route", httpHandler(g.handleAdmission(types.ProtocolOpenAIChat)))
	r.POST("/v1/chat/completions", httpHandler(g.handleAdmission(types.ProtocolOpenAIChat)))
	r.POST("/v1/responses", httpHandler(g.handleAdmission(types.ProtocolOpenAIResponses)))
	r.POST("/v1/messages", httpHandler(g.handleAdmission(types.ProtocolAnthropic)))
	r.POST("/query-prefix", httpHandler(g.handleQueryPrefix))
	r.POST("/tokenize/preview", httpHandler(g.handleTokenizePreview))
	r.POST("/config/effective-policy/preview", httpHandler(g.proxyOne))

	return r
}

func httpHandler(h http.HandlerFunc, pathParams ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range pathParams {
			c.Request.SetPathValue(name, c.Param(name))
		}
		h(c.Writer, c.Request)
	}
}

// ---- connection registry admin ----

// handleListConnections returns all registered kvindexer connections. The token
// is redacted (replaced with whether one is set) so the secret never leaves the
// gateway via its own API.
func (g *Gateway) handleListConnections(w http.ResponseWriter, r *http.Request) {
	type view struct {
		ID       string `json:"id"`
		Cluster  string `json:"cluster"`
		URL      string `json:"url"`
		HasToken bool   `json:"has_token"`
		Enabled  bool   `json:"enabled"`
	}
	conns := g.store.List()
	out := make([]view, 0, len(conns))
	for _, c := range conns {
		out = append(out, view{ID: c.ID, Cluster: c.Cluster, URL: c.URL, HasToken: c.Token != "", Enabled: c.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUpsertConnection creates or updates a connection. Body is a Connection
// JSON ({id, cluster, url, token?, enabled?}). Omitted fields fall back to the
// existing row's values (so the redacted list can be round-tripped without
// re-sending the token), and a NEW connection defaults to enabled. `token` and
// `enabled` are decoded as pointers so "omitted" is distinguishable from
// "explicitly empty/false".
func (g *Gateway) handleUpsertConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string  `json:"id"`
		Cluster string  `json:"cluster"`
		URL     string  `json:"url"`
		Token   *string `json:"token"`
		Enabled *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Find the existing row (if any) to fill in omitted fields.
	var existing *Connection
	for _, ex := range g.store.List() {
		if ex.ID == in.ID {
			e := ex
			existing = &e
			break
		}
	}

	c := Connection{ID: in.ID, Cluster: in.Cluster, URL: in.URL}
	if existing != nil {
		if c.Cluster == "" {
			c.Cluster = existing.Cluster
		}
		if c.URL == "" {
			c.URL = existing.URL
		}
	}
	switch {
	case in.Token != nil:
		c.Token = *in.Token
	case existing != nil:
		c.Token = existing.Token // preserve existing secret
	}
	switch {
	case in.Enabled != nil:
		c.Enabled = *in.Enabled
	case existing != nil:
		c.Enabled = existing.Enabled // preserve existing state
	default:
		c.Enabled = true // new connections are enabled by default
	}

	if err := g.store.Upsert(c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": c.ID})
}

// handleDeleteConnection removes a connection by id.
func (g *Gateway) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := g.store.Delete(id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id})
}

// ---- post-merge sorters ----

func sortByTimestampAsc(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return asString(items[i]["timestamp"]) < asString(items[j]["timestamp"])
	})
}

func sortByObservedAtAsc(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return asString(items[i]["observed_at"]) < asString(items[j]["observed_at"])
	})
}

func sortByVersionAsc(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return asFloat(items[i]["version"]) < asFloat(items[j]["version"])
	})
}

// ---- helpers ----

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "X-KVI-Backend, X-KVI-Cluster")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": msg}})
}

func errString(err error, status int, body []byte) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("status %d: %s", status, truncate(body))
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func truncate(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
