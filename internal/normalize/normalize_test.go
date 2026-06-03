package normalize

import (
	"encoding/json"
	"testing"
)

// ---- helpers for assertions on structured content ----

func parts(t *testing.T, c any) []any {
	t.Helper()
	p, ok := c.([]any)
	if !ok {
		t.Fatalf("content is not []any: %T (%v)", c, c)
	}
	return p
}

func partMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("part is not map: %T (%v)", v, v)
	}
	return m
}

// ---- OpenAI Chat ----

func TestFromOpenAIChat(t *testing.T) {
	rr, err := FromOpenAIChat([]byte(`{
		"model":"qwen3.5-4b",
		"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hi"}],
		"max_tokens":50,"stream":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Model != "qwen3.5-4b" || len(rr.Messages) != 2 || rr.MaxTokens != 50 {
		t.Fatalf("bad parse: %+v", rr)
	}
	if rr.Turns[0].Role != "system" || rr.Turns[1].Text != "hi" {
		t.Fatalf("turns wrong: %+v", rr.Turns)
	}
	if s, ok := rr.Messages[1].Content.(string); !ok || s != "hi" {
		t.Fatalf("string content not preserved: %#v", rr.Messages[1].Content)
	}
}

func TestFromOpenAIChatMaxCompletionTokens(t *testing.T) {
	rr, err := FromOpenAIChat([]byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"max_completion_tokens":7,"max_tokens":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.MaxTokens != 7 {
		t.Fatalf("max_completion_tokens should win: %d", rr.MaxTokens)
	}
}

func TestFromOpenAIChatContentPartsPreserved(t *testing.T) {
	rr, err := FromOpenAIChat([]byte(`{
		"model":"m",
		"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"x"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// Content must stay structured (NOT flattened) so HasMultimodalContent works.
	p := parts(t, rr.Messages[0].Content)
	if len(p) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(p))
	}
	if !rr.HasMultimodalContent() {
		t.Fatalf("should detect multimodal content")
	}
	// Turns still flatten text for diagnostics.
	if rr.Turns[0].Text != "a" {
		t.Fatalf("turn text wrong: %q", rr.Turns[0].Text)
	}
}

func TestFromOpenAIChatToolCallsPassThrough(t *testing.T) {
	rr, err := FromOpenAIChat([]byte(`{
		"model":"m",
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{\"a\":1}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"42"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	asst := rr.Messages[0]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool_calls not preserved: %+v", asst)
	}
	if asst.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("arguments must stay JSON string: %q", asst.ToolCalls[0].Function.Arguments)
	}
	if rr.Messages[1].ToolCallID != "call_1" {
		t.Fatalf("tool_call_id not preserved: %+v", rr.Messages[1])
	}
	// Assistant tool-call-only message must serialize WITHOUT a content key.
	b, _ := json.Marshal(asst)
	if got := string(b); contains(got, `"content"`) {
		t.Fatalf("tool-call-only assistant should omit content: %s", got)
	}
}

func TestFromOpenAIChatToolChoiceNoneDropsTools(t *testing.T) {
	rr, err := FromOpenAIChat([]byte(`{
		"model":"m","tool_choice":"none",
		"tools":[{"type":"function","function":{"name":"f"}}],
		"messages":[{"role":"user","content":"x"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Tools != nil {
		t.Fatalf("tool_choice=none must drop tools, got %+v", rr.Tools)
	}
}

// ---- OpenAI Responses ----

func TestFromOpenAIResponsesString(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m","instructions":"sys","input":"hello","max_output_tokens":20
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 2 {
		t.Fatalf("expected system+user, got %d", len(rr.Messages))
	}
	if rr.Messages[0].Role != "system" || rr.Messages[1].Content != "hello" {
		t.Fatalf("bad responses parse: %+v", rr.Messages)
	}
	if rr.MaxTokens != 20 {
		t.Fatalf("max_output_tokens not mapped: %d", rr.MaxTokens)
	}
}

func TestFromOpenAIResponsesArrayText(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[{"role":"user","content":[{"type":"input_text","text":"analyze this"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 1 || rr.Messages[0].Content != "analyze this" {
		t.Fatalf("single text part should collapse to string: %+v", rr.Messages)
	}
}

func TestFromOpenAIResponsesInputImage(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[{"role":"user","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"http://img"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	if len(p) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(p))
	}
	img := partMap(t, p[1])
	if img["type"] != "image_url" {
		t.Fatalf("expected image_url part: %+v", img)
	}
	if !rr.HasMultimodalContent() {
		t.Fatalf("should detect multimodal")
	}
}

func TestFromOpenAIResponsesInputFilePreserved(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[{"role":"user","content":[{"type":"input_file","file_id":"f1"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !rr.HasMultimodalContent() {
		t.Fatalf("input_file should flag multimodal")
	}
}

func TestFromOpenAIResponsesFunctionCall(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[{"type":"function_call","call_id":"c1","name":"get_weather","arguments":"{\"city\":\"SF\"}"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 1 || rr.Messages[0].Role != "assistant" {
		t.Fatalf("function_call should become assistant msg: %+v", rr.Messages)
	}
	tc := rr.Messages[0].ToolCalls
	if len(tc) != 1 || tc[0].ID != "c1" || tc[0].Function.Name != "get_weather" {
		t.Fatalf("tool call wrong: %+v", tc)
	}
	// Arguments arrive as JSON string already; must pass through verbatim.
	if tc[0].Function.Arguments != `{"city":"SF"}` {
		t.Fatalf("arguments double-encoded or altered: %q", tc[0].Function.Arguments)
	}
}

func TestFromOpenAIResponsesFunctionCallMissingID(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[{"type":"function_call","name":"f","arguments":"{}"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	id := rr.Messages[0].ToolCalls[0].ID
	if id != "function_call_0" {
		t.Fatalf("expected generated function_call_0, got %q", id)
	}
}

func TestFromOpenAIResponsesTwoFunctionCallsMerge(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[
			{"type":"function_call","call_id":"c1","name":"a","arguments":"{}"},
			{"type":"function_call","call_id":"c2","name":"b","arguments":"{}"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 1 {
		t.Fatalf("consecutive function_calls should merge into one assistant msg, got %d", len(rr.Messages))
	}
	if len(rr.Messages[0].ToolCalls) != 2 {
		t.Fatalf("expected 2 merged tool_calls, got %d", len(rr.Messages[0].ToolCalls))
	}
}

func TestFromOpenAIResponsesFunctionCallOutput(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[
			{"type":"function_call","call_id":"c1","name":"a","arguments":"{}"},
			{"type":"function_call_output","call_id":"c1","output":"result text"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// [assistant(tool_calls), tool]
	if len(rr.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(rr.Messages), rr.Messages)
	}
	out := rr.Messages[1]
	if out.Role != "tool" || out.ToolCallID != "c1" || out.Content != "result text" {
		t.Fatalf("function_call_output wrong: %+v", out)
	}
}

func TestFromOpenAIResponsesReasoningDropped(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m",
		"input":[
			{"type":"reasoning","content":[{"type":"text","text":"thinking..."}]},
			{"role":"user","content":"q"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 1 || rr.Messages[0].Content != "q" {
		t.Fatalf("reasoning should be dropped, leaving only user msg: %+v", rr.Messages)
	}
}

func TestFromOpenAIResponsesTools(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m","input":"x",
		"tools":[{"type":"function","name":"f","description":"d","parameters":{"type":"object"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(rr.Tools))
	}
	tm := rr.Tools[0].(map[string]any)
	if tm["type"] != "function" {
		t.Fatalf("tool not nested under function: %+v", tm)
	}
	fn := tm["function"].(map[string]any)
	if fn["name"] != "f" || fn["description"] != "d" {
		t.Fatalf("function fields wrong: %+v", fn)
	}
}

func TestFromOpenAIResponsesToolChoiceNone(t *testing.T) {
	rr, err := FromOpenAIResponses([]byte(`{
		"model":"m","input":"x","tool_choice":"none",
		"tools":[{"type":"function","name":"f"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Tools != nil {
		t.Fatalf("tool_choice=none must drop tools: %+v", rr.Tools)
	}
}

// ---- Anthropic ----

func TestFromAnthropic(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"claude","system":"be brief",
		"messages":[{"role":"user","content":"q1"},{"role":"assistant","content":[{"type":"text","text":"a1"}]}],
		"max_tokens":100
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 3 {
		t.Fatalf("expected system+2 turns, got %d", len(rr.Messages))
	}
	if rr.Messages[0].Role != "system" || rr.Messages[0].Content != "be brief" {
		t.Fatalf("anthropic system not folded: %+v", rr.Messages[0])
	}
	// Single text block collapses to string.
	if rr.Messages[2].Content != "a1" {
		t.Fatalf("single text block should collapse to string: %+v", rr.Messages[2])
	}
	if rr.MaxTokens != 100 {
		t.Fatalf("max_tokens not mapped: %d", rr.MaxTokens)
	}
}

func TestAnthropicSystemArray(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"claude",
		"system":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}],
		"messages":[{"role":"user","content":"hi"}],
		"max_tokens":10
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// SGLang joins system text blocks with newlines.
	if rr.Messages[0].Content != "line1\nline2" {
		t.Fatalf("system array should newline-join: %q", rr.Messages[0].Content)
	}
}

func TestAnthropicMultiTextBlocks(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	if len(p) != 2 {
		t.Fatalf("expected 2 text parts, got %d", len(p))
	}
}

func TestAnthropicImageBase64(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"AAAA"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	img := partMap(t, p[0])
	iu := img["image_url"].(map[string]any)
	if iu["url"] != "data:image/jpeg;base64,AAAA" {
		t.Fatalf("base64 data URI wrong: %v", iu["url"])
	}
	if !rr.HasMultimodalContent() {
		t.Fatalf("should detect multimodal")
	}
}

func TestAnthropicImageBase64DefaultMediaType(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"X"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	iu := partMap(t, p[0])["image_url"].(map[string]any)
	if iu["url"] != "data:image/png;base64,X" {
		t.Fatalf("default media_type should be image/png: %v", iu["url"])
	}
}

func TestAnthropicImageURL(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://x/y.png"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	iu := partMap(t, p[0])["image_url"].(map[string]any)
	if iu["url"] != "https://x/y.png" {
		t.Fatalf("url source wrong: %v", iu["url"])
	}
}

func TestAnthropicImageInvalidSourceSkipped(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// Invalid image dropped; only the single text remains -> collapses to string.
	if rr.Messages[0].Content != "hi" {
		t.Fatalf("invalid image should be skipped: %+v", rr.Messages[0].Content)
	}
}

func TestAnthropicToolUse(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"search","input":{"q":"go"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	tc := rr.Messages[0].ToolCalls
	if len(tc) != 1 || tc[0].ID != "tu1" || tc[0].Function.Name != "search" {
		t.Fatalf("tool_use wrong: %+v", tc)
	}
	if tc[0].Function.Arguments != `{"q":"go"}` {
		t.Fatalf("arguments must be JSON string of input: %q", tc[0].Function.Arguments)
	}
	if rr.Messages[0].Content != nil {
		t.Fatalf("tool-call-only assistant should have nil content: %#v", rr.Messages[0].Content)
	}
}

func TestAnthropicToolUseMissingIDDeterministic(t *testing.T) {
	body := `{
		"model":"c","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"f","input":{}}]}]
	}`
	rr1, _ := FromAnthropic([]byte(body))
	rr2, _ := FromAnthropic([]byte(body))
	id1 := rr1.Messages[0].ToolCalls[0].ID
	id2 := rr2.Messages[0].ToolCalls[0].ID
	if id1 != "call_0" {
		t.Fatalf("expected deterministic call_0, got %q", id1)
	}
	// Determinism is critical for a KV-cache indexer: identical requests must
	// tokenize identically.
	if id1 != id2 {
		t.Fatalf("generated ids must be deterministic across identical requests: %q vs %q", id1, id2)
	}
}

func TestAnthropicToolUseEmptyInput(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"x","name":"f"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := rr.Messages[0].ToolCalls[0].Function.Arguments; got != "{}" {
		t.Fatalf("empty input should be {}, got %q", got)
	}
}

func TestAnthropicUserToolResult(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"the answer"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 1 {
		t.Fatalf("expected 1 tool message, got %d", len(rr.Messages))
	}
	m := rr.Messages[0]
	if m.Role != "tool" || m.ToolCallID != "tu1" || m.Content != "the answer" {
		t.Fatalf("user tool_result -> tool message wrong: %+v", m)
	}
}

func TestAnthropicToolResultIDPrecedence(t *testing.T) {
	rr, _ := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu","id":"id","content":"x"}]}]
	}`))
	if rr.Messages[0].ToolCallID != "tu" {
		t.Fatalf("tool_use_id should win over id: %q", rr.Messages[0].ToolCallID)
	}
}

func TestAnthropicToolResultContentList(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"text","text":"r1"},{"type":"image","source":{"type":"url","url":"u"}}]}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	if len(p) != 2 {
		t.Fatalf("expected text+image parts, got %d", len(p))
	}
}

func TestAnthropicAssistantToolResultInline(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"t","content":"done"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// Assistant-role tool_result becomes inline "Tool result: ..." text, not a tool message.
	if len(rr.Messages) != 1 || rr.Messages[0].Role != "assistant" {
		t.Fatalf("expected single assistant message, got %+v", rr.Messages)
	}
	if rr.Messages[0].Content != "Tool result: done" {
		t.Fatalf("inline tool result wrong: %#v", rr.Messages[0].Content)
	}
}

func TestAnthropicThinkingDropped(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"answer"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Messages[0].Content != "answer" {
		t.Fatalf("thinking should be dropped, leaving text: %#v", rr.Messages[0].Content)
	}
}

func TestAnthropicTools(t *testing.T) {
	deferTrue := `{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":"x"}],
		"tools":[{"name":"f","input_schema":{"type":"object"},"defer_loading":true}]
	}`
	rr, err := FromAnthropic([]byte(deferTrue))
	if err != nil {
		t.Fatal(err)
	}
	tm := rr.Tools[0].(map[string]any)
	if tm["type"] != "function" {
		t.Fatalf("tool not function-typed: %+v", tm)
	}
	if tm["defer_loading"] != true {
		t.Fatalf("defer_loading not propagated: %+v", tm)
	}
	fn := tm["function"].(map[string]any)
	if fn["name"] != "f" {
		t.Fatalf("function name wrong: %+v", fn)
	}
}

func TestAnthropicToolChoiceNone(t *testing.T) {
	rr, _ := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,"tool_choice":{"type":"none"},
		"tools":[{"name":"f","input_schema":{"type":"object"}}],
		"messages":[{"role":"user","content":"x"}]
	}`))
	if rr.Tools != nil {
		t.Fatalf("tool_choice none must drop tools: %+v", rr.Tools)
	}
}

func TestAnthropicMultiTurnToolFlow(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[
			{"role":"user","content":"weather?"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"w","input":{"c":"SF"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"sunny"}]},
			{"role":"assistant","content":[{"type":"text","text":"It is sunny."}]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(rr.Messages), rr.Messages)
	}
	roles := []string{rr.Messages[0].Role, rr.Messages[1].Role, rr.Messages[2].Role, rr.Messages[3].Role}
	want := []string{"user", "assistant", "tool", "assistant"}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("role[%d]=%q want %q (%+v)", i, roles[i], want[i], roles)
		}
	}
	// tool_use id and tool_result id must match so the template pairs them.
	if rr.Messages[1].ToolCalls[0].ID != "tu1" || rr.Messages[2].ToolCallID != "tu1" {
		t.Fatalf("tool ids should match: %q vs %q", rr.Messages[1].ToolCalls[0].ID, rr.Messages[2].ToolCallID)
	}
}

func TestAnthropicEmptyMessageSkipped(t *testing.T) {
	rr, err := FromAnthropic([]byte(`{
		"model":"c","max_tokens":1,
		"messages":[{"role":"user","content":[]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.Messages) != 0 {
		t.Fatalf("empty content message should be skipped, got %+v", rr.Messages)
	}
}

// ---- multimodal detection (kept from original) ----

func TestMultimodalDetection(t *testing.T) {
	rr, _ := FromOpenAIChat([]byte(`{
		"model":"m",
		"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]
	}`))
	if !rr.HasMultimodalContent() {
		t.Fatalf("should detect multimodal content")
	}
	rr2, _ := FromOpenAIChat([]byte(`{"model":"m","messages":[{"role":"user","content":"just text"}]}`))
	if rr2.HasMultimodalContent() {
		t.Fatalf("plain text should not be multimodal")
	}
}

// contains is a tiny substring helper (avoids importing strings just for tests).
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
