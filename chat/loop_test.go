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
}

func (s *sourcedTool) Name() string            { return "fetch" }
func (s *sourcedTool) Description() string     { return "fetch" }
func (s *sourcedTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *sourcedTool) Source(json.RawMessage) string {
	return s.source
}
func (s *sourcedTool) Execute(context.Context, json.RawMessage) (string, error) {
	s.calls++
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
