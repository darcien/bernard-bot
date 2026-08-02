package llm

import (
	"testing"
)

func callMsg(ids ...string) Message {
	m := Message{Role: "assistant"}
	for _, id := range ids {
		m.ToolCalls = append(m.ToolCalls, ToolCall{ID: id, Type: "function",
			Function: FunctionCall{Name: "probe", Arguments: "{}"}})
	}
	return m
}

func resultMsg(id, content string) Message {
	return Message{Role: "tool", ToolCallID: id, Content: content}
}

// A healthy history must come back as the same slice: any copy is a new
// backing array for no reason, and the prefix has to stay byte-identical for
// the provider's cache to hit.
func TestSanitize_WellFormedPassesThrough(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"},
		callMsg("c1", "c2"),
		resultMsg("c1", "one"),
		resultMsg("c2", "two"),
		{Role: "assistant", Content: "done"},
	}
	got := sanitize(msgs)
	if &got[0] != &msgs[0] {
		t.Error("want the input slice returned unchanged")
	}
}

// An unanswered call is what a turn that died mid-round leaves behind, and
// what the endpoint rejects the whole request over.
func TestSanitize_BackfillsUnansweredCall(t *testing.T) {
	msgs := []Message{callMsg("c1", "c2"), resultMsg("c1", "one")}
	got := sanitize(msgs)
	if len(got) != 3 {
		t.Fatalf("want the call plus two results, got %d messages", len(got))
	}
	if got[2].ToolCallID != "c2" || got[2].Content != interruptedToolResult {
		t.Errorf("want a placeholder answering c2, got %+v", got[2])
	}
	// Stands in for a real result, so it carries the same name.
	if got[2].Name != "probe" {
		t.Errorf("want the placeholder named after the call it answers, got %q", got[2].Name)
	}
}

// A name must not push a healthy history off the fast path — re-allocating
// moves the cached prefix for nothing.
func TestSanitize_NamedResultsStayOnTheFastPath(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "u"},
		callMsg("c1"),
		{Role: "tool", Name: "web_fetch", ToolCallID: "c1", Content: "page"},
	}
	got := sanitize(msgs)
	if &got[0] != &msgs[0] {
		t.Error("want the input slice returned unchanged")
	}
}

func TestSanitize_ReordersResultsToMatchCalls(t *testing.T) {
	msgs := []Message{callMsg("c1", "c2"), resultMsg("c2", "two"), resultMsg("c1", "one")}
	got := sanitize(msgs)
	if got[1].ToolCallID != "c1" || got[2].ToolCallID != "c2" {
		t.Errorf("want results in call order, got %q then %q", got[1].ToolCallID, got[2].ToolCallID)
	}
	if got[1].Content != "one" || got[2].Content != "two" {
		t.Error("want each result to keep its own content")
	}
}

// A tool message with no call above it answers nothing; the endpoint rejects
// it, and there is nothing to repair it into.
func TestSanitize_DropsOrphanResults(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "u"},
		resultMsg("gone", "orphan"),
		{Role: "assistant", Content: "done"},
	}
	got := sanitize(msgs)
	if len(got) != 2 {
		t.Fatalf("want the orphan dropped, got %d messages", len(got))
	}
	for _, m := range got {
		if m.Role == "tool" {
			t.Error("want no tool message left")
		}
	}
}

func TestSanitize_ReplacesUnparseableArguments(t *testing.T) {
	call := callMsg("c1")
	call.ToolCalls[0].Function.Arguments = `{"url": "https://exa`
	msgs := []Message{call, resultMsg("c1", "one")}

	got := sanitize(msgs)
	if got[0].ToolCalls[0].Function.Arguments != "{}" {
		t.Errorf("want broken arguments replaced, got %q", got[0].ToolCalls[0].Function.Arguments)
	}
	// Copy-on-write: the caller's history is not the place to fix the wire.
	if call.ToolCalls[0].Function.Arguments == "{}" {
		t.Error("want the caller's message left alone")
	}
}

// Empty arguments are what some gateways send for a no-arg call, and are not
// a defect to repair.
func TestSanitize_KeepsEmptyArguments(t *testing.T) {
	call := callMsg("c1")
	call.ToolCalls[0].Function.Arguments = ""
	msgs := []Message{call, resultMsg("c1", "one")}
	if got := sanitize(msgs); got[0].ToolCalls[0].Function.Arguments != "" {
		t.Errorf("want empty arguments untouched, got %q", got[0].ToolCalls[0].Function.Arguments)
	}
}
