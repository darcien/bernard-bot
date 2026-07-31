package discord

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
)

const maxMessageLength = 2000

var (
	rgxMarkdownOpen  = regexp.MustCompile("(?m)^```markdown$")
	rgxMarkdownClose = regexp.MustCompile("(?m)^```$")
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// RespondFromResult sends a JSON or multipart response depending on text length,
// matching the behavior of makeWebhookResponseFromHandlerResult in webhook_response.ts.
func RespondFromResult(w http.ResponseWriter, responseType InteractionResponseType, text string) {
	rt := responseType
	if rt == 0 {
		rt = InteractionResponseTypeChannelMessageWithSource
	}

	if utf16Len(text) > maxMessageLength {
		respondMultipart(w, rt, text)
		return
	}

	WriteJSON(w, http.StatusOK, MessageResponse{
		Type: rt,
		Data: MessageResponseData{Content: text},
	})
}

// respondMultipart sends a Discord file attachment response with content as response.md.
// Matches makeReplyAsMarkdownAttachment in webhook_response.ts.
// https://discord.com/developers/docs/reference#uploading-files
func respondMultipart(w http.ResponseWriter, responseType InteractionResponseType, content string) {
	payloadJSON, _ := json.Marshal(MessageResponse{
		Type: responseType,
		Data: MessageResponseData{
			Attachments: []Attachment{{ID: 0, Filename: "response.md"}},
		},
	})

	contentType, body := multipartAttachment(payloadJSON, content)

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// multipartAttachment builds a multipart body with payloadJSON and content as
// a response.md file part. Shared by interaction responses and followups.
func multipartAttachment(payloadJSON []byte, content string) (contentType string, body []byte) {
	// Strip ```markdown ... ``` fences — same hack as the TS version.
	if bytes.Contains([]byte(content), []byte("```markdown")) {
		content = rgxMarkdownOpen.ReplaceAllString(content, "")
		content = rgxMarkdownClose.ReplaceAllString(content, "\n")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h1 := textproto.MIMEHeader{}
	h1.Set("Content-Disposition", `form-data; name="payload_json"`)
	h1.Set("Content-Type", "application/json")
	p1, _ := mw.CreatePart(h1)
	_, _ = p1.Write(payloadJSON)

	h2 := textproto.MIMEHeader{}
	h2.Set("Content-Disposition", `form-data; name="files[0]"; filename="response.md"`)
	h2.Set("Content-Type", "text/markdown")
	p2, _ := mw.CreatePart(h2)
	_, _ = p2.Write([]byte(content))

	mw.Close()

	return mw.FormDataContentType(), buf.Bytes()
}

// utf16Len returns the number of UTF-16 code units in s,
// matching JavaScript's String.length which Discord uses for its limit.
// Runes > U+FFFF (e.g. emoji) require 2 code units (a surrogate pair).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
