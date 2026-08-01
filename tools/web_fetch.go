package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const webFetchBodyLimit = 1 << 20 // 1MB of raw body is plenty for text

var (
	rgxScriptStyle = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	rgxTag         = regexp.MustCompile(`<[^>]*>`)
	rgxWhitespace  = regexp.MustCompile(`\s+`)
)

// WebFetch fetches a URL and returns its text content. Any Discord user can
// point it at an arbitrary URL, so it refuses non-http(s) schemes and
// validates resolved IPs at dial time — resolving inside the dialer (rather
// than check-then-connect) closes the DNS-rebinding gap, and redirects are
// covered for free because every hop goes through the same dialer.
type WebFetch struct {
	client    *http.Client
	resultCap int
	// allowLocal disables the private-IP guard; tests only.
	allowLocal bool
}

func NewWebFetch(timeout time.Duration, resultCap int) *WebFetch {
	f := &WebFetch{resultCap: resultCap}
	f.client = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: f.dialVetted,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return f
}

func (f *WebFetch) Name() string { return "web_fetch" }

func (f *WebFetch) Description() string {
	return "Fetch a public http(s) URL and return its text content. Use to read web pages, articles, or APIs the user asks about."
}

func (f *WebFetch) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "Absolute http or https URL to fetch."
			}
		},
		"required": ["url"]
	}`)
}

// Source reports the URL the model asked for, so the reply can cite it.
func (f *WebFetch) Source(args json.RawMessage) string {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return ""
	}
	return params.URL
}

func (f *WebFetch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	u, err := url.Parse(params.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("need an absolute http(s) URL, got %q", params.URL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "bernard-bot (+https://github.com/darcien/bernard-bot)")

	start := time.Now()
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchBodyLimit))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	if strings.ContainsRune(string(body), 0) {
		return "", errors.New("binary content, not text")
	}

	text := TruncateHeadTail(htmlToText(string(body)), f.resultCap)
	// Transport facts only — what the tool layer can't see: where the
	// request actually landed after redirects, and how much of the page
	// survived stripping and truncation.
	attrs := []any{
		"url", resp.Request.URL.String(), // post-redirect
		"status", resp.StatusCode,
		"type", resp.Header.Get("Content-Type"),
		"bytes", len(body),
		"text_chars", len(text),
		"dur", time.Since(start).Round(time.Millisecond),
	}
	if len(body) == webFetchBodyLimit {
		attrs = append(attrs, "body_limit_hit", true)
	}
	slog.Debug("web fetch", attrs...)
	return text, nil
}

// dialVetted resolves the host, filters the candidate IPs, and dials vetted
// IPs directly so validation and connection share one resolution. Tries each
// allowed IP in turn (a multi-record host may have a dead first address).
func (f *WebFetch) dialVetted(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialErr error
	for _, a := range addrs {
		if !f.allowLocal && blockedIP(a.IP) {
			continue
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	if dialErr != nil {
		return nil, dialErr
	}
	return nil, fmt.Errorf("blocked: %s resolves to a private or local address", host)
}

// blockedIP refuses loopback, private LAN, link-local (includes the cloud
// metadata address 169.254.169.254), and unspecified addresses.
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// htmlToText strips markup down to readable text. Crude by design: drop
// script/style blocks, drop tags, decode entities, collapse whitespace.
func htmlToText(s string) string {
	s = rgxScriptStyle.ReplaceAllString(s, " ")
	s = rgxTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(rgxWhitespace.ReplaceAllString(s, " "))
}
