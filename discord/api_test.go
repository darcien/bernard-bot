package discord

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureRequest(t *testing.T) (contentType, body *string) {
	t.Helper()
	var ct, b string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		b = string(data)
	}))
	t.Cleanup(srv.Close)

	orig := DiscordAPIBase
	DiscordAPIBase = srv.URL
	t.Cleanup(func() { DiscordAPIBase = orig })
	return &ct, &b
}

// Replies over the message limit become markdown attachments instead of
// being rejected by Discord.
func TestCreateMessage_LongContentBecomesAttachment(t *testing.T) {
	contentType, body := captureRequest(t)

	t.Run("short reply is one JSON message", func(t *testing.T) {
		if err := CreateMessage("chan", "short answer"); err != nil {
			t.Fatal(err)
		}
		if *contentType != "application/json" {
			t.Errorf("want JSON body, got %q", *contentType)
		}
		if !strings.Contains(*body, "short answer") {
			t.Errorf("want content in body, got %s", *body)
		}
	})

	t.Run("long reply is multipart with the text attached", func(t *testing.T) {
		long := strings.Repeat("a", 2500)
		if err := CreateMessage("chan", long); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(*contentType, "multipart/form-data") {
			t.Fatalf("want multipart, got %q", *contentType)
		}
		if !strings.Contains(*body, "response.md") || !strings.Contains(*body, long) {
			t.Error("want the reply in an attachment part")
		}
	})
}

func TestCreateFollowupMessage(t *testing.T) {
	contentType, body := captureRequest(t)

	if err := CreateFollowupMessage("app", "tok", "deferred answer"); err != nil {
		t.Fatal(err)
	}
	if *contentType != "application/json" {
		t.Errorf("want JSON body, got %q", *contentType)
	}
	if !strings.Contains(*body, "deferred answer") {
		t.Errorf("want content in body, got %s", *body)
	}
}
