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

func TestWebFetch_RendersPageAsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><style>body{color:red}</style><script>alert(1)</script></head>
			<body><h1>Sheep &amp; Wool</h1><p>are  great</p></body></html>`)
	}))
	defer srv.Close()

	got, err := newTestFetch().Execute(context.Background(), fetchArgs(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if got != "# Sheep & Wool\n\nare great" {
		t.Errorf("want headings kept and entities decoded, got %q", got)
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

// Markdown and JSON arrive usable; only HTML needs converting, and running
// the others through the HTML parser would mangle them.
func TestReadable(t *testing.T) {
	cases := []struct {
		name, body, contentType, wantFormat, wantContent string
	}{
		{"negotiated markdown", "# Title\n\n- point", "text/markdown; charset=utf-8", "markdown", "# Title\n\n- point"},
		{"html converted", "<html><body><p>hi</p></body></html>", "text/html; charset=utf-8", "html", "hi"},
		{"json left alone", `{"a": "<b>"}`, "application/json", "text", `{"a": "<b>"}`},
		{"plain text left alone", "just words", "text/plain", "text", "just words"},
		{"unlabelled html is sniffed", "<!DOCTYPE html><html><body><p>hi</p></body></html>", "", "html", "hi"},
		{"unlabelled text is not", "just words", "", "text", "just words"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, format := readable(tc.body, tc.contentType)
			if format != tc.wantFormat {
				t.Errorf("want format %q, got %q", tc.wantFormat, format)
			}
			if content != tc.wantContent {
				t.Errorf("want content %q, got %q", tc.wantContent, content)
			}
		})
	}
}

func TestWebFetch_RequestsMarkdownFirst(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprint(w, "# Sheep\n\nthey are fine")
	}))
	defer srv.Close()

	got, err := newTestFetch().Execute(context.Background(), fetchArgs(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	// Ranking matters only when a URL has several representations, but the
	// order must stay deliberate: the reader is a model, so prefer whatever
	// is mostly content, and take HTML — mostly furniture — last.
	if !strings.HasPrefix(accept, "text/markdown") {
		t.Errorf("want markdown preferred in Accept, got %q", accept)
	}
	for _, pair := range [][2]string{
		{"text/markdown", "application/json"},
		{"application/json", "text/plain"},
		{"text/plain", "text/html"},
		{"text/html", "*/*"},
	} {
		if strings.Index(accept, pair[0]) > strings.Index(accept, pair[1]) {
			t.Errorf("want %s ranked above %s, got %q", pair[0], pair[1], accept)
		}
	}
	// A tie hands the choice to the server. html and xhtml are allowed to
	// share one: they're the same representation in two spellings.
	if strings.Count(accept, "q=0.9") > 1 || strings.Count(accept, "q=0.8") > 1 {
		t.Errorf("want no ties between types we rank differently, got %q", accept)
	}
	if got != "# Sheep\n\nthey are fine" {
		t.Errorf("want negotiated markdown passed through untouched, got %q", got)
	}
}

func TestWebFetch_Source(t *testing.T) {
	f := newTestFetch()
	if got := f.Source(fetchArgs("https://example.com/x")); got != "https://example.com/x" {
		t.Errorf("want the requested URL, got %q", got)
	}
	if got := f.Source(json.RawMessage(`not json`)); got != "" {
		t.Errorf("want no source for malformed args, got %q", got)
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
