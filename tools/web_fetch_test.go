package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestFetch() *WebFetch {
	f := NewWebFetch(5*time.Second, 4000)
	f.allowLocal = true // httptest listens on loopback
	return f
}

func fetchArgs(url string) json.RawMessage {
	args, _ := json.Marshal(map[string]string{"url": url})
	return args
}

func TestWebFetch_StripsHTMLToText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><style>body{color:red}</style><script>alert(1)</script></head>
			<body><h1>Sheep &amp; Wool</h1><p>are  great</p></body></html>`)
	}))
	defer srv.Close()

	got, err := newTestFetch().Execute(context.Background(), fetchArgs(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Sheep & Wool are great" {
		t.Errorf("want stripped text, got %q", got)
	}
}

func TestWebFetch_ErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := newTestFetch().Execute(context.Background(), fetchArgs(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("want HTTP 403 error, got %v", err)
	}
}

func TestWebFetch_RefusesNonHTTPSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com", "not a url", ""} {
		if _, err := newTestFetch().Execute(context.Background(), fetchArgs(u)); err == nil {
			t.Errorf("want refusal for %q", u)
		}
	}
}

// The SSRF guard: local/private destinations are blocked at dial time by
// default (allowLocal is test-only), so loopback literals, names resolving
// to loopback, and the cloud metadata address all refuse before connecting.
func TestWebFetch_BlocksPrivateAddresses(t *testing.T) {
	f := NewWebFetch(2*time.Second, 4000) // guard ON

	for _, u := range []string{
		"http://127.0.0.1:1/",
		"http://localhost:1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
	} {
		_, err := f.Execute(context.Background(), fetchArgs(u))
		if err == nil {
			t.Errorf("want blocked fetch for %q", u)
			continue
		}
		if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("want dial-time block for %q, got %v", u, err)
		}
	}
}
