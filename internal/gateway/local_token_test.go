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
