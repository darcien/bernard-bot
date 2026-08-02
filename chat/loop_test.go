package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bernard/llm"
	"bernard/tools"
)

// sourcedTool stands in for web_fetch: it cites where its output came from.
type sourcedTool struct {
	calls  int
	source string
	result string // when set, returned instead of the default page text
}

func (s *sourcedTool) Name() string            { return "fetch" }
func (s *sourcedTool) Description() string     { return "fetch" }
func (s *sourcedTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *sourcedTool) Source(json.RawMessage) string {
	return s.source
}
func (s *sourcedTool) Execute(context.Context, json.RawMessage) (string, error) {
	s.calls++
	if s.result != "" {
		return s.result, nil
	}
	return "page text", nil
}

// The model must receive the citation number with the content, and the
// harness must keep its own record of what the number points at.
func TestRunToolLoop_NumbersSources(t *testing.T) {
	var toolResults []string
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "page text") {
			toolResults = append(toolResults, string(body))
		}
		round++
		if round == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"fetch","arguments":"{}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"per the page [1], yes"}}]}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry(toolResultCap, &sourcedTool{source: "https://example.com/x"})
	client := llm.NewClient(srv.URL, "k", "m")
	prompt := []llm.Message{{Role: "user", Content: "u: go"}}

	res, err := runToolLoop(context.Background(), client, reg, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.sources) != 1 || res.sources[0] != "https://example.com/x" {
		t.Errorf("want the source recorded, got %v", res.sources)
	}
	if len(toolResults) == 0 || !strings.Contains(toolResults[0], `[1] source: https://example.com/x`) {
		t.Error("want the citation marker sent to the model with the tool result")
	}
}

func TestLoopResult_CiteReusesNumberForSamePlace(t *testing.T) {
	var res loopResult
	if n := res.cite("https://a.example"); n != 1 {
		t.Errorf("want first source to be [1], got [%d]", n)
	}
	if n := res.cite("https://b.example"); n != 2 {
		t.Errorf("want second source to be [2], got [%d]", n)
	}
	if n := res.cite("https://a.example"); n != 1 {
		t.Errorf("want a repeated source to reuse [1], got [%d]", n)
	}
	if len(res.sources) != 2 {
		t.Errorf("want 2 distinct sources, got %v", res.sources)
	}
}

type loopTool struct{ calls int }

func (l *loopTool) Name() string            { return "probe" }
func (l *loopTool) Description() string     { return "probe" }
func (l *loopTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (l *loopTool) Execute(context.Context, json.RawMessage) (string, error) {
	l.calls++
	return "probed", nil
}

// A message that arrives mid-turn is folded into the run after the round's
// tool results, so the model reads it without a second turn. It reaches the
// model marked as guidance and reaches history plain — the marker is
// transport, not something the next turn should read back as conversation.
func TestRunToolLoop_SteersMidTurnMessageIntoTheRun(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, string(body))
		if len(requests) == 1 {
			_, _ = w.Write([]byte(wantsToolReply))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry(toolResultCap, &loopTool{})
	client := llm.NewClient(srv.URL, "k", "m")
	prompt := []llm.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u: go"}}

	steers := 0
	steer := func() (llm.Message, bool) {
		steers++
		if steers > 1 {
			return llm.Message{}, false // only one message was waiting
		}
		return llm.Message{Role: "user", Content: "budi: make it short"}, true
	}

	res, err := runToolLoop(context.Background(), client, reg, prompt, steer)
	if err != nil {
		t.Fatal(err)
	}

	if res.steers != 1 || res.rounds != 2 {
		t.Errorf("want one steer folded into two rounds, got %+v", res)
	}
	if len(requests) != 2 {
		t.Fatalf("want 2 LLM calls, got %d", len(requests))
	}
	if !strings.Contains(requests[1], steerPrefix) {
		t.Error("want the mid-turn message marked as guidance for the model")
	}
	if !strings.Contains(requests[1], "make it short") {
		t.Error("want the mid-turn message in the next round's prompt")
	}

	var found bool
	for _, m := range res.produced {
		if strings.Contains(m.Content, steerPrefix) {
			t.Error("want the marker kept out of history")
		}
		if m.Role == "user" && m.Content == "budi: make it short" {
			found = true
		}
	}
	if !found {
		t.Error("want the plain mid-turn message committed to history")
	}
}

// The injection point is after the round's tool results, so every tool_call
// still has its answer — DeepSeek 400s on a pair split by another message.
func TestRunToolLoop_SteerKeepsToolCallPairing(t *testing.T) {
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		if round == 1 {
			_, _ = w.Write([]byte(wantsToolReply))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry(toolResultCap, &loopTool{})
	client := llm.NewClient(srv.URL, "k", "m")
	prompt := []llm.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u: go"}}

	once := true
	res, err := runToolLoop(context.Background(), client, reg, prompt, func() (llm.Message, bool) {
		if !once {
			return llm.Message{}, false
		}
		once = false
		return llm.Message{Role: "user", Content: "budi: also this"}, true
	})
	if err != nil {
		t.Fatal(err)
	}

	// Every assistant tool_call must be followed by its tool result before
	// anything else appears.
	for i, m := range res.produced {
		if len(m.ToolCalls) == 0 {
			continue
		}
		for j, call := range m.ToolCalls {
			next := res.produced[i+1+j]
			if next.Role != "tool" || next.ToolCallID != call.ID {
				t.Fatalf("tool_call %q not answered in place: %+v", call.ID, next)
			}
		}
	}
}

// The reported prompt size is the last call's, not the first or the sum: a
// later round carries the tool results, so it is the one that measures the
// session against the window.
func TestRunToolLoop_ReportsTheLastPromptSize(t *testing.T) {
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		if round == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"probe","arguments":"{}"}}]}}],
				"usage":{"prompt_tokens":100}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}],
			"usage":{"prompt_tokens":900}}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry(toolResultCap, &loopTool{})
	client := llm.NewClient(srv.URL, "k", "m")
	prompt := []llm.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u: go"}}

	res, err := runToolLoop(context.Background(), client, reg, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}

	if res.lastPromptTokens != 900 {
		t.Errorf("want the last call's tokens, got %d", res.lastPromptTokens)
	}
	if res.usage.PromptTokens != 1000 {
		t.Errorf("want the sum kept for cost accounting, got %d", res.usage.PromptTokens)
	}
	// Paired with the same call: the second prompt carries the tool result,
	// so it must measure larger than the first.
	if res.lastPromptBytes <= unitSize(prompt) {
		t.Errorf("want the grown prompt measured, got %d", res.lastPromptBytes)
	}
}

const wantsToolReply = `{"choices":[{"message":{"role":"assistant","content":"",
	"tool_calls":[{"id":"c1","type":"function","function":{"name":"probe","arguments":"{}"}}]}}]}`

// A model that never stops calling tools gets maxToolRounds rounds, then one
// grace round with tools stripped and a nudge — and the nudge must not leak
// into the produced messages that get committed to the session.
func TestRunToolLoop_GraceRound(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, string(body))
		if len(requests) <= maxToolRounds {
			_, _ = w.Write([]byte(wantsToolReply))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"best guess"}}]}`))
	}))
	defer srv.Close()

	tool := &loopTool{}
	reg := tools.NewRegistry(toolResultCap, tool)
	client := llm.NewClient(srv.URL, "k", "m")
	prompt := []llm.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u: go"}}

	res, err := runToolLoop(context.Background(), client, reg, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, produced := res.reply, res.produced

	if reply != "best guess" {
		t.Errorf("want grace-round answer, got %q", reply)
	}
	if !res.grace || res.rounds != maxToolRounds+1 || res.toolCalls != maxToolRounds {
		t.Errorf("want grace round counted in the summary, got %+v", res)
	}
	if tool.calls != maxToolRounds {
		t.Errorf("want %d tool executions, got %d", maxToolRounds, tool.calls)
	}
	if len(requests) != maxToolRounds+1 {
		t.Fatalf("want %d LLM calls, got %d", maxToolRounds+1, len(requests))
	}

	grace := requests[len(requests)-1]
	if strings.Contains(grace, `"tools"`) {
		t.Error("want grace round without tools")
	}
	if !strings.Contains(grace, graceNudge) {
		t.Error("want grace nudge in final request")
	}
	for _, m := range produced {
		if strings.Contains(m.Content, graceNudge) {
			t.Error("want nudge excluded from produced messages")
		}
	}
}

// The citation line is prefixed after the registry has capped the result, so
// a long URL — the model picks it — would otherwise push the tool message
// past toolResultCap by however long that URL is.
func TestRunToolLoop_CitationLineCannotBlowTheCap(t *testing.T) {
	longURL := "https://example.com/" + strings.Repeat("a", citationURLCap*2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "\"role\":\"tool\"") {
			_, _ = w.Write([]byte(finalReply("done")))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",
			"tool_calls":[{"id":"c1","type":"function","function":{"name":"fetch","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry(toolResultCap, &sourcedTool{source: longURL, result: strings.Repeat("x", toolResultCap*2)})
	res, err := runToolLoop(context.Background(), llm.NewClient(srv.URL, "k", "m"), reg,
		[]llm.Message{{Role: "user", Content: "u: go"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var toolMsg llm.Message
	for _, m := range res.produced {
		if m.Role == "tool" {
			toolMsg = m
		}
	}
	if toolMsg.Content == "" {
		t.Fatal("want a tool result recorded")
	}
	over := len(toolMsg.Content) - toolResultCap
	if over > citationURLCap {
		t.Errorf("want the citation line bounded, result runs %d bytes over the cap", over)
	}
	// A cut URL is a broken link that looks like a working one, so the line
	// says the URL is missing instead of showing part of it.
	if strings.Contains(toolMsg.Content, longURL[:100]) {
		t.Error("want no part of the oversized URL shown to the model")
	}
	// The origin survives, because naming where something came from is what
	// the model needs the source for.
	if !strings.Contains(toolMsg.Content, "https://example.com/…") {
		t.Errorf("want the origin kept and the path elided, got %q", toolMsg.Content[:120])
	}
	// The footer still cites the whole URL: only the model's copy is bounded.
	if len(res.sources) != 1 || res.sources[0] != longURL {
		t.Error("want the full URL recorded as the source")
	}
}

func TestCitationLabel(t *testing.T) {
	short := "https://example.com/a/b?c=d"
	if got := citationLabel(short); got != short {
		t.Errorf("want a normal URL untouched, got %q", got)
	}

	long := "https://news.example.com/" + strings.Repeat("x", citationURLCap)
	got := citationLabel(long)
	if !strings.HasPrefix(got, "https://news.example.com/…") {
		t.Errorf("want the origin kept, got %q", got)
	}
	if len(got) > citationURLCap {
		t.Errorf("want the label bounded, got %d bytes", len(got))
	}
	if strings.Contains(got, "xxxx") {
		t.Error("want the path gone rather than cut")
	}

	// Not every source is parseable; a tool can report anything as its origin.
	if got := citationLabel(strings.Repeat("%", citationURLCap+1)); !strings.Contains(got, "too long") {
		t.Errorf("want the unparseable case named, got %q", got)
	}
}
