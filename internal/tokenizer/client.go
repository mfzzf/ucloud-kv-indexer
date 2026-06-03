// Package tokenizer is a thin HTTP client to the TARGET engine's tokenizer
// endpoint (vLLM/SGLang /tokenize). It NEVER tokenizes locally and never
// re-implements a chat template: it forwards messages/prompt/tools verbatim and
// trusts only the returned token IDs and count. This keeps tokenization on the
// engine side where the authoritative chat template lives.
package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ucloud/kv-indexer/internal/types"
)

// Client calls a vLLM/SGLang-compatible /tokenize endpoint.
type Client struct {
	HTTP *http.Client
}

// New returns a Client with a sane default timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}}
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

// normalizeEndpoint ensures we hit the /tokenize path of the given base URL.
func normalizeEndpoint(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/tokenize") || strings.HasSuffix(base, "/v1/tokenize") {
		return base
	}
	return base + "/tokenize"
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

// TokenizeCompletion sends the prompt form.
func (c *Client) TokenizeCompletion(ctx context.Context, endpoint, model, prompt string) (*Result, error) {
	body := completionRequest{Model: model, Prompt: prompt, AddSpecialTokens: true}
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
		return nil, fmt.Errorf("tokenize endpoint status %d: %s", resp.StatusCode, truncate(string(data), 200))
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
