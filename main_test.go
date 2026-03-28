package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer generates a fresh ed25519 key pair, returns a server with the
// public key and the private key for signing test requests.
func newTestServer(t *testing.T) (*server, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &server{publicKey: pub}, priv
}

// signedRequest builds a POST request to "/" with a valid Discord ED25519 signature.
func signedRequest(t *testing.T, priv ed25519.PrivateKey, body string) *http.Request {
	t.Helper()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	msg := append([]byte(timestamp), []byte(body)...)
	sig := ed25519.Sign(priv, msg)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	req.Header.Set("X-Signature-Timestamp", timestamp)
	return req
}

func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHandlePing(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	s.handlePing(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "Pong!" {
		t.Errorf("want Pong!, got %s", body["message"])
	}
}

func TestHandleInteraction_MissingHeaders(t *testing.T) {
	s, _ := newTestServer(t)

	tests := []struct {
		name      string
		sigHeader string
		tsHeader  string
	}{
		{"missing both", "", ""},
		{"missing signature", "", "12345"},
		{"missing timestamp", "aabbcc", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
			if tc.sigHeader != "" {
				req.Header.Set("X-Signature-Ed25519", tc.sigHeader)
			}
			if tc.tsHeader != "" {
				req.Header.Set("X-Signature-Timestamp", tc.tsHeader)
			}
			w := httptest.NewRecorder()
			s.handleInteraction(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", w.Code)
			}
		})
	}
}

func TestHandleInteraction_InvalidSignature(t *testing.T) {
	s, _ := newTestServer(t)

	body := `{"type":1}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", strings.Repeat("ab", 32)) // wrong sig
	req.Header.Set("X-Signature-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	w := httptest.NewRecorder()
	s.handleInteraction(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestHandleInteraction_Ping(t *testing.T) {
	s, priv := newTestServer(t)

	body := marshalJSON(t, map[string]any{"type": 1})
	req := signedRequest(t, priv, body)

	w := httptest.NewRecorder()
	s.handleInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != float64(1) {
		t.Errorf("want type=1 (Pong), got %v", resp["type"])
	}
}

func TestHandleInteraction_UnknownCommand(t *testing.T) {
	s, priv := newTestServer(t)

	body := marshalJSON(t, map[string]any{
		"type":     2,
		"id":       "1",
		"token":    "tok",
		"guild_id": "g1",
		"channel":  map[string]any{"id": "c1"},
		"member":   map[string]any{"user": map[string]any{"id": "u1", "username": "tester"}},
		"data":     map[string]any{"name": "nonexistent", "options": []any{}},
	})
	req := signedRequest(t, priv, body)

	w := httptest.NewRecorder()
	s.handleInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestHandleInteraction_MissingFields(t *testing.T) {
	s, priv := newTestServer(t)

	base := map[string]any{
		"type":     2,
		"id":       "1",
		"token":    "tok",
		"guild_id": "g1",
		"channel":  map[string]any{"id": "c1"},
		"member":   map[string]any{"user": map[string]any{"id": "u1", "username": "tester"}},
		"data":     map[string]any{"name": "roll", "options": []any{}},
	}

	tests := []struct {
		name   string
		remove string
	}{
		{"missing channel", "channel"},
		{"missing guild_id", "guild_id"},
		{"missing member", "member"},
		{"missing data", "data"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := make(map[string]any, len(base))
			for k, v := range base {
				m[k] = v
			}
			delete(m, tc.remove)

			req := signedRequest(t, priv, marshalJSON(t, m))
			w := httptest.NewRecorder()
			s.handleInteraction(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d", w.Code)
			}
		})
	}
}

func TestHandleInteraction_UnknownType(t *testing.T) {
	s, priv := newTestServer(t)

	body := marshalJSON(t, map[string]any{"type": 99})
	req := signedRequest(t, priv, body)

	w := httptest.NewRecorder()
	s.handleInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestHandleInteraction_RollCommand(t *testing.T) {
	s, priv := newTestServer(t)

	body := marshalJSON(t, map[string]any{
		"type":     2,
		"id":       "1",
		"token":    "tok",
		"guild_id": "g1",
		"channel":  map[string]any{"id": "c1"},
		"member":   map[string]any{"user": map[string]any{"id": "u1", "username": "tester"}},
		"data":     map[string]any{"name": "roll", "options": []any{}},
	})
	req := signedRequest(t, priv, body)

	w := httptest.NewRecorder()
	s.handleInteraction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}
