package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayLocalTokenizerModeSkipsBackendRouteAndCachePolicy(t *testing.T) {
	var backendRouteCalls atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"model_id":      "local-model",
				"tokenizer_dir": "/tokenizers/local-model",
			})
		case "/tokenize":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			var words []string
			for _, msg := range req.Messages {
				if s, ok := msg.Content.(string); ok {
					words = append(words, strings.Fields(s)...)
				}
			}
			tokens := make([]int, len(words))
			for i := range tokens {
				tokens[i] = i + 1
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens, "count": len(tokens)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer local.Close()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/model-profiles":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"model_id":       "local-model",
				"framework":      "sglang",
				"version":        1,
				"tokenizer_mode": "local",
				"hash_profile":   "sglang-v1-text",
				"block_size":     16,
				"hash_seed":      "0",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/policies":
			minHit := 0.9
			writeJSON(w, http.StatusOK, []map[string]any{
				{
					"rule_id":  "cache-hit-would-reject-if-not-filtered",
					"priority": 100,
					"conditions": []map[string]any{{
						"field": "input_tokens",
						"op":    "gte",
						"value": 1,
					}},
					"action": map[string]any{"type": "require_cache_hit", "min_hit_ratio": minHit},
				},
				{
					"rule_id":  "token-limit",
					"priority": 50,
					"conditions": []map[string]any{{
						"field": "input_tokens",
						"op":    "gt",
						"value": 3,
					}},
					"action": map[string]any{"type": "reject", "reject_status": http.StatusTooManyRequests},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/engines":
			writeJSON(w, http.StatusOK, []map[string]any{{
				"engine_id":     "e0",
				"cluster_id":    "c0",
				"api_endpoint":  "http://engine",
				"served_models": []string{"local-model"},
				"enabled":       true,
				"healthy":       true,
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/config/versions":
			writeJSON(w, http.StatusOK, map[string]any{"config_version": 7})
		case r.Method == http.MethodGet && r.URL.Path == "/decisions":
			writeJSON(w, http.StatusOK, []map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			backendRouteCalls.Add(1)
			writeJSON(w, http.StatusOK, map[string]any{"unexpected": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	store := NewMemoryStore()
	if _, err := store.SeedIfEmpty([]Connection{{ID: "idx-0", Cluster: "c0", URL: backend.URL, Enabled: true}}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if _, err := store.PutTokenizerAsset(t.Context(), TokenizerAssetInput{
		Cluster:      "c0",
		ModelID:      "local-model",
		TokenizerZip: []byte("zip-bytes"),
	}); err != nil {
		t.Fatalf("put tokenizer asset: %v", err)
	}
	g := NewWithStore(store, time.Now)
	g.SetLocalTokenizerURL(local.URL)
	h := g.Router()

	code, body := doJSON(t, h, http.MethodPost, "/v1/chat/completions?cluster=c0", `{
		"model":"local-model",
		"messages":[{"role":"user","content":"one two three"}]
	}`)
	if code != http.StatusOK {
		t.Fatalf("short local request status=%d body=%s", code, body)
	}
	if !strings.Contains(body, `"decision":"accept"`) {
		t.Fatalf("short request should be accepted after cache policy is filtered: %s", body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/v1/chat/completions?cluster=c0", `{
		"model":"local-model",
		"messages":[{"role":"user","content":"one two three four"}]
	}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("long local request status=%d body=%s", code, body)
	}
	if !strings.Contains(body, `"matched_rule_id":"token-limit"`) {
		t.Fatalf("long request should match token-limit rule: %s", body)
	}
	if got := backendRouteCalls.Load(); got != 0 {
		t.Fatalf("backend route should not be called in local mode, got %d calls", got)
	}

	code, body = doJSON(t, h, http.MethodGet, "/decisions?cluster=c0", "")
	if code != http.StatusOK {
		t.Fatalf("decisions status=%d body=%s", code, body)
	}
	var decisions []map[string]any
	if err := json.Unmarshal([]byte(body), &decisions); err != nil {
		t.Fatalf("decode decisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("gateway should expose local-tokenizer decisions, got %d: %s", len(decisions), body)
	}
	for _, rec := range decisions {
		if rec["source"] != "gateway_local_tokenizer" || rec["_cluster"] != "c0" || rec["_backend"] != "idx-0" {
			t.Fatalf("local decision missing gateway tags/source: %v", rec)
		}
	}
	accepted := decisions[0]
	rejected := decisions[1]
	if accepted["decision"] != "accept" || accepted["target_engine"] != "e0" || accepted["input_tokens"].(float64) != 3 {
		t.Fatalf("accepted local decision missing target/token count: %v", accepted)
	}
	if rejected["decision"] != "reject" || rejected["input_tokens"].(float64) != 4 {
		t.Fatalf("rejected local decision missing token count: %v", rejected)
	}
}

func TestGatewayVirtualLocalTokenizerAdmission(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"model_id":      "local-model",
				"tokenizer_dir": "/tokenizers/local-model",
			})
		case "/tokenize":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			var words []string
			for _, msg := range req.Messages {
				if s, ok := msg.Content.(string); ok {
					words = append(words, strings.Fields(s)...)
				}
			}
			tokens := make([]int, len(words))
			for i := range tokens {
				tokens[i] = i + 1
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens, "count": len(tokens)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer local.Close()

	store := NewMemoryStore()
	defer store.Close()
	g := NewWithStore(store, time.Now)
	g.SetLocalTokenizerURL(local.URL)
	h := g.Router()

	code, body := doJSON(t, h, http.MethodPost, "/admin/connections", `{
		"id":"virt-0",
		"kind":"virtual",
		"cluster":"local-tokenizer",
		"enabled":true
	}`)
	if code != http.StatusOK {
		t.Fatalf("create virtual connection status=%d body=%s", code, body)
	}
	if _, err := store.PutTokenizerAsset(t.Context(), TokenizerAssetInput{
		Cluster:      "local-tokenizer",
		ModelID:      "local-model",
		TokenizerZip: []byte("zip-bytes"),
	}); err != nil {
		t.Fatalf("put tokenizer asset: %v", err)
	}

	code, body = doJSON(t, h, http.MethodPost, "/model-profiles?backend=virt-0", `{
		"model_id":"local-model",
		"framework":"sglang",
		"tokenizer_mode":"local",
		"hash_profile":"sglang-v1-text",
		"block_size":16,
		"hash_seed":"0"
	}`)
	if code != http.StatusOK {
		t.Fatalf("create virtual profile status=%d body=%s", code, body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/policies?backend=virt-0", `{
		"rule_id":"token-limit",
		"priority":50,
		"conditions":[{"field":"input_tokens","op":"gt","value":3}],
		"action":{"type":"reject","reject_status":429}
	}`)
	if code != http.StatusOK {
		t.Fatalf("create virtual policy status=%d body=%s", code, body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/tokenize/preview?backend=virt-0", `{
		"model":"local-model",
		"protocol":"openai.chat",
		"raw":{"model":"local-model","messages":[{"role":"user","content":"one two three"}]}
	}`)
	if code != http.StatusOK {
		t.Fatalf("virtual tokenize preview status=%d body=%s", code, body)
	}
	var preview tokenizePreviewResponse
	if err := json.Unmarshal([]byte(body), &preview); err != nil {
		t.Fatalf("decode virtual preview: %v", err)
	}
	if preview.Model != "local-model" || preview.Namespace != "local-model/v1/sglang-v1-text/16" ||
		preview.BlockSize != 16 || preview.Count != 3 || len(preview.Tokens) != 3 {
		t.Fatalf("unexpected virtual preview: %+v", preview)
	}

	code, body = doJSON(t, h, http.MethodPost, "/v1/chat/completions?backend=virt-0", `{
		"model":"local-model",
		"messages":[{"role":"user","content":"one two three"}]
	}`)
	if code != http.StatusOK {
		t.Fatalf("short virtual request status=%d body=%s", code, body)
	}
	if !strings.Contains(body, `"decision":"accept"`) || strings.Contains(body, `"target"`) {
		t.Fatalf("short virtual request should accept without real target: %s", body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/v1/chat/completions?backend=virt-0", `{
		"model":"local-model",
		"messages":[{"role":"user","content":"one two three four"}]
	}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("long virtual request status=%d body=%s", code, body)
	}
	if !strings.Contains(body, `"matched_rule_id":"token-limit"`) {
		t.Fatalf("long virtual request should match token-limit: %s", body)
	}

	code, body = doJSON(t, h, http.MethodPost, "/query-prefix?backend=virt-0", `{"model":"local-model"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "virtual clusters do not support") {
		t.Fatalf("virtual query-prefix status=%d body=%s", code, body)
	}

	code, body = doJSON(t, h, http.MethodGet, "/decisions?backend=virt-0", "")
	if code != http.StatusOK {
		t.Fatalf("virtual decisions status=%d body=%s", code, body)
	}
	var decisions []map[string]any
	if err := json.Unmarshal([]byte(body), &decisions); err != nil {
		t.Fatalf("decode virtual decisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("gateway should expose virtual decisions, got %d: %s", len(decisions), body)
	}
	for _, rec := range decisions {
		if rec["source"] != "gateway_virtual_tokenizer" || rec["_cluster"] != "local-tokenizer" || rec["_backend"] != "virt-0" {
			t.Fatalf("virtual decision missing tags/source: %v", rec)
		}
	}
}
