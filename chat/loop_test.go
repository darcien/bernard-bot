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
