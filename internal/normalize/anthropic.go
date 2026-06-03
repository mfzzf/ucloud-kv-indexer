package normalize

import (
	"encoding/json"
	"fmt"

	"github.com/ucloud/kv-indexer/internal/types"
)

// This file holds the Anthropic Messages → OpenAI-chat pieces SHARED by both
// the SGLang and vLLM adapters: request envelope parsing, tool / tool_choice
// conversion (identical in both engines), and the image-source converter
// (parameterized by the default media type, which differs: SGLang png, vLLM
// jpeg). The per-framework block conversion lives in anthropic_sglang.go and
// anthropic_vllm.go.

// anthropicTool is an Anthropic tool definition.
type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	DeferLoading *bool           `json:"defer_loading"`
}

// anthropicBlock is one content block of an Anthropic message.
type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Source    json.RawMessage `json:"source"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

// anthropicEnvelope is the parsed top-level Anthropic Messages request, common
// to both adapters.
type anthropicEnvelope struct {
	Model    string          `json:"model"`
	System   json.RawMessage `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools      []anthropicTool `json:"tools"`
	ToolChoice json.RawMessage `json:"tool_choice"`
	MaxTokens  int             `json:"max_tokens"`
	Stream     bool            `json:"stream"`
}

func parseAnthropicEnvelope(raw []byte) (*anthropicEnvelope, error) {
	var env anthropicEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	if env.Model == "" {
		return nil, fmt.Errorf("missing model")
	}
	return &env, nil
}

// buildAnthropicRouteRequest assembles the RouteRequest from a parsed envelope
// and the already-converted messages, applying the shared tools/tool_choice
// rules (drop tools on tool_choice:none).
func buildAnthropicRouteRequest(env *anthropicEnvelope, msgs []types.ChatMessage) *types.RouteRequest {
	var tools []any
	if !anthropicToolChoiceNone(env.ToolChoice) {
		tools = convertAnthropicTools(env.Tools)
	}
	rr := &types.RouteRequest{
		Protocol:  types.ProtocolAnthropic,
		Model:     env.Model,
		Messages:  msgs,
		Tools:     tools,
		Stream:    env.Stream,
		MaxTokens: env.MaxTokens,
	}
	rr.Turns = turnsFromMessages(msgs)
	return rr
}

// convertAnthropicTools maps Anthropic tools to OpenAI function tools. Identical
// in SGLang and vLLM (both nest under {type:function, function:{...}} and
// propagate defer_loading).
func convertAnthropicTools(tools []anthropicTool) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		fn := map[string]any{"name": t.Name, "description": t.Description}
		if len(t.InputSchema) > 0 {
			fn["parameters"] = t.InputSchema
		}
		toolMap := map[string]any{"type": "function", "function": fn}
		if t.DeferLoading != nil {
			toolMap["defer_loading"] = *t.DeferLoading
		}
		out = append(out, toolMap)
	}
	return out
}

// anthropicToolChoiceNone reports whether an Anthropic tool_choice object has
// type "none".
func anthropicToolChoiceNone(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var tc struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &tc) == nil && tc.Type == "none"
}

// anthropicImageToPart converts an Anthropic image source to an OpenAI
// image_url content part. base64 sources become data URIs (using defaultMT when
// media_type is absent); url sources pass through. Returns false for an
// invalid/empty source so the caller skips it.
//
// defaultMT differs by engine: SGLang uses "image/png", vLLM "image/jpeg".
func anthropicImageToPart(raw json.RawMessage, defaultMT string) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var src struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	}
	if json.Unmarshal(raw, &src) != nil {
		return nil, false
	}
	var url string
	switch {
	case src.Type == "base64" && src.Data != "":
		mt := src.MediaType
		if mt == "" {
			mt = defaultMT
		}
		url = "data:" + mt + ";base64," + src.Data
	case src.URL != "":
		url = src.URL
	default:
		return nil, false
	}
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}, true
}

// collapseContentParts applies the single-text-part optimization shared by both
// adapters: one {type:text} part collapses to a plain string; multiple/mixed
// parts stay as a []any; empty stays nil.
func collapseContentParts(parts []any) any {
	switch len(parts) {
	case 0:
		return nil
	case 1:
		if m, ok := parts[0].(map[string]any); ok && m["type"] == "text" {
			return m["text"]
		}
		return parts
	default:
		return parts
	}
}
