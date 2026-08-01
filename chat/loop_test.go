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

	res, err := runToolLoop(context.Background(), client, reg, prompt)
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

	res, err := runToolLoop(context.Background(), client, reg, prompt)
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
