// Package normalize converts the three supported inbound protocols (OpenAI
// chat completions, OpenAI responses, Anthropic messages) into a common
// RouteRequest holding STRUCTURED OpenAI-chat messages and OpenAI-function
// tools. The message list is forwarded VERBATIM to the target engine's
// /v1/tokenize chat endpoint; the engine applies the authoritative chat
// template. We never re-implement a chat template here — we only re-shape
// protocol-specific system-prompt placement, content-part arrays, tool calls,
// and tool results into the single OpenAI-chat wire shape the engine accepts.
//
// # Framework-specific adapters
//
// The OpenAI Chat and OpenAI Responses conversions are framework-independent.
// The Anthropic Messages conversion, however, DIFFERS between SGLang and vLLM
// (both ship their own Anthropic→chat converter, and they disagree on system
// concatenation, thinking handling, tool_result placement, default image media
// type, etc.). To stay byte-faithful to whatever the target engine actually
// caches, callers select an Adapter by the engine's framework:
//
//		adapter := normalize.AdapterFor(profile.Framework)
//		rr, err := adapter.FromAnthropic(raw)
//
//	  - SGLang Anthropic  -> sglang/srt/entrypoints/anthropic/serving.py
//	  - vLLM   Anthropic  -> vllm/entrypoints/anthropic/serving.py
//	  - Responses (both)  -> vllm/entrypoints/openai/responses/utils.py
//
// tool_choice is deliberately NOT forwarded: it is a grammar/sampling
// constraint and does not change the rendered prompt — EXCEPT "none", which
// makes the engine skip rendering tools (serving_chat.py: `request.tools and
// request.tool_choice != "none"`). We honor that by dropping tools when
// tool_choice == "none". This keeps the tokenized prefix faithful while
// avoiding any risk of a malformed tool_choice failing engine validation.
package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ucloud/kv-indexer/internal/types"
)

// Adapter converts inbound protocol bodies into a RouteRequest for a specific
// serving framework. Chat and Responses are shared via baseAdapter; only
// FromAnthropic varies between frameworks.
type Adapter interface {
	FromOpenAIChat(raw []byte) (*types.RouteRequest, error)
	FromOpenAIResponses(raw []byte) (*types.RouteRequest, error)
	FromAnthropic(raw []byte) (*types.RouteRequest, error)
}

// AdapterFor returns the Adapter matching a framework string ("vllm" /
// "sglang"). Unknown or empty frameworks default to SGLang, matching the
// historical behavior of the package-level From* functions.
func AdapterFor(framework string) Adapter {
	switch strings.ToLower(strings.TrimSpace(framework)) {
	case "vllm":
		return vllmAdapter{}
	default:
		return sglangAdapter{}
	}
}

// baseAdapter implements the framework-independent conversions (Chat, Responses)
// shared by every concrete adapter.
type baseAdapter struct{}

// ---- backward-compatible package-level funcs (default to SGLang) ----
//
// These preserve the original API used by tests and any caller that does not
// (yet) thread a framework through. New code should prefer AdapterFor().

// FromOpenAIChat parses an OpenAI /v1/chat/completions body (framework-agnostic).
func FromOpenAIChat(raw []byte) (*types.RouteRequest, error) {
	return baseAdapter{}.FromOpenAIChat(raw)
}

// FromOpenAIResponses parses an OpenAI /v1/responses body (framework-agnostic).
func FromOpenAIResponses(raw []byte) (*types.RouteRequest, error) {
	return baseAdapter{}.FromOpenAIResponses(raw)
}

// FromAnthropic parses an Anthropic /v1/messages body using the SGLang converter
// (the historical default). Use AdapterFor(framework).FromAnthropic for vLLM.
func FromAnthropic(raw []byte) (*types.RouteRequest, error) {
	return sglangAdapter{}.FromAnthropic(raw)
}

// ---- OpenAI Chat (framework-independent passthrough) ----

func (baseAdapter) FromOpenAIChat(raw []byte) (*types.RouteRequest, error) {
	var body struct {
		Model               string              `json:"model"`
		Messages            []types.ChatMessage `json:"messages"`
		Tools               []any               `json:"tools"`
		ToolChoice          json.RawMessage     `json:"tool_choice"`
		MaxTokens           *int                `json:"max_tokens"`
		MaxCompletionTokens *int                `json:"max_completion_tokens"`
		Stream              bool                `json:"stream"`
		User                string              `json:"user"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse chat request: %w", err)
	}
	if body.Model == "" {
		return nil, fmt.Errorf("missing model")
	}
	tools := body.Tools
	if isToolChoiceNone(body.ToolChoice) {
		tools = nil // engine would not render tools under tool_choice="none"
	}
	rr := &types.RouteRequest{
		Protocol: types.ProtocolOpenAIChat,
		Model:    body.Model,
		Messages: body.Messages,
		Tools:    tools,
		Stream:   body.Stream,
		TenantID: body.User,
	}
	rr.MaxTokens = firstNonNil(body.MaxCompletionTokens, body.MaxTokens)
	rr.Turns = turnsFromMessages(body.Messages)
	return rr, nil
}

// ---- OpenAI Responses (framework-independent; mirrors vLLM utils.py) ----

func (baseAdapter) FromOpenAIResponses(raw []byte) (*types.RouteRequest, error) {
	var body struct {
		Model           string          `json:"model"`
		Input           json.RawMessage `json:"input"`
		Instructions    string          `json:"instructions"`
		Tools           []responsesTool `json:"tools"`
		ToolChoice      json.RawMessage `json:"tool_choice"`
		MaxOutputTokens *int            `json:"max_output_tokens"`
		Stream          bool            `json:"stream"`
		User            string          `json:"user"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	if body.Model == "" {
		return nil, fmt.Errorf("missing model")
	}

	var msgs []types.ChatMessage
	if body.Instructions != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: body.Instructions})
	}

	if len(body.Input) > 0 {
		var s string
		if json.Unmarshal(body.Input, &s) == nil {
			msgs = append(msgs, types.ChatMessage{Role: "user", Content: s})
		} else {
			var items []json.RawMessage
			if err := json.Unmarshal(body.Input, &items); err != nil {
				return nil, fmt.Errorf("parse responses input: %w", err)
			}
			counter := 0
			nextID := func() string { id := fmt.Sprintf("function_call_%d", counter); counter++; return id }
			for _, it := range items {
				msgs = appendResponsesItem(msgs, it, nextID)
			}
		}
	}

	var tools []any
	if !isToolChoiceNone(body.ToolChoice) {
		tools = convertResponsesTools(body.Tools)
	}

	rr := &types.RouteRequest{
		Protocol:  types.ProtocolOpenAIResponses,
		Model:     body.Model,
		Messages:  msgs,
		Tools:     tools,
		Stream:    body.Stream,
		TenantID:  body.User,
		MaxTokens: deref(body.MaxOutputTokens),
	}
	rr.Turns = turnsFromMessages(msgs)
	return rr, nil
}

// responsesTool is a flat OpenAI Responses tool definition.
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// appendResponsesItem dispatches one Responses input item to one or more
// chat messages, merging consecutive function_call items into a single
// assistant message's tool_calls (matching vLLM construct_chat_messages).
func appendResponsesItem(msgs []types.ChatMessage, raw json.RawMessage, nextID func() string) []types.ChatMessage {
	var peek struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		Output    json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return msgs
	}

	switch peek.Type {
	case "function_call":
		// Responses arguments arrive as a JSON STRING already; pass through.
		tc := types.ToolCall{
			ID:       firstNonEmpty(peek.CallID, nextID()),
			Type:     "function",
			Function: types.ToolCallFunction{Name: peek.Name, Arguments: peek.Arguments},
		}
		if n := len(msgs); n > 0 && msgs[n-1].Role == "assistant" {
			msgs[n-1].ToolCalls = append(msgs[n-1].ToolCalls, tc)
			return msgs
		}
		return append(msgs, types.ChatMessage{Role: "assistant", ToolCalls: []types.ToolCall{tc}})

	case "function_call_output":
		var out any
		if len(peek.Output) > 0 {
			_ = json.Unmarshal(peek.Output, &out)
		}
		return append(msgs, types.ChatMessage{Role: "tool", ToolCallID: peek.CallID, Content: out})

	case "reasoning":
		// Model-internal chain-of-thought; engine chat templates exclude prior
		// reasoning, so dropping it keeps prefix tokens faithful.
		return msgs

	default:
		// "message" / "output_message" / a bare role+content item.
		role := peek.Role
		if role == "" {
			if peek.Type == "output_message" {
				role = "assistant"
			} else {
				role = "user"
			}
		}
		return append(msgs, types.ChatMessage{Role: role, Content: convertResponsesContent(peek.Content)})
	}
}

// convertResponsesContent maps a Responses content value (string or array of
// typed parts) into OpenAI-chat content (string or []any of part maps).
func convertResponsesContent(raw json.RawMessage) any {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []map[string]any
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []any
	for _, p := range parts {
		t, _ := p["type"].(string)
		switch t {
		case "input_text", "output_text", "text":
			out = append(out, map[string]any{"type": "text", "text": p["text"]})
		case "input_image":
			url := ""
			if u, ok := p["image_url"].(string); ok {
				url = u
			} else if u, ok := p["url"].(string); ok {
				url = u
			}
			out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		default:
			// Preserve unknown parts (e.g. input_file) verbatim so nothing is
			// silently dropped and HasMultimodalContent still flags them.
			out = append(out, p)
		}
	}
	if len(out) == 1 {
		if m, ok := out[0].(map[string]any); ok && m["type"] == "text" {
			return m["text"]
		}
	}
	if len(out) > 0 {
		return out
	}
	return ""
}

// convertResponsesTools nests each flat Responses tool under {type:function,
// function:{...}}, matching vLLM convert_tool_responses_to_completions_format.
func convertResponsesTools(tools []responsesTool) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		fn := map[string]any{"name": t.Name, "description": t.Description}
		if len(t.Parameters) > 0 {
			fn["parameters"] = t.Parameters
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

// ---- shared helpers ----

// jsonString renders a JSON value (e.g. an Anthropic tool_use.input object) as a
// compact JSON string for OpenAI tool_call arguments. Empty input becomes "{}".
func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return "{}"
	}
	return buf.String()
}

// isToolChoiceNone reports whether an OpenAI-style tool_choice is the string
// "none" (the only value that changes the rendered prompt — tools are dropped).
func isToolChoiceNone(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s == "none"
}

// turnsFromMessages flattens chat messages to (role, text) for diagnostics.
func turnsFromMessages(msgs []types.ChatMessage) []types.Turn {
	out := make([]types.Turn, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, types.Turn{Role: m.Role, Text: contentToText(m.Content)})
	}
	return out
}

func contentToText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if mp, ok := part.(map[string]any); ok {
				if t, ok := mp["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	default:
		return ""
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func firstNonNil(a, b *int) int {
	if a != nil {
		return *a
	}
	if b != nil {
		return *b
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
