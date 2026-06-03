package normalize

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ucloud/kv-indexer/internal/types"
)

// vllmAdapter converts inbound protocols for a vLLM engine. Chat and Responses
// come from baseAdapter; the Anthropic converter mirrors
// vllm/entrypoints/anthropic/serving.py::_convert_anthropic_to_openai_request,
// which differs from SGLang in several ways (see per-step comments below).
type vllmAdapter struct{ baseAdapter }

// vllmImageDefaultMediaType is vLLM's fallback for base64 images without a
// media_type (serving.py _convert_image_source_to_url default "image/jpeg").
const vllmImageDefaultMediaType = "image/jpeg"

// billingHeaderPrefix is Claude Code's per-request attribution header. vLLM
// strips system text blocks starting with it because the per-request hash
// defeats prefix caching (serving.py:159).
const billingHeaderPrefix = "x-anthropic-billing-header"

// FromAnthropic converts an Anthropic Messages request the vLLM way:
//   - system string passes through; system array text blocks are concatenated
//     with NO separator ("" join), skipping x-anthropic-billing-header blocks
//   - thinking blocks -> assistant `reasoning` field (NOT dropped)
//   - redacted_thinking -> skipped
//   - user tool_result -> {role:tool, tool_call_id: tool_use_id||"", content: text};
//     any images become a SEPARATE {role:user} image message, and tool_reference
//     parts become a SEPARATE {role:tool} message
//   - tool_call_id = tool_use_id || "" (no fallback to id)
//   - assistant-role tool_result -> inline "Tool result: <str(content)>"
//   - base64 image default media type image/jpeg
func (vllmAdapter) FromAnthropic(raw []byte) (*types.RouteRequest, error) {
	env, err := parseAnthropicEnvelope(raw)
	if err != nil {
		return nil, err
	}

	var msgs []types.ChatMessage
	if sys, ok := vllmAnthropicSystem(env.System); ok {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: sys})
	}

	counter := 0
	genID := func() string { id := fmt.Sprintf("call_%d", counter); counter++; return id }
	for _, m := range env.Messages {
		msgs = vllmAppendAnthropicMessage(msgs, m.Role, m.Content, genID)
	}

	return buildAnthropicRouteRequest(env, msgs), nil
}

// vllmAnthropicSystem flattens system. A string passes through. An array
// concatenates text blocks with NO separator, skipping billing-header blocks.
// ok=false means "no system message" (absent system).
func vllmAnthropicSystem(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if strings.HasPrefix(b.Text, billingHeaderPrefix) {
					continue
				}
				sb.WriteString(b.Text)
			}
		}
		return sb.String(), true
	}
	return "", false
}

// vllmAppendAnthropicMessage converts one Anthropic message and appends the
// resulting messages (a content message plus any split-out tool/image messages).
func vllmAppendAnthropicMessage(msgs []types.ChatMessage, role string, contentRaw json.RawMessage, genID func() string) []types.ChatMessage {
	var s string
	if json.Unmarshal(contentRaw, &s) == nil {
		return append(msgs, types.ChatMessage{Role: role, Content: s})
	}
	var blocks []anthropicBlock
	if json.Unmarshal(contentRaw, &blocks) != nil {
		return msgs
	}

	var contentParts []any
	var toolCalls []types.ToolCall
	var reasoning strings.Builder
	// Trailing messages split out of this turn (tool results / images), appended
	// AFTER the content message to match vLLM's append order.
	var trailing []types.ChatMessage

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				contentParts = append(contentParts, map[string]any{"type": "text", "text": b.Text})
			}
		case "image":
			if part, ok := anthropicImageToPart(b.Source, vllmImageDefaultMediaType); ok {
				contentParts = append(contentParts, part)
			}
		case "thinking":
			// vLLM keeps thinking as the message's reasoning field (serving.py:232).
			reasoning.WriteString(b.Thinking)
		case "redacted_thinking":
			// Opaque safety-filtered content; skipped.
		case "tool_use":
			toolCalls = append(toolCalls, types.ToolCall{
				ID:       firstNonEmpty(b.ID, genID()),
				Type:     "function",
				Function: types.ToolCallFunction{Name: b.Name, Arguments: jsonString(b.Input)},
			})
		case "tool_result":
			if role == "user" {
				trailing = vllmUserToolResult(trailing, b)
			} else {
				// assistant role: inline text using Python str(content) semantics.
				contentParts = append(contentParts, map[string]any{
					"type": "text", "text": "Tool result: " + anthropicContentToStr(b.Content),
				})
			}
		case "tool_reference":
			// Expanded during tool_result processing; standalone ones are ignored.
		}
	}

	msg := types.ChatMessage{Role: role}
	if reasoning.Len() > 0 {
		msg.Reasoning = reasoning.String()
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	msg.Content = collapseContentParts(contentParts)

	// vLLM appends the message unless it's a user message that ended up with no
	// content key (serving.py:177). reasoning/tool_calls still force an append.
	if msg.Content != nil || len(toolCalls) > 0 || reasoning.Len() > 0 {
		msgs = append(msgs, msg)
	} else if role != "user" {
		msgs = append(msgs, msg)
	}
	return append(msgs, trailing...)
}

// vllmUserToolResult splits a user-role tool_result into a {role:tool} text
// message plus optional separate image ({role:user}) and tool_reference
// ({role:tool}) messages (serving.py:_convert_user_tool_result). vLLM uses
// tool_use_id || "" as the tool_call_id (no fallback to id).
func vllmUserToolResult(trailing []types.ChatMessage, b anthropicBlock) []types.ChatMessage {
	toolText, imageURLs, toolRefs := vllmSplitToolResultContent(b.Content)
	toolCallID := b.ToolUseID

	trailing = append(trailing, types.ChatMessage{
		Role:       "tool",
		ToolCallID: toolCallID,
		Content:    toolText,
	})
	if len(imageURLs) > 0 {
		parts := make([]any, 0, len(imageURLs))
		for _, u := range imageURLs {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
		}
		trailing = append(trailing, types.ChatMessage{Role: "user", Content: parts})
	}
	if len(toolRefs) > 0 {
		refs := make([]any, 0, len(toolRefs))
		for _, r := range toolRefs {
			refs = append(refs, map[string]any{"type": "tool_reference", "name": r})
		}
		trailing = append(trailing, types.ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: refs})
	}
	return trailing
}

// vllmSplitToolResultContent mirrors _convert_user_tool_result: text parts are
// newline-joined into one string; images and tool_references are collected
// separately to become their own messages.
func vllmSplitToolResultContent(raw json.RawMessage) (text string, imageURLs []string, toolRefs []string) {
	if len(raw) == 0 {
		return "", nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil, nil
	}
	var items []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Source   json.RawMessage `json:"source"`
		ToolName string          `json:"tool_name"`
		Name     string          `json:"name"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return "", nil, nil
	}
	var texts []string
	for _, it := range items {
		switch it.Type {
		case "text":
			texts = append(texts, it.Text)
		case "image":
			if part, ok := anthropicImageToPart(it.Source, vllmImageDefaultMediaType); ok {
				iu := part["image_url"].(map[string]any)
				if u, _ := iu["url"].(string); u != "" {
					imageURLs = append(imageURLs, u)
				}
			}
		case "tool_reference":
			if ref := firstNonEmpty(it.ToolName, it.Name); ref != "" {
				toolRefs = append(toolRefs, ref)
			}
		}
	}
	return strings.Join(texts, "\n"), imageURLs, toolRefs
}

// anthropicContentToStr approximates Python str(block.content) for the
// assistant-role inline tool_result case: a JSON string yields its value; any
// other shape yields its compact JSON form (vLLM uses str() which for a list
// would be a Python repr — we use compact JSON as the closest stable rendering).
func anthropicContentToStr(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return jsonString(raw)
}
