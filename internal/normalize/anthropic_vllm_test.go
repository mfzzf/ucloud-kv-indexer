package normalize

import "testing"

// These tests pin the vLLM-specific Anthropic→chat divergences from SGLang.
// Reference: vllm/entrypoints/anthropic/serving.py.

func TestVLLMAnthropicSystemArrayNoSeparator(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"system":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}],
		"messages":[{"role":"user","content":"hi"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// vLLM concatenates system text blocks with NO separator (SGLang uses "\n").
	if rr.Messages[0].Content != "line1line2" {
		t.Fatalf("vLLM system should concat without separator: %q", rr.Messages[0].Content)
	}
}

func TestVLLMAnthropicStripsBillingHeader(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"system":[{"type":"text","text":"x-anthropic-billing-header: abc123"},{"type":"text","text":"real system"}],
		"messages":[{"role":"user","content":"hi"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Messages[0].Content != "real system" {
		t.Fatalf("billing header block should be stripped: %q", rr.Messages[0].Content)
	}
}

func TestVLLMAnthropicThinkingBecomesReasoning(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"let me think"},{"type":"text","text":"answer"}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// vLLM keeps thinking as reasoning (SGLang drops it).
	if rr.Messages[0].Reasoning != "let me think" {
		t.Fatalf("thinking should map to reasoning: %q", rr.Messages[0].Reasoning)
	}
	if rr.Messages[0].Content != "answer" {
		t.Fatalf("text content wrong: %#v", rr.Messages[0].Content)
	}
}

func TestVLLMAnthropicImageDefaultMediaTypeJpeg(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"X"}}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, rr.Messages[0].Content)
	iu := partMap(t, p[0])["image_url"].(map[string]any)
	// vLLM defaults to image/jpeg (SGLang image/png).
	if iu["url"] != "data:image/jpeg;base64,X" {
		t.Fatalf("vLLM default media type should be jpeg: %v", iu["url"])
	}
}

func TestVLLMAnthropicUserToolResultSplitsImage(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"tu1","content":[
				{"type":"text","text":"see attached"},
				{"type":"image","source":{"type":"url","url":"https://img"}}
			]}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// vLLM splits: a tool message (text only) THEN a separate user image message.
	if len(rr.Messages) != 2 {
		t.Fatalf("expected tool + user(image) messages, got %d: %+v", len(rr.Messages), rr.Messages)
	}
	if rr.Messages[0].Role != "tool" || rr.Messages[0].ToolCallID != "tu1" || rr.Messages[0].Content != "see attached" {
		t.Fatalf("tool message wrong: %+v", rr.Messages[0])
	}
	if rr.Messages[1].Role != "user" {
		t.Fatalf("image should split into separate user message: %+v", rr.Messages[1])
	}
	imgParts := parts(t, rr.Messages[1].Content)
	iu := partMap(t, imgParts[0])["image_url"].(map[string]any)
	if iu["url"] != "https://img" {
		t.Fatalf("split image url wrong: %v", iu["url"])
	}
}

func TestVLLMAnthropicUserToolResultSplitsToolReference(t *testing.T) {
	rr, err := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"tu1","content":[
				{"type":"text","text":"ok"},
				{"type":"tool_reference","tool_name":"search"}
			]}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// tool message (text) THEN a separate tool message carrying the reference.
	if len(rr.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(rr.Messages), rr.Messages)
	}
	refParts := parts(t, rr.Messages[1].Content)
	rm := partMap(t, refParts[0])
	if rm["type"] != "tool_reference" || rm["name"] != "search" {
		t.Fatalf("tool_reference split wrong: %+v", rm)
	}
}

func TestVLLMAnthropicToolResultIDNoIDFallback(t *testing.T) {
	rr, _ := vllmAdapter{}.FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"messages":[{"role":"user","content":[{"type":"tool_result","id":"only-id","content":"x"}]}]
	}`))
	// vLLM uses tool_use_id || "" — it does NOT fall back to id (SGLang does).
	if rr.Messages[0].ToolCallID != "" {
		t.Fatalf("vLLM should not fall back to id, got %q", rr.Messages[0].ToolCallID)
	}
}

func TestVLLMAnthropicToolUseDeterministicID(t *testing.T) {
	body := `{
		"model":"m","max_tokens":1,
		"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"f","input":{}}]}]
	}`
	rr1, _ := vllmAdapter{}.FromAnthropic([]byte(body))
	rr2, _ := vllmAdapter{}.FromAnthropic([]byte(body))
	id1 := rr1.Messages[0].ToolCalls[0].ID
	if id1 != "call_0" || id1 != rr2.Messages[0].ToolCalls[0].ID {
		t.Fatalf("generated ids must be deterministic call_0, got %q / %q", id1, rr2.Messages[0].ToolCalls[0].ID)
	}
}

// AdapterFor wiring: vllm -> vllmAdapter, anything else -> sglangAdapter.
func TestAdapterForSelection(t *testing.T) {
	if _, ok := AdapterFor("vllm").(vllmAdapter); !ok {
		t.Fatalf("AdapterFor(vllm) should be vllmAdapter")
	}
	if _, ok := AdapterFor("sglang").(sglangAdapter); !ok {
		t.Fatalf("AdapterFor(sglang) should be sglangAdapter")
	}
	if _, ok := AdapterFor("").(sglangAdapter); !ok {
		t.Fatalf("AdapterFor(\"\") should default to sglangAdapter")
	}
	if _, ok := AdapterFor("unknown").(sglangAdapter); !ok {
		t.Fatalf("AdapterFor(unknown) should default to sglangAdapter")
	}
}

// Sanity: the SGLang adapter still applies its own rules (newline system join,
// thinking dropped) via the same AdapterFor path.
func TestSGLangAdapterStillNewlineSystem(t *testing.T) {
	rr, err := AdapterFor("sglang").FromAnthropic([]byte(`{
		"model":"m","max_tokens":1,
		"system":[{"type":"text","text":"a"},{"type":"text","text":"b"}],
		"messages":[{"role":"user","content":"hi"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rr.Messages[0].Content != "a\nb" {
		t.Fatalf("sglang system should newline-join: %q", rr.Messages[0].Content)
	}
}
