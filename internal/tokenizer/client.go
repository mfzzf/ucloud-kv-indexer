// Package tokenizer is a thin HTTP client to tokenizer endpoints: either the
// target engine's vLLM/SGLang /tokenize API or the gateway-local Python
// sidecar. It never re-implements a chat template in Go; it forwards
// messages/prompt/tools verbatim and trusts only the returned token IDs/count.
package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ucloud/kv-indexer/internal/types"
)

// Client calls a vLLM/SGLang-compatible /tokenize endpoint.
type Client struct {
	HTTP          *http.Client
	Debug         bool
	FullErrorBody bool
}

// New returns a Client with a sane default timeout.
func New() *Client {
	debug := envBool("KVINDEXER_TOKENIZER_DEBUG")
	return &Client{
		HTTP:          &http.Client{Timeout: 10 * time.Second},
		Debug:         debug,
		FullErrorBody: debug || envBool("KVINDEXER_TOKENIZER_FULL_ERROR"),
	}
}

// Result is the trusted output of tokenization.
type Result struct {
	Tokens      []int32
	Count       int
	MaxModelLen int
}

// vLLM/SGLang TokenizeResponse: {count, max_model_len, tokens, token_strs}.
type tokenizeResponse struct {
	Count       int     `json:"count"`
	MaxModelLen int     `json:"max_model_len"`
	Tokens      []int32 `json:"tokens"`
}

// chatRequest mirrors vLLM TokenizeChatRequest (chat form).
type chatRequest struct {
	Model                string              `json:"model,omitempty"`
	Messages             []types.ChatMessage `json:"messages"`
	Tools                []any               `json:"tools,omitempty"`
	AddGenerationPrompt  bool                `json:"add_generation_prompt"`
	ContinueFinalMessage bool                `json:"continue_final_message,omitempty"`
	ChatTemplateKwargs   map[string]any      `json:"chat_template_kwargs,omitempty"`
}

// completionRequest mirrors vLLM TokenizeCompletionRequest (prompt form).
type completionRequest struct {
	Model            string `json:"model,omitempty"`
	Prompt           string `json:"prompt"`
	AddSpecialTokens bool   `json:"add_special_tokens"`
}

// normalizeEndpoint ensures we hit the vLLM-compatible /tokenize path of the
// given base URL.
func normalizeEndpoint(base string) string {
	return normalizeEndpointPath(base, "/tokenize")
}

// normalizeOpenAIEndpoint ensures we hit SGLang's documented OpenAI
// /v1/tokenize path of the given base URL.
func normalizeOpenAIEndpoint(base string) string {
	return normalizeEndpointPath(base, "/v1/tokenize")
}

func normalizeEndpointPath(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/tokenize") || strings.HasSuffix(base, "/v1/tokenize") {
		return base
	}
	return base + path
}

// TokenizeChat sends the chat form. chatTemplateKwargs may be nil.
func (c *Client) TokenizeChat(ctx context.Context, endpoint, model string, messages []types.ChatMessage, tools []any, chatTemplateKwargs map[string]any) (*Result, error) {
	body := chatRequest{
		Model:               model,
		Messages:            messages,
		Tools:               tools,
		AddGenerationPrompt: true,
		ChatTemplateKwargs:  chatTemplateKwargs,
	}
	return c.post(ctx, normalizeEndpoint(endpoint), body)
}

// TokenizeChatSGLang sends the chat form to SGLang's OpenAI-compatible
// /v1/tokenize endpoint. SGLang also aliases /tokenize in recent builds, but
// /v1/tokenize is the tested/documented chat-tokenize route.
func (c *Client) TokenizeChatSGLang(ctx context.Context, endpoint, model string, messages []types.ChatMessage, tools []any, chatTemplateKwargs map[string]any) (*Result, error) {
	body := chatRequest{
		Model:               model,
		Messages:            messages,
		Tools:               tools,
		AddGenerationPrompt: true,
		ChatTemplateKwargs:  chatTemplateKwargs,
	}
	return c.post(ctx, normalizeOpenAIEndpoint(endpoint), body)
}

// TokenizeCompletion sends the prompt form.
func (c *Client) TokenizeCompletion(ctx context.Context, endpoint, model, prompt string) (*Result, error) {
	body := completionRequest{Model: model, Prompt: prompt, AddSpecialTokens: true}
	return c.post(ctx, normalizeEndpoint(endpoint), body)
}

// TokenizeLocalChat sends chat-form input to the gateway-local Python
// tokenizer sidecar. The sidecar exposes /tokenize, independent of serving
// framework, so never use the SGLang /v1/tokenize variant here.
func (c *Client) TokenizeLocalChat(ctx context.Context, endpoint, model string, messages []types.ChatMessage, tools []any) (*Result, error) {
	body := chatRequest{
		Model:               model,
		Messages:            messages,
		Tools:               tools,
		AddGenerationPrompt: true,
	}
	return c.post(ctx, normalizeEndpoint(endpoint), body)
}

func (c *Client) post(ctx context.Context, url string, body any) (*Result, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tokenize request: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		if c.Debug {
			log.Printf("tokenize endpoint error url=%s status=%d request_body=%s response_body=%s", url, resp.StatusCode, string(b), string(data))
		}
		return nil, tokenizeStatusError(resp.StatusCode, data, b, c.FullErrorBody)
	}
	var tr tokenizeResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return nil, fmt.Errorf("decode tokenize response: %w", err)
	}
	// Trust count if present, else len(tokens).
	cnt := tr.Count
	if cnt == 0 {
		cnt = len(tr.Tokens)
	}
	return &Result{Tokens: tr.Tokens, Count: cnt, MaxModelLen: tr.MaxModelLen}, nil
}

func tokenizeStatusError(status int, data, requestBody []byte, fullBody bool) error {
	body := string(data)
	if !fullBody {
		body = truncate(body, 200)
	}
	msg := fmt.Sprintf("tokenize endpoint status %d: %s", status, body)
	if looksLikeLegacySGLangChatTokenize(data, requestBody) {
		msg += " (SGLang rejected chat-form tokenize: this engine appears to require prompt-only /tokenize. Upgrade SGLang to v0.5.12+ or a build containing commit 27445f9836 / PR #23981 so /v1/tokenize accepts messages.)"
	}
	return fmt.Errorf("%s", msg)
}

func looksLikeLegacySGLangChatTokenize(data, requestBody []byte) bool {
	if !bytes.Contains(requestBody, []byte(`"messages"`)) {
		return false
	}
	lower := bytes.ToLower(data)
	return bytes.Contains(lower, []byte("prompt")) &&
		(bytes.Contains(lower, []byte("field required")) || bytes.Contains(lower, []byte("missing")))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}
