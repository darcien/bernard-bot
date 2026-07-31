package llm

import (
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
