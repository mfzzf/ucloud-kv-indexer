package normalize

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ucloud/kv-indexer/internal/types"
)

// sglangAdapter converts inbound protocols for an SGLang engine. Chat and
// Responses come from baseAdapter; only the Anthropic converter is SGLang-
// specific, mirroring sglang/srt/entrypoints/anthropic/serving.py.
type sglangAdapter struct{ baseAdapter }

// sglangImageDefaultMediaType is SGLang's fallback for base64 images without a
// media_type (serving.py: media_type default "image/png").
const sglangImageDefaultMediaType = "image/png"

// FromAnthropic converts an Anthropic Messages request the SGLang way:
//   - system string/array joined with "\n"
//   - thinking / redacted_thinking blocks DROPPED
//   - user tool_result -> a single {role:tool} message whose content may be a
//     parts array (text+image+tool_reference together)
//   - tool_call_id = tool_use_id || id
//   - assistant-role tool_result -> inline "Tool result: <joined text>"
//   - base64 image default media type image/png
func (sglangAdapter) FromAnthropic(raw []byte) (*types.RouteRequest, error) {
	env, err := parseAnthropicEnvelope(raw)
	if err != nil {
		return nil, err
	}

	var msgs []types.ChatMessage
	if sys := sglangAnthropicSystem(env.System); sys != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: sys})
	}

	counter := 0
	genID := func() string { id := fmt.Sprintf("call_%d", counter); counter++; return id }
	for _, m := range env.Messages {
		msgs = append(msgs, sglangAnthropicMessage(m.Role, m.Content, genID)...)
	}

	return buildAnthropicRouteRequest(env, msgs), nil
}

// sglangAnthropicSystem flattens the system field, joining text blocks with "\n".
func sglangAnthropicSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// sglangAnthropicMessage converts one Anthropic message into a content message
// plus any trailing user tool-role messages.
func sglangAnthropicMessage(role string, contentRaw json.RawMessage, genID func() string) []types.ChatMessage {
	var s string
	if json.Unmarshal(contentRaw, &s) == nil {
		return []types.ChatMessage{{Role: role, Content: s}}
	}
	var blocks []anthropicBlock
	if json.Unmarshal(contentRaw, &blocks) != nil {
		return nil
	}

	var out []types.ChatMessage
	var contentParts []any
	var toolCalls []types.ToolCall

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				contentParts = append(contentParts, map[string]any{"type": "text", "text": b.Text})
			}
		case "image":
			if part, ok := anthropicImageToPart(b.Source, sglangImageDefaultMediaType); ok {
				contentParts = append(contentParts, part)
			}
		case "tool_use":
			toolCalls = append(toolCalls, types.ToolCall{
				ID:       firstNonEmpty(b.ID, genID()),
				Type:     "function",
				Function: types.ToolCallFunction{Name: b.Name, Arguments: jsonString(b.Input)},
			})
		case "tool_result":
			content, toolText := sglangToolResultContent(b.Content)
			if role == "user" {
				out = append(out, types.ChatMessage{
					Role:       "tool",
					ToolCallID: firstNonEmpty(b.ToolUseID, b.ID),
					Content:    content,
				})
			} else {
				contentParts = append(contentParts, map[string]any{
					"type": "text", "text": "Tool result: " + toolText,
				})
			}
			// "thinking" / "redacted_thinking": dropped (SGLang does not render them).
		}
	}

	msg := types.ChatMessage{Role: role}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	msg.Content = collapseContentParts(contentParts)
	if msg.Content == nil && len(toolCalls) == 0 {
		// Empty message with no tool calls: skip entirely.
		return out
	}
	return append(out, msg)
}

// sglangToolResultContent converts an Anthropic tool_result content value into
// OpenAI tool-message content (a string or a parts array). Returns
// (content, toolText) where toolText is used for the assistant-role inline case.
func sglangToolResultContent(raw json.RawMessage) (any, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, s
	}
	var items []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Source   json.RawMessage `json:"source"`
		ToolName string          `json:"tool_name"`
		Name     string          `json:"name"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return "", ""
	}
	var parts []any
	var texts []string
	for _, it := range items {
		switch it.Type {
		case "text":
			if it.Text != "" {
				texts = append(texts, it.Text)
				parts = append(parts, map[string]any{"type": "text", "text": it.Text})
			}
		case "image":
			if part, ok := anthropicImageToPart(it.Source, sglangImageDefaultMediaType); ok {
				parts = append(parts, part)
			}
		case "tool_reference":
			if ref := firstNonEmpty(it.ToolName, it.Name); ref != "" {
				parts = append(parts, map[string]any{"type": "tool_reference", "name": ref})
			}
		}
	}
	toolText := strings.Join(texts, "\n")
	if len(parts) == 1 {
		if m, ok := parts[0].(map[string]any); ok && m["type"] == "text" {
			return m["text"], toolText
		}
	}
	if len(parts) > 0 {
		return parts, toolText
	}
	return "", toolText
}
