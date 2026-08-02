package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func shortBackoff(t *testing.T) {
	t.Helper()
	prev := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = prev })
}

// 429 and 5xx are the statuses a wait can actually fix, and the ones the
// provider hands out under load.
func TestSend_RetriesTransientStatuses(t *testing.T) {
	shortBackoff(t)
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(status)
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
		}))

		c := NewClient(srv.URL, "k", "m")
		m, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
		if err != nil {
			t.Errorf("status %d: want a retry to recover, got %v", status, err)
		}
		if m.Content != "ok" {
			t.Errorf("status %d: want the retried reply, got %q", status, m.Content)
		}
		srv.Close()
	}
}

// A 400 is a malformed request: the same bytes will fail the same way, and
// retrying spends the user's turn proving it.
func TestSend_DoesNotRetryClientErrors(t *testing.T) {
	shortBackoff(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "k", "m").Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("want the 400 reported")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("want exactly one attempt, got %d", got)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("want the status in the error, got %v", err)
	}
}

func TestSend_GivesUpAfterMaxRetries(t *testing.T) {
	shortBackoff(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL, "k", "m").Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("want the failure reported after the last attempt")
	}
	if got, want := int(calls.Load()), maxRetries+1; got != want {
		t.Errorf("want %d attempts, got %d", want, got)
	}
}

// A cancelled turn has nobody waiting for the answer; retrying spends the
// endpoint's capacity on a reply nothing will read.
func TestSend_StopsWhenTheCallerGivesUp(t *testing.T) {
	shortBackoff(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewClient(srv.URL, "k", "m").Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if got := calls.Load(); got > 1 {
		t.Errorf("want no retries after cancellation, got %d attempts", got)
	}
}

// A server that says when to come back is obeyed rather than guessed at.
func TestBackoff_HonoursRetryAfterUpToTheCap(t *testing.T) {
	if got := backoff(1, 3*time.Second); got != 3*time.Second {
		t.Errorf("want the server's delay, got %v", got)
	}
	if got := backoff(1, time.Hour); got != maxBackoff {
		t.Errorf("want the cap, got %v", got)
	}
	// Without a header: exponential, plus jitter that never shortens it.
	if got := backoff(2, 0); got < 2*retryBase || got > 2*retryBase+250*time.Millisecond {
		t.Errorf("want the second backoff near %v, got %v", 2*retryBase, got)
	}
}
