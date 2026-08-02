package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bernard/logid"
)

const webFetchBodyLimit = 1 << 20 // 1MB of raw body is plenty for text

// A fetch is "thin" when a substantial page yields text worth a fraction of a
// percent of it. Article HTML lands well above this even after boilerplate is
// stripped; a JS shell lands far below.
const (
	thinExtractMinBytes = 50_000
	thinExtractRatio    = 100 // text × this < body bytes
)

// acceptHeader ranks representations of the *same* URL. These weights only
// decide anything when a server offers several — most pages have one, which
// it sends regardless — and the trailing wildcard means we never provoke a
// 406.
//
// The reader here is a model, not a person: anyone who wanted the page as
// rendered would open the link themselves. So rank by how much of the
// response is content rather than markup, worst last:
//
//   - markdown: the same page converted at the origin, nav and scripts
//     already stripped (Cloudflare's "Markdown for Agents" and similar).
//   - json: structured and chrome-free. Discourse, WordPress and most CMSes
//     serve the post body as JSON for the very same URL.
//   - plain text: no markup to undo, just unstructured.
//   - html last: needs converting, and most of what arrives is furniture.
const acceptHeader = "text/markdown," +
	"application/json;q=0.9," +
	"text/plain;q=0.8," +
	"text/html;q=0.7,application/xhtml+xml;q=0.7," +
	"*/*;q=0.5"

// WebFetch fetches a URL and returns its text content. Any Discord user can
// point it at an arbitrary URL, so it refuses non-http(s) schemes and
// validates resolved IPs at dial time — resolving inside the dialer (rather
// than check-then-connect) closes the DNS-rebinding gap, and redirects are
// covered for free because every hop goes through the same dialer.
//
// It returns the whole extracted text. How much of that reaches the model is
// the registry's decision, applied to every tool alike — a cap here would be
// the same policy in a second place, kept in step only by being handed the
// same constant.
type WebFetch struct {
	client *http.Client
	// allowLocal disables the private-IP guard; tests only.
	allowLocal bool
}

func NewWebFetch(timeout time.Duration) *WebFetch {
	f := &WebFetch{}
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
	req.Header.Set("Accept", acceptHeader)

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

	contentType := resp.Header.Get("Content-Type")
	content, format := readable(string(body), contentType)

	// Transport facts only — what the tool layer can't see: where the
	// request actually landed after redirects, which representation the
	// server gave us, and how much survived conversion. What survived the cap
	// is the registry's line to log, as `truncated_from`.
	attrs := []any{
		"url", resp.Request.URL.String(), // post-redirect
		"status", resp.StatusCode,
		"type", contentType,
		"format", format,
		"bytes", len(body),
		"text_bytes", len(content),
		"dur", time.Since(start).Round(time.Millisecond),
	}
	if len(body) == webFetchBodyLimit {
		attrs = append(attrs, "body_limit_hit", true)
	}
	// A big page that extracts to almost nothing is a JS-rendered site, not a
	// short page. Nothing is truncated in that case, so the registry logs no
	// truncated_from and the two look identical; this is the only thing that
	// says which happened.
	if len(body) >= thinExtractMinBytes && len(content)*thinExtractRatio < len(body) {
		attrs = append(attrs, "thin", true)
	}
	slog.Debug("web fetch", append(attrs, logid.Attrs(ctx)...)...)
	return content, nil
}

// readable turns a response body into text for the model, and names the
// path taken. Markdown and plain text arrive usable — running them through
// an HTML parser would only mangle them (a JSON API answer is not markup).
// HTML is converted; anything unlabelled is sniffed, since servers do
// mislabel content type.
func readable(body, contentType string) (content, format string) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch {
	case mediaType == "text/markdown":
		return strings.TrimSpace(body), "markdown"
	case mediaType == "text/html", mediaType == "application/xhtml+xml":
		return htmlToText(body), "html"
	case mediaType != "":
		return strings.TrimSpace(body), "text"
	case looksLikeHTML(body):
		return htmlToText(body), "html"
	default:
		return strings.TrimSpace(body), "text"
	}
}

// looksLikeHTML sniffs an unlabelled body the way a browser would: check
// only the start, where a doctype or root tag lives.
func looksLikeHTML(body string) bool {
	head := strings.ToLower(strings.TrimSpace(body))
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.HasPrefix(head, "<!doctype html") ||
		strings.HasPrefix(head, "<html") ||
		strings.Contains(head, "<head") ||
		strings.Contains(head, "<body")
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

// cgnat is the carrier-grade NAT range, which some clouds use for their
// metadata service.
var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// blockedIP refuses loopback, private LAN, link-local (includes the cloud
// metadata address 169.254.169.254), CGNAT, and unspecified addresses.
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		cgnat.Contains(ip)
}
