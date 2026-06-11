package tokenizer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ucloud/kv-indexer/internal/normalize"
	"github.com/ucloud/kv-indexer/internal/types"
)

// fakeTokenizeServer echoes back a fixed token list and captures the request
// body it received so we can assert the exact wire shape we sent.
func fakeTokenizeServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*captured = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tokens":[1,2,3],"count":3,"max_model_len":8192}`))
	}))
}

func TestTokenizeChatSGLangUsesV1TokenizeByDefault(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tokens":[1],"count":1,"max_model_len":8192}`))
	}))
	defer srv.Close()

	cli := New()
	_, err := cli.TokenizeChatSGLang(context.Background(), srv.URL, "m", []types.ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/tokenize" {
		t.Fatalf("SGLang chat tokenize path: got %q, want /v1/tokenize", gotPath)
	}
}

func TestTokenizeChatLegacySGLangErrorIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"object":"error","message":"1 validation error:\n  {'type': 'missing', 'loc': ('body', 'prompt'), 'msg': 'Field required'}","type":"BadRequestError","code":400}`))
	}))
	defer srv.Close()

	cli := New()
	_, err := cli.TokenizeChatSGLang(context.Background(), srv.URL, "m", []types.ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"v0.5.12+", "27445f9836", "/v1/tokenize accepts messages"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

func TestTokenizeFullErrorBodyCanBeReturned(t *testing.T) {
	const tail = "tail-marker-after-default-truncation"
	longBody := `{"object":"error","message":"` + strings.Repeat("x", 260) + tail + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(longBody))
	}))
	defer srv.Close()

	cli := New()
	cli.FullErrorBody = true
	_, err := cli.TokenizeChatSGLang(context.Background(), srv.URL, "m", []types.ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), tail) {
		t.Fatalf("full error body missing tail marker:\n%s", err)
	}
}

// TestTokenizeChatForwardsStructuredAnthropic verifies that an Anthropic
// multi-turn tool-use conversation reaches /v1/tokenize as STRUCTURED OpenAI
// chat messages — content arrays, assistant tool_calls (arguments as a JSON
// string), and a separate tool-role message — not flattened strings.
func TestTokenizeChatForwardsStructuredAnthropic(t *testing.T) {
	var captured []byte
	srv := fakeTokenizeServer(t, &captured)
	defer srv.Close()

	rr, err := normalize.FromAnthropic([]byte(`{
		"model":"qwen3.5-4b","max_tokens":16,
		"system":"be brief",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"weather in SF?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"get_weather","input":{"city":"SF"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"sunny"}]}
		],
		"tools":[{"name":"get_weather","description":"w","input_schema":{"type":"object"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	cli := New()
	res, err := cli.TokenizeChat(context.Background(), srv.URL, "qwen3.5-4b", rr.Messages, rr.Tools, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 3 {
		t.Fatalf("count: got %d", res.Count)
	}

	// Decode what the engine actually received.
	var got struct {
		Model               string `json:"model"`
		AddGenerationPrompt bool   `json:"add_generation_prompt"`
		Messages            []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(captured, &got); err != nil {
		t.Fatalf("captured body not valid JSON: %v\n%s", err, captured)
	}

	if !got.AddGenerationPrompt {
		t.Fatalf("add_generation_prompt should be true")
	}
	if len(got.Messages) != 4 {
		t.Fatalf("expected 4 messages [system,user,assistant,tool], got %d: %s", len(got.Messages), captured)
	}

	// system
	if got.Messages[0].Role != "system" {
		t.Fatalf("msg0 role: %q", got.Messages[0].Role)
	}

	// user: content must be a JSON ARRAY (text + image_url), not a string.
	if got.Messages[1].Content[0] != '[' {
		t.Fatalf("user content should be an array, got: %s", got.Messages[1].Content)
	}

	// assistant: tool_calls with arguments as a JSON STRING.
	asst := got.Messages[2]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls missing: %+v", asst)
	}
	if asst.ToolCalls[0].Function.Arguments != `{"city":"SF"}` {
		t.Fatalf("arguments must be a JSON string: %q", asst.ToolCalls[0].Function.Arguments)
	}
	// Tool-call-only assistant message must omit content.
	if len(asst.Content) != 0 {
		t.Fatalf("assistant content should be omitted, got: %s", asst.Content)
	}

	// tool: separate role with tool_call_id.
	tool := got.Messages[3]
	if tool.Role != "tool" || tool.ToolCallID != "tu1" {
		t.Fatalf("tool message wrong: %+v", tool)
	}

	// tools forwarded as OpenAI function tools.
	if len(got.Tools) != 1 || got.Tools[0]["type"] != "function" {
		t.Fatalf("tools not forwarded as function tools: %+v", got.Tools)
	}
}

// TestTokenizeChatForwardsStructuredResponses verifies the Responses path:
// a function_call + function_call_output become an assistant tool_calls message
// and a tool-role message, with arguments passed through verbatim.
func TestTokenizeChatForwardsStructuredResponses(t *testing.T) {
	var captured []byte
	srv := fakeTokenizeServer(t, &captured)
	defer srv.Close()

	rr, err := normalize.FromOpenAIResponses([]byte(`{
		"model":"m","instructions":"sys",
		"input":[
			{"role":"user","content":"weather?"},
			{"type":"function_call","call_id":"c1","name":"w","arguments":"{\"city\":\"SF\"}"},
			{"type":"function_call_output","call_id":"c1","output":"sunny"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	cli := New()
	if _, err := cli.TokenizeChat(context.Background(), srv.URL, "m", rr.Messages, rr.Tools, nil); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Messages []struct {
			Role      string `json:"role"`
			ToolCalls []struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured, &got); err != nil {
		t.Fatal(err)
	}
	// [system, user, assistant(tool_calls), tool]
	if len(got.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", len(got.Messages), captured)
	}
	if got.Messages[2].Role != "assistant" || len(got.Messages[2].ToolCalls) != 1 {
		t.Fatalf("function_call should be assistant tool_calls: %s", captured)
	}
	if got.Messages[2].ToolCalls[0].Function.Arguments != `{"city":"SF"}` {
		t.Fatalf("arguments altered: %q", got.Messages[2].ToolCalls[0].Function.Arguments)
	}
	if got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "c1" {
		t.Fatalf("function_call_output should be tool message: %s", captured)
	}
}
