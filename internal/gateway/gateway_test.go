package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeBackend spins up an httptest server emulating a kvindexer's relevant routes.
func fakeBackend(t *testing.T, cluster string, engines []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /engines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, engines)
	})
	mux.HandleFunc("POST /engines/register", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		m["_registered_in"] = cluster
		writeJSON(w, http.StatusOK, m)
	})
	mux.HandleFunc("GET /config/versions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"config_version": 7})
	})
	return httptest.NewServer(mux)
}

func newTestGateway(t *testing.T) (*Gateway, func()) {
	t.Helper()
	a := fakeBackend(t, "h20-1", []map[string]any{{"engine_id": "h20-1-eng-0"}})
	b := fakeBackend(t, "h20-1", []map[string]any{{"engine_id": "h20-1-eng-1"}})
	c := fakeBackend(t, "h200-1", []map[string]any{{"engine_id": "h200-1-eng-0"}})
	g := New([]Backend{
		{ID: "h20-1-0", Cluster: "h20-1", URL: a.URL},
		{ID: "h20-1-1", Cluster: "h20-1", URL: b.URL},
		{ID: "h200-1-0", Cluster: "h200-1", URL: c.URL},
	}, time.Now)
	return g, func() { a.Close(); b.Close(); c.Close() }
}

func doJSON(t *testing.T, h http.Handler, method, target, body string) (int, string) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestFanoutAllClustersTagged(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()

	code, body := doJSON(t, h, "GET", "/engines", "")
	if code != 200 {
		t.Fatalf("status %d body %s", code, body)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 engines across all clusters, got %d: %s", len(items), body)
	}
	clusters := map[string]int{}
	for _, it := range items {
		if it["_cluster"] == nil || it["_backend"] == nil {
			t.Fatalf("element not tagged with _cluster/_backend: %v", it)
		}
		clusters[it["_cluster"].(string)]++
	}
	if clusters["h20-1"] != 2 || clusters["h200-1"] != 1 {
		t.Fatalf("cluster tag counts wrong: %v", clusters)
	}
}

func TestFanoutClusterFilter(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()

	code, body := doJSON(t, h, "GET", "/engines?cluster=h20-1", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var items []map[string]any
	json.Unmarshal([]byte(body), &items)
	if len(items) != 2 {
		t.Fatalf("want 2 h20-1 engines, got %d: %s", len(items), body)
	}
	for _, it := range items {
		if it["_cluster"] != "h20-1" {
			t.Fatalf("leaked non-h20-1 engine: %v", it)
		}
	}
}

func TestProxyOneRequiresUnambiguousTarget(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()

	// No selector + 3 backends => ambiguous => 400.
	code, _ := doJSON(t, h, "POST", "/engines/register", `{"engine_id":"x"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("want 400 for ambiguous write, got %d", code)
	}

	// cluster=h20-1 still has 2 backends => ambiguous => 400.
	code, _ = doJSON(t, h, "POST", "/engines/register?cluster=h20-1", `{"engine_id":"x"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("want 400 for 2-backend cluster, got %d", code)
	}

	// backend=h200-1-0 resolves to exactly one => 200, routed to h200-1.
	code, body := doJSON(t, h, "POST", "/engines/register?backend=h200-1-0", `{"engine_id":"x"}`)
	if code != 200 {
		t.Fatalf("want 200 for unambiguous write, got %d body %s", code, body)
	}
	var m map[string]any
	json.Unmarshal([]byte(body), &m)
	if m["_registered_in"] != "h200-1" {
		t.Fatalf("write landed in wrong cluster: %v", m)
	}

	// cluster with a single backend resolves too.
	code, _ = doJSON(t, h, "POST", "/engines/register?cluster=h200-1", `{"engine_id":"x"}`)
	if code != 200 {
		t.Fatalf("want 200 for single-backend cluster, got %d", code)
	}
}

func TestProxyOneUnknownTarget(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()
	code, _ := doJSON(t, h, "POST", "/engines/register?cluster=nowhere", `{"engine_id":"x"}`)
	if code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown cluster, got %d", code)
	}
}

func TestClustersHealth(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()

	for _, path := range []string{"/clusters-health"} {
		code, body := doJSON(t, h, "GET", path, "")
		if code != 200 {
			t.Fatalf("%s status %d", path, code)
		}
		var clusters []clusterInfo
		if err := json.Unmarshal([]byte(body), &clusters); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(clusters) != 2 {
			t.Fatalf("%s: want 2 clusters, got %d: %s", path, len(clusters), body)
		}
		for _, c := range clusters {
			for _, b := range c.Backends {
				if !b.Healthy {
					t.Fatalf("backend %s should be healthy: %+v", b.ID, b)
				}
			}
		}
	}
}

func TestAdminConnectionsCRUDPreservesOmittedFields(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	h := NewWithStore(store, time.Now).Router()

	code, body := doJSON(t, h, "POST", "/admin/connections", `{
		"id":"idx-0",
		"cluster":"hkg-vllm",
		"url":"http://127.0.0.1:8090",
		"token":"secret-token",
		"enabled":true
	}`)
	if code != http.StatusOK {
		t.Fatalf("create status %d body %s", code, body)
	}

	code, body = doJSON(t, h, "POST", "/admin/connections", `{"id":"idx-0","enabled":false}`)
	if code != http.StatusOK {
		t.Fatalf("partial update status %d body %s", code, body)
	}

	conns := store.List()
	if len(conns) != 1 {
		t.Fatalf("connections=%d want 1", len(conns))
	}
	if conns[0].Cluster != "hkg-vllm" || conns[0].URL != "http://127.0.0.1:8090" || conns[0].Token != "secret-token" {
		t.Fatalf("partial update did not preserve existing fields: %+v", conns[0])
	}
	if conns[0].Enabled {
		t.Fatalf("enabled should be false after partial update: %+v", conns[0])
	}

	code, body = doJSON(t, h, "GET", "/admin/connections", "")
	if code != http.StatusOK {
		t.Fatalf("list status %d body %s", code, body)
	}
	var listed []map[string]any
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0]["has_token"] != true {
		t.Fatalf("list should redact token and expose has_token: %s", body)
	}
	if _, leaked := listed[0]["token"]; leaked {
		t.Fatalf("list leaked token: %s", body)
	}

	code, body = doJSON(t, h, "DELETE", "/admin/connections/idx-0", "")
	if code != http.StatusOK {
		t.Fatalf("delete status %d body %s", code, body)
	}
	if store.Count() != 0 {
		t.Fatalf("delete left %d rows", store.Count())
	}
}

func TestVirtualConnectionDoesNotRequireURL(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	h := NewWithStore(store, time.Now).Router()

	code, body := doJSON(t, h, http.MethodPost, "/admin/connections", `{
		"id":"virt-0",
		"kind":"virtual",
		"cluster":"local-tokenizer",
		"display_name":"Local Tokenizer",
		"enabled":true
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual create status %d body %s", code, body)
	}

	conns := store.List()
	if len(conns) != 1 {
		t.Fatalf("connections=%d want 1", len(conns))
	}
	if conns[0].Kind != ConnectionKindVirtual || conns[0].URL != "" || conns[0].Cluster != "local-tokenizer" {
		t.Fatalf("virtual connection not stored correctly: %+v", conns[0])
	}
	if got := store.Backends(); len(got) != 0 {
		t.Fatalf("virtual connection should not become a real backend: %+v", got)
	}

	code, body = doJSON(t, h, http.MethodPost, "/admin/connections", `{
		"id":"bad-backend",
		"kind":"backend",
		"cluster":"local-tokenizer"
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("backend without url status %d body %s", code, body)
	}
}

func TestVirtualConnectionHealthAndModelProfiles(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model_id":                "local-model",
			"chat_template_sha256":    "",
			"tokenizer_dir":           "/tokenizers/local-model",
			"tokenizer_config_sha256": "",
		})
	}))
	defer local.Close()

	g := NewWithStore(store, time.Now)
	g.SetLocalTokenizerURL(local.URL)
	h := g.Router()

	code, body := doJSON(t, h, http.MethodPost, "/admin/connections", `{
		"id":"virt-0",
		"kind":"virtual",
		"cluster":"local-tokenizer",
		"display_name":"Local Tokenizer",
		"enabled":true
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual connection create status %d body %s", code, body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/clusters-health", "")
	if code != http.StatusOK {
		t.Fatalf("clusters-health status %d body %s", code, body)
	}
	var health []clusterInfo
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatal(err)
	}
	if len(health) != 1 || health[0].Cluster != "local-tokenizer" || len(health[0].Backends) != 1 {
		t.Fatalf("virtual health missing cluster/backend: %+v", health)
	}
	if !health[0].Backends[0].Healthy || !health[0].Backends[0].Virtual {
		t.Fatalf("virtual health should be healthy and tagged virtual: %+v", health[0].Backends[0])
	}

	code, body = doJSON(t, h, http.MethodPost, "/model-profiles?backend=virt-0", `{
		"model_id":"local-model",
		"framework":"sglang",
		"tokenizer_mode":"remote",
		"hash_profile":"sglang-v1-text",
		"block_size":16,
		"hash_seed":"0"
	}`)
	if code != http.StatusBadRequest {
		t.Fatalf("remote virtual profile status %d body %s", code, body)
	}

	_, err := store.PutTokenizerAsset(t.Context(), TokenizerAssetInput{
		Cluster:      "local-tokenizer",
		ModelID:      "/mnt/local-model",
		TokenizerZip: []byte("zip-bytes"),
	})
	if err != nil {
		t.Fatalf("put tokenizer asset: %v", err)
	}
	code, body = doJSON(t, h, http.MethodPost, "/model-profiles?backend=virt-0", `{
		"model_id":"/mnt/local-model",
		"framework":"sglang",
		"tokenizer_mode":"local",
		"hash_profile":"sglang-v1-text",
		"block_size":16,
		"hash_seed":"0"
	}`)
	if code != http.StatusOK {
		t.Fatalf("local virtual profile status %d body %s", code, body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/model-profiles?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("list virtual profiles status %d body %s", code, body)
	}
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(body), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles=%d want 1 body %s", len(profiles), body)
	}
	if profiles[0]["model_id"] != "/mnt/local-model" || profiles[0]["_cluster"] != "local-tokenizer" ||
		profiles[0]["_backend"] != "virt-0" || profiles[0]["_virtual"] != true {
		t.Fatalf("virtual profile missing tags: %+v", profiles[0])
	}

	code, body = doJSON(t, h, http.MethodDelete, "/model-profiles/%2Fmnt%2Flocal-model?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("delete virtual profile status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/model-profiles?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("list virtual profiles after delete status %d body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Fatalf("virtual profile delete left rows: %+v", profiles)
	}

	_, err = store.PutTokenizerAsset(t.Context(), TokenizerAssetInput{
		Cluster:      "local-tokenizer",
		ModelID:      "glm-5.1",
		TokenizerZip: []byte("zip-bytes"),
	})
	if err != nil {
		t.Fatalf("put ordinary tokenizer asset: %v", err)
	}
	code, body = doJSON(t, h, http.MethodPost, "/model-profiles?backend=virt-0", `{
		"model_id":"glm-5.1",
		"framework":"vllm",
		"tokenizer_mode":"local",
		"hash_profile":"vllm-v1-text",
		"block_size":16,
		"hash_seed":"0"
	}`)
	if code != http.StatusOK {
		t.Fatalf("ordinary virtual profile status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodDelete, "/model-profiles/glm-5.1?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("delete ordinary virtual profile status %d body %s", code, body)
	}
}

func TestVirtualPolicyPatchAndDelete(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	h := NewWithStore(store, time.Now).Router()

	code, body := doJSON(t, h, http.MethodPost, "/admin/connections", `{
		"id":"virt-0",
		"kind":"virtual",
		"cluster":"local-tokenizer",
		"enabled":true
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual connection create status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodPost, "/policies?backend=virt-0", `{
		"rule_id":"limit",
		"priority":10,
		"action":{"type":"accept"}
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual policy create status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodPost, "/config/effective-policy/preview?backend=virt-0", `{
		"cluster_id":"local-tokenizer",
		"model_id":"local-model",
		"input_tokens":4
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual policy preview status %d body %s", code, body)
	}
	if !strings.Contains(body, `"matched_rule_id":"limit"`) {
		t.Fatalf("virtual policy preview should evaluate virtual rule: %s", body)
	}
	code, body = doJSON(t, h, http.MethodPatch, "/policies/limit?backend=virt-0", `{
		"rule_id":"ignored",
		"name":"Token limit",
		"priority":20,
		"action":{"type":"reject","reject_status":429},
		"enabled":false
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual policy patch status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/policies?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("virtual policy list status %d body %s", code, body)
	}
	var policies []map[string]any
	if err := json.Unmarshal([]byte(body), &policies); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || policies[0]["rule_id"] != "limit" ||
		policies[0]["name"] != "Token limit" || policies[0]["priority"].(float64) != 20 ||
		policies[0]["enabled"] != false {
		t.Fatalf("virtual policy patch not reflected: %+v", policies)
	}
	code, body = doJSON(t, h, http.MethodDelete, "/policies/limit?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("virtual policy delete status %d body %s", code, body)
	}
	code, body = doJSON(t, h, http.MethodGet, "/policies?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("virtual policy list after delete status %d body %s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &policies); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 0 {
		t.Fatalf("virtual policy delete left rows: %+v", policies)
	}
}

func TestConfigVersionsAggregate(t *testing.T) {
	g, cleanup := newTestGateway(t)
	defer cleanup()
	h := g.Router()
	code, body := doJSON(t, h, "GET", "/config/versions", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var out []map[string]any
	json.Unmarshal([]byte(body), &out)
	if len(out) != 3 {
		t.Fatalf("want 3 per-backend versions, got %d: %s", len(out), body)
	}
	for _, v := range out {
		if v["config_version"].(float64) != 7 {
			t.Fatalf("unexpected config_version: %v", v)
		}
	}
}
