// Package types holds shared domain types used across the kv-indexer.
package types

// Protocol identifies the inbound request wire protocol.
type Protocol string

const (
	ProtocolOpenAIChat      Protocol = "openai.chat"      // POST /v1/chat/completions
	ProtocolOpenAIResponses Protocol = "openai.responses" // POST /v1/responses
	ProtocolAnthropic       Protocol = "anthropic.messages"
)

// Turn is one role/text pair extracted from a request, used only for
// tokenization preview/diagnostics. The authoritative token IDs come from the
// target engine tokenizer endpoint, not from re-encoding these turns.
type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// RouteRequest is the protocol-agnostic normalized form of an inbound request.
// Only stable, framework-independent fields live here; the original messages
// are forwarded verbatim to the tokenizer endpoint.
type RouteRequest struct {
	Protocol  Protocol `json:"protocol"`
	RequestID string   `json:"request_id,omitempty"`
	TenantID  string   `json:"tenant_id,omitempty"`
	Model     string   `json:"model"`

	// Messages is the OpenAI-chat-shaped message list to hand to the tokenizer
	// (system prompt folded in as the first message for Responses/Anthropic).
	Messages []ChatMessage `json:"messages,omitempty"`
	// Tools forwarded verbatim to the tokenizer (affects prompt tokens).
	Tools []any `json:"tools,omitempty"`

	// Generation-only knobs. Do NOT affect prompt tokens; MaxTokens feeds
	// capacity estimation only.
	MaxTokens int  `json:"max_tokens,omitempty"`
	Stream    bool `json:"stream,omitempty"`

	// Turns is the flattened (role,text) view for previews/diagnostics.
	Turns []Turn `json:"turns,omitempty"`
}

// ChatMessage is the OpenAI chat message shape forwarded VERBATIM to /tokenize.
// We never apply a chat template locally; the engine does. To stay faithful to
// what the engine would tokenize, we preserve structure rather than flatten:
//
//   - Content is a string OR a []any of content-part maps:
//     {"type":"text","text":string}
//     {"type":"image_url","image_url":{"url":string}}
//     It is `any` (with omitempty) so an assistant message that carries only
//     tool_calls serializes WITHOUT a content key (Content left nil). A non-nil
//     interface — including an empty string — is always kept, matching input.
//   - Assistant messages may carry ToolCalls.
//   - Tool-role messages carry ToolCallID (and optionally Name).
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant only
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool role only
	Name       string     `json:"name,omitempty"`         // optional tool/function name
	// Reasoning carries prior chain-of-thought. vLLM's Anthropic converter maps
	// `thinking` blocks (and Responses `reasoning` items) into this assistant
	// field; the engine chat template decides whether to render it. SGLang's
	// converter drops thinking, so its adapter leaves this empty.
	Reasoning string `json:"reasoning,omitempty"`
}

// ToolCall is an OpenAI assistant tool call. Arguments is a JSON STRING per the
// OpenAI contract; SGLang/vLLM re-parse it to a dict before apply_chat_template,
// so the exact whitespace we send is normalized away on the engine side.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // always "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the {name, arguments} pair of an OpenAI tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string, e.g. `{"city":"SF"}`
}

// StorageTier enumerates KV residency tiers, lowercased.
const (
	TierGPU  = "gpu"
	TierCPU  = "cpu"
	TierDisk = "disk"
)

// HasMultimodalContent reports whether any message carries non-text content
// parts (image_url, input_image, input_file, etc.). For the text-only hash
// profile, such a request cannot be hashed reliably and must fall back rather
// than be judged on cache hit. LoRA/cache_salt are not parsed into
// RouteRequest in the MVP, so this currently checks only multimodal content.
func (rr *RouteRequest) HasMultimodalContent() bool {
	for _, m := range rr.Messages {
		if isMultimodalContent(m.Content) {
			return true
		}
	}
	return false
}

// isMultimodalContent reports whether a message content carries non-text parts
// (image_url, input_image, input_file, etc.).
func isMultimodalContent(c any) bool {
	parts, ok := c.([]any)
	if !ok {
		return false
	}
	for _, p := range parts {
		mp, ok := p.(map[string]any)
		if !ok {
			continue
		}
		t, _ := mp["type"].(string)
		switch t {
		case "", "text", "input_text", "output_text":
			// textual
		default:
			return true
		}
	}
	return false
}
