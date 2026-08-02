package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bernard/logid"
)

// A turn is one user waiting on one answer, so retries are sized against the
// turn rather than the process: the caller's timeout is the real ceiling, and
// these attempts have to fit inside it alongside the call they retry.
// Reasonix allows ten, which suits a CLI a person can watch; here the fourth
// attempt is already a minute of silence in a chat channel.
const (
	maxRetries = 3
	maxBackoff = 15 * time.Second
)

// retryBase is the first backoff, doubling from there. A var so tests can
// shrink it; nothing else writes to it.
var retryBase = 500 * time.Millisecond

// send POSTs the request body, retrying what a retry can plausibly fix:
// 408, 429, 5xx, and transient network errors. Everything else — a 400 for a
// malformed history, a 401 for a bad key — is a caller problem that will fail
// identically the next time, so it comes straight back.
//
// The response body is read here, so a retried attempt is always a whole
// request rather than a resumed one. Nothing streams, so there is no
// half-emitted answer to worry about replaying.
func (c *Client) send(ctx context.Context, url string, reqBody []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, backoff(attempt, retryAfterOf(lastErr))); err != nil {
				return nil, lastErr // the turn ran out of time; report the cause
			}
			slog.Debug("llm retry", append([]any{"attempt", attempt, "err", lastErr}, logid.Attrs(ctx)...)...)
		}
		body, err := c.attempt(ctx, url, reqBody)
		if err == nil {
			return body, nil
		}
		if !retryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *Client) attempt(ctx context.Context, url string, reqBody []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{
			status:     resp.StatusCode,
			endpoint:   req.URL.Host + req.URL.Path,
			body:       truncate(string(body), 300),
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	return body, nil
}

// apiError is a non-2xx response. It carries Retry-After so the backoff can
// honour a server that said when to come back.
type apiError struct {
	status     int
	endpoint   string
	body       string
	retryAfter time.Duration
}

func (e *apiError) Error() string {
	return "llm API " + strconv.Itoa(e.status) + " from " + e.endpoint + ": " + e.body
}

// retryable reports whether another attempt could plausibly succeed. A
// cancelled or expired context never can — the caller has stopped waiting.
func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if apiErr, ok := errors.AsType[*apiError](err); ok {
		s := apiErr.status
		return s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || (s >= 500 && s <= 599)
	}
	return true // a transport error: connection reset, DNS blip, timeout on the wire
}

func retryAfterOf(err error) time.Duration {
	if apiErr, ok := errors.AsType[*apiError](err); ok {
		return apiErr.retryAfter
	}
	return 0
}

// backoff is Reasonix's: exponential from 500ms, capped, with jitter so
// several channels rate-limited at once do not come back in lockstep. A
// Retry-After wins outright, still capped — a server asking for an hour is
// asking for longer than any turn has.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, maxBackoff)
	}
	d := min(time.Duration(1<<(attempt-1))*retryBase, maxBackoff)
	return d + time.Duration(rand.Intn(250))*time.Millisecond
}

// parseRetryAfter reads the delta-seconds form. The HTTP-date form is legal
// and unused by the endpoints here; a date that cannot be parsed simply falls
// back to the exponential delay.
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
