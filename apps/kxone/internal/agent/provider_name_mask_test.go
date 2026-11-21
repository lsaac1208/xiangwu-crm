package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// v29 A21-2 (费用 question) was stopped twice by unknown_name. Reproduced:
// “费用较高的说法” matched 高+的+说. Degree adverb + surname is an adjective.
func TestDegreeAdverbSurnameIsNotAName(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, text := range []string{"费用较高的说法", "家长觉得价格偏高的", "费用太高会影响决定", "比较高的意向", "认为学费很高说要考虑"} {
		clean, n := r.sanitize(text)
		if n != 0 || clean != normalizedSensitiveText(text) {
			t.Errorf("business phrase damaged: %q => %q (%d)", text, clean, n)
		}
	}
	for _, text := range []string{"高明说想来校", "客户：高明", "王小明表示同意", "高先生"} {
		if clean, n := r.sanitize(text); n == 0 {
			t.Errorf("name heuristic weakened: %q => %q", text, clean)
		}
	}
}

func TestNameHintsCarryOnlySurnameAndCategories(t *testing.T) {
	r := newRedactor()
	r.prepare()
	hints := nameHints(r, "跟进：王小明表示费用太贵")
	if len(hints) == 0 || hints[0].Surname != "王" || hints[0].Before != "punct" || hints[0].Rule != "prose" {
		t.Fatalf("hints: %+v", hints)
	}
	raw, _ := json.Marshal(hints)
	for _, leaked := range []string{"小明", "费用", "跟进"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("hint leaked text %q: %s", leaked, raw)
		}
	}
}

func finalizeCall(t *testing.T, name string, args map[string]any) ToolCall {
	raw, _ := json.Marshal(args)
	var c ToolCall
	c.ID, c.Type = "call", "function"
	c.Function.Name, c.Function.Arguments = name, string(raw)
	return c
}

func TestUnknownNameInPresentationProseIsMaskedNotFatal(t *testing.T) {
	snapshot := measuredFixture(t)
	snapshot.redactor.addName("陆青岚", "客户#7")
	snapshot.redactor.prepare()
	id := "11111111-1111-4111-8111-111111111111"
	msg := ModelMessage{Role: "assistant", ToolCalls: []ToolCall{finalizeCall(t, "finalize_answer", map[string]any{"result_ids": []string{id}, "markdown": "跟进中王小明表示费用太贵，不能证明费用造成流失。"})}}
	findings := providerPrivacyFindings(snapshot, msg)
	if len(findings) != 1 || findings[0].Rule != "unknown_name" || len(findings[0].Hints) == 0 {
		t.Fatalf("expected one unknown_name finding with hints: %+v", findings)
	}
	masked, ok := maskUnknownNames(snapshot, msg, findings)
	if !ok || len(providerPrivacyFindings(snapshot, masked)) != 0 {
		t.Fatalf("mask failed: ok=%v %+v", ok, masked)
	}
	args := masked.ToolCalls[0].Function.Arguments
	if strings.Contains(args, "王小明") || !strings.Contains(args, "[身份片段未纳入分析]") || !strings.Contains(args, id) || !strings.Contains(args, "费用造成流失") {
		t.Fatalf("masking changed the wrong parts: %s", args)
	}
	// Known customer names, contacts and secrets still reject the turn.
	for _, text := range []string{"陆青岚说费用太贵", "王小明电话13800138000"} {
		bad := ModelMessage{Role: "assistant", ToolCalls: []ToolCall{finalizeCall(t, "finalize_answer", map[string]any{"result_ids": []string{id}, "markdown": text})}}
		if _, ok := maskUnknownNames(snapshot, bad, providerPrivacyFindings(snapshot, bad)); ok {
			t.Errorf("known identity/contact masked instead of rejected: %q", text)
		}
	}
	// Query arguments and native reasoning are never edited.
	query := ModelMessage{Role: "assistant", ToolCalls: []ToolCall{finalizeCall(t, "query_data", map[string]any{"sql": "SELECT * FROM leads WHERE note LIKE '%王小明表示%'"})}}
	if _, ok := maskUnknownNames(snapshot, query, providerPrivacyFindings(snapshot, query)); ok {
		t.Error("query arguments were masked")
	}
	reasoning := "王小明表示同意"
	withReasoning := msg
	withReasoning.ReasoningContent = &reasoning
	if _, ok := maskUnknownNames(snapshot, withReasoning, providerPrivacyFindings(snapshot, withReasoning)); ok {
		t.Error("turn with native reasoning was masked")
	}
}
