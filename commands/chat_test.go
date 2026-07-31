package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bernard/discord"
	"bernard/llm"
)

// fakeDiscord captures followup POSTs and restores DiscordAPIBase on cleanup.
func fakeDiscord(t *testing.T, capture *[]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(body, &payload)
		*capture = append(*capture, payload.Content)
	}))
	t.Cleanup(srv.Close)

	orig := discord.DiscordAPIBase
	discord.DiscordAPIBase = srv.URL
	t.Cleanup(func() { discord.DiscordAPIBase = orig })
}

func fakeLLM(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ConfigureChat(llm.NewClient(srv.URL, "test-key", "test-model"), "app123")
	t.Cleanup(func() { ConfigureChat(nil, "") })
}

func TestHandleChat_Offline(t *testing.T) {
	ConfigureChat(nil, "")
	res, err := handleChat(CommandContext{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResponseText != chatOfflineMessage {
		t.Errorf("want offline message, got %q", res.ResponseText)
	}
}

func TestHandleChat_Deferred(t *testing.T) {
	var followups []string
	fakeDiscord(t, &followups)
	fakeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"woof"}}]}`))
	})

	res, err := handleChat(CommandContext{
		InteractionData: discord.ApplicationCommandData{
			Name: "chat",
			Options: []discord.InteractionDataOption{
				{Name: "message", Type: discord.OptionTypeString, Value: json.RawMessage(`"hi"`)},
			},
		},
		InteractionToken: "tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResponseType != discord.InteractionResponseTypeDeferredChannelMessageWithSource {
		t.Errorf("want deferred response type, got %d", res.ResponseType)
	}

	if !WaitBackground(5 * time.Second) {
		t.Fatal("background task did not finish")
	}
	if len(followups) != 1 || followups[0] != "woof" {
		t.Errorf("want followup [woof], got %v", followups)
	}
}

func TestProcessChatMessage_LLMError(t *testing.T) {
	var followups []string
	fakeDiscord(t, &followups)
	fakeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	processChatMessage("hi", "tok")

	if len(followups) != 1 || !strings.Contains(followups[0], "error bro, katanya") {
		t.Errorf("want error followup, got %v", followups)
	}
}

func TestProcessChatMessage_EmptyReply(t *testing.T) {
	var followups []string
	fakeDiscord(t, &followups)
	fakeLLM(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})

	processChatMessage("hi", "tok")

	if len(followups) != 1 || followups[0] != "kurang tau bro" {
		t.Errorf("want [kurang tau bro], got %v", followups)
	}
}
