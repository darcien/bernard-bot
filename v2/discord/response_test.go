package discord

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondFromResult_Short(t *testing.T) {
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, "hello")

	res := w.Result()
	if res.StatusCode != 200 {
		t.Fatalf("want 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}

	var body MessageResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Type != InteractionResponseTypeChannelMessageWithSource {
		t.Errorf("want type %d, got %d", InteractionResponseTypeChannelMessageWithSource, body.Type)
	}
	if body.Data.Content != "hello" {
		t.Errorf("want content %q, got %q", "hello", body.Data.Content)
	}
}

func TestRespondFromResult_DefaultResponseType(t *testing.T) {
	w := httptest.NewRecorder()
	RespondFromResult(w, 0, "hello")

	var body MessageResponse
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body.Type != InteractionResponseTypeChannelMessageWithSource {
		t.Errorf("want default type %d, got %d", InteractionResponseTypeChannelMessageWithSource, body.Type)
	}
}

func TestRespondFromResult_ExactLimit(t *testing.T) {
	text := strings.Repeat("a", 2000)
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, text)

	if ct := w.Result().Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("want JSON at exactly 2000 chars, got Content-Type %q", ct)
	}
}

func TestRespondFromResult_Multipart(t *testing.T) {
	text := strings.Repeat("a", 2001)
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, text)

	res := w.Result()
	ct := res.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("want multipart/form-data, got %q (err: %v)", ct, err)
	}

	mr := multipart.NewReader(res.Body, params["boundary"])

	// Part 1: payload_json
	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("reading payload_json part: %v", err)
	}
	if part.FormName() != "payload_json" {
		t.Errorf("want form name payload_json, got %q", part.FormName())
	}
	var payload MessageResponse
	if err := json.NewDecoder(part).Decode(&payload); err != nil {
		t.Fatalf("decode payload_json: %v", err)
	}
	if payload.Type != InteractionResponseTypeChannelMessageWithSource {
		t.Errorf("want type %d, got %d", InteractionResponseTypeChannelMessageWithSource, payload.Type)
	}
	if len(payload.Data.Attachments) != 1 || payload.Data.Attachments[0].Filename != "response.md" {
		t.Errorf("want attachment response.md, got %+v", payload.Data.Attachments)
	}

	// Part 2: files[0]
	part, err = mr.NextPart()
	if err != nil {
		t.Fatalf("reading files[0] part: %v", err)
	}
	if part.FormName() != "files[0]" {
		t.Errorf("want form name files[0], got %q", part.FormName())
	}
	if part.FileName() != "response.md" {
		t.Errorf("want filename response.md, got %q", part.FileName())
	}
	var sb strings.Builder
	io.Copy(&sb, part)
	buf := &sb
	if buf.String() != text {
		t.Errorf("file content mismatch")
	}
}

func TestUtf16Len(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"bmp unicode", "café", 4},
		// 🐴 is U+1F434 (> U+FFFF), costs 2 UTF-16 code units
		{"single emoji", "🐴", 2},
		{"emoji in text", "hi 🐴!", 6},
		// 🇯🇵 is two regional indicator symbols, each > U+FFFF
		{"flag emoji", "🇯🇵", 4},
		{"mixed", "a🐴b🎉c", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf16Len(tc.s); got != tc.want {
				t.Errorf("utf16Len(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

func TestRespondFromResult_EmojiAtBoundary(t *testing.T) {
	// 1999 ASCII chars + 🐴 (2 UTF-16 code units) = 2001 JS .length → multipart
	text := strings.Repeat("a", 1999) + "🐴"
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, text)

	ct := w.Result().Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "multipart/form-data" {
		t.Errorf("emoji pushing past 2000 UTF-16 units should trigger multipart, got %q", ct)
	}
}

func TestRespondFromResult_EmojiUnderLimit(t *testing.T) {
	// 1998 ASCII chars + 🐴 (2 UTF-16 code units) = 2000 JS .length → JSON
	text := strings.Repeat("a", 1998) + "🐴"
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, text)

	if ct := w.Result().Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("exactly 2000 UTF-16 units should stay JSON, got %q", ct)
	}
}

func TestWriteJSON_UnmarshalableValue(t *testing.T) {
	w := httptest.NewRecorder()
	// channels cannot be marshaled to JSON
	WriteJSON(w, 200, make(chan int))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", w.Code)
	}
}

func TestRespondFromResult_MultipartStripsMarkdownFences(t *testing.T) {
	// Matches the fence-stripping behavior in makeReplyAsMarkdownAttachment (TS).
	content := strings.Repeat("x", 1500) + "\n```markdown\n# heading\n```\n" + strings.Repeat("y", 500)
	w := httptest.NewRecorder()
	RespondFromResult(w, InteractionResponseTypeChannelMessageWithSource, content)

	res := w.Result()
	_, params, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	mr := multipart.NewReader(res.Body, params["boundary"])
	mr.NextPart() // skip payload_json

	part, _ := mr.NextPart()
	var sb strings.Builder
	io.Copy(&sb, part)
	buf := &sb
	got := buf.String()

	if strings.Contains(got, "```markdown") {
		t.Error("expected ```markdown fence to be stripped")
	}
	if strings.Contains(got, "# heading") == false {
		t.Error("expected content between fences to be preserved")
	}
}
