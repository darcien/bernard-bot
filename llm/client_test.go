package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplete(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello friend"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/", "test-key", "test-model")
	reply, err := c.Complete("be nice", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "hello friend" {
		t.Errorf("want %q, got %q", "hello friend", reply)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("want /chat/completions (trailing slash trimmed), got %q", gotPath)
	}
	if gotReq.Model != "test-model" {
		t.Errorf("want model test-model, got %q", gotReq.Model)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" || gotReq.Messages[1].Role != "user" {
		t.Errorf("want [system, user] messages, got %+v", gotReq.Messages)
	}
	if gotReq.Thinking.Type != "disabled" {
		t.Errorf("want thinking disabled, got %q", gotReq.Thinking.Type)
	}
}

func TestComplete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model overloaded"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, err := c.Complete("s", "u")
	if err == nil {
		t.Fatal("want error on 503")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "model overloaded") {
		t.Errorf("want status and body in error, got %v", err)
	}
}

func TestComplete_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	reply, err := c.Complete("s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "" {
		t.Errorf("want empty reply, got %q", reply)
	}
}

func captureServer(t *testing.T, rawBody *string, response string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*rawBody = string(body)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "k", "test-model")
}

func TestChat_ToolCallRoundTrip(t *testing.T) {
	var rawBody string
	c := captureServer(t, &rawBody, `{"choices":[{"message":{
		"role":"assistant","content":"",
		"tool_calls":[{"id":"call_1","type":"function","function":{"name":"current_time","arguments":"{}"}}]
	}}]}`)

	tools := json.RawMessage(`[{"type":"function","function":{"name":"current_time"}}]`)
	m, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "what time"}}, tools)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(rawBody, `"tools":[{"type":"function"`) {
		t.Errorf("want tools array in request, got %s", rawBody)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].Function.Name != "current_time" || m.ToolCalls[0].ID != "call_1" {
		t.Errorf("want parsed tool call, got %+v", m.ToolCalls)
	}
}

// A tool result names its producer so maintenance can ask it for geometry.
// Nothing else carries a name.
func TestChat_ToolResultCarriesTheToolName(t *testing.T) {
	var rawBody string
	c := captureServer(t, &rawBody, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)

	msgs := []Message{
		{Role: "user", Content: "what's on hn"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function",
			Function: FunctionCall{Name: "web_fetch", Arguments: "{}"}}}},
		{Role: "tool", Name: "web_fetch", ToolCallID: "call_1", Content: "page"},
	}
	if _, _, err := c.Chat(context.Background(), msgs, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawBody, `"name":"web_fetch","tool_call_id":"call_1"`) {
		t.Errorf("want the tool result to carry its name, got %s", rawBody)
	}
	if strings.Count(rawBody, `"name":"web_fetch"`) != 2 {
		// Once in tool_calls, once on the result, nowhere else.
		t.Errorf("want the name only where it belongs, got %s", rawBody)
	}
}

func TestChat_NilToolsOmitted(t *testing.T) {
	var rawBody string
	c := captureServer(t, &rawBody, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)

	if _, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawBody, `"tools"`) {
		t.Errorf("want no tools field when nil, got %s", rawBody)
	}
	if !strings.Contains(rawBody, `"max_tokens":2048`) {
		t.Errorf("want explicit max_tokens (provider defaults cut replies), got %s", rawBody)
	}
}

// DeepSeek's strict deserializer rejects a message missing the content
// field, so even an assistant message carrying only tool calls must
// serialize "content":"".
func TestChat_ContentFieldAlwaysPresent(t *testing.T) {
	var rawBody string
	c := captureServer(t, &rawBody, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)

	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "t", Arguments: "{}"}}}},
		{Role: "tool", Content: "result", ToolCallID: "call_1"},
	}
	if _, _, err := c.Chat(context.Background(), msgs, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawBody, `"content":""`) {
		t.Errorf("want empty content field serialized, got %s", rawBody)
	}
	if !strings.Contains(rawBody, `"tool_call_id":"call_1"`) {
		t.Errorf("want tool_call_id serialized, got %s", rawBody)
	}
}
