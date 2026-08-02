package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// https://discord.com/developers/docs/reference
const repoURL = "https://github.com/darcien/bernard-bot"

// DiscordAPIBase is a var so tests can point API calls at a fake server.
var DiscordAPIBase = "https://discord.com/api/v10"

var (
	apiClient = &http.Client{Timeout: 10 * time.Second}
	botToken  string
)

// SetBotToken stores the bot token for API calls. Must be called before any API use.
func SetBotToken(token string) { botToken = token }

func fetchAsBot(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// https://discord.com/developers/docs/reference#user-agent
	req.Header.Set("User-Agent", fmt.Sprintf("DiscordBot (%s)", repoURL))
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := apiClient.Do(req)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		slog.Error("discord API request failed", "url", url, "dur", dur, "err", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	logAPI(url, resp.StatusCode, dur)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord API %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// messagePayload builds a message body: JSON when the content fits the
// message limit, otherwise a markdown attachment. Shared by followups and
// channel posts.
func messagePayload(content string) (contentType string, payload []byte) {
	// Marshal of a fixed struct of strings cannot fail, and an error return
	// here would reach a caller with nothing to do about it.
	if utf16Len(content) > maxMessageLength {
		payloadJSON, _ := json.Marshal(MessageResponseData{
			Attachments: []Attachment{{ID: 0, Filename: "response.md"}},
		})
		return multipartAttachment(payloadJSON, content)
	}
	payload, _ = json.Marshal(MessageResponseData{Content: content})
	return "application/json", payload
}

// CreateFollowupMessage posts a followup message for a deferred interaction
// response. Auth is the interaction token in the URL; no bot token needed.
// https://discord.com/developers/docs/interactions/receiving-and-responding#followup-messages
func CreateFollowupMessage(applicationID, interactionToken, content string) error {
	url := fmt.Sprintf("%s/webhooks/%s/%s", DiscordAPIBase, applicationID, interactionToken)
	contentType, payload := messagePayload(content)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("DiscordBot (%s)", repoURL))
	req.Header.Set("Content-Type", contentType)

	// Logs say "followup" instead of the real URL on purpose: the webhook
	// URL embeds the interaction token (it IS the auth), and tokens don't
	// belong in logs.
	start := time.Now()
	resp, err := apiClient.Do(req)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		slog.Error("discord API request failed", "url", "followup", "dur", dur, "err", err)
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	logAPI("followup", resp.StatusCode, dur)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord API %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetMessagesAfter returns channel messages with IDs after afterID. Used to
// sync messages sent between chat invocations into the conversation history.
// Note the ordering trap: unlike a plain fetch, Discord returns ?after=
// results oldest-first, so callers must not assume a position means newest.
func GetMessagesAfter(channelID, afterID string, limit int) ([]Message, error) {
	url := fmt.Sprintf("%s/channels/%s/messages?after=%s&limit=%d", DiscordAPIBase, channelID, afterID, limit)
	body, err := fetchAsBot(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages after %s from channel %s: %w", afterID, channelID, err)
	}
	var messages []Message
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// CreateMessage posts a plain message to a channel as the bot (the mention
// reply path — no interaction token involved). Long content becomes a
// markdown attachment via the shared payload builder.
// https://discord.com/developers/docs/resources/message#create-message
func CreateMessage(channelID, content string) error {
	url := fmt.Sprintf("%s/channels/%s/messages", DiscordAPIBase, channelID)
	contentType, payload := messagePayload(content)
	return postAsBot(url, contentType, payload)
}

// TriggerTyping shows "Bernard is typing..." in the channel for ~10s.
// Best effort — callers ignore failures.
func TriggerTyping(channelID string) error {
	url := fmt.Sprintf("%s/channels/%s/typing", DiscordAPIBase, channelID)
	return postAsBot(url, "application/json", nil)
}

func postAsBot(url, contentType string, payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("DiscordBot (%s)", repoURL))
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", contentType)

	start := time.Now()
	resp, err := apiClient.Do(req)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		slog.Error("discord API request failed", "url", url, "dur", dur, "err", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// A successful typing tick is the one call that says nothing: it re-fires
	// every typingInterval for the life of a turn, so a long turn writes a
	// column of identical 204s. Failure still logs, here and at the caller.
	if !isTyping(url) || !ok(resp.StatusCode) {
		logAPI(url, resp.StatusCode, dur)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord API %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func isTyping(url string) bool { return strings.HasSuffix(url, "/typing") }

func ok(status int) bool { return status >= 200 && status < 300 }

// logAPI records one Discord call. A non-2xx answer is Warn, not Debug: a
// transport failure already logs at Error, so leaving a 429 or a 5xx at Debug
// made an outage quieter than a flaky socket.
func logAPI(url string, status int, dur time.Duration) {
	level := slog.LevelDebug
	if !ok(status) {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "discord API", "url", url, "status", status, "dur", dur)
}

func GetMessagesFromChannel(channelID string, limit int) ([]Message, error) {
	url := fmt.Sprintf("%s/channels/%s/messages?limit=%d", DiscordAPIBase, channelID, limit)
	body, err := fetchAsBot(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages from channel %s: %w", channelID, err)
	}
	var messages []Message
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func GetGuildMembers(guildID string, limit int) ([]Member, error) {
	url := fmt.Sprintf("%s/guilds/%s/members?limit=%d", DiscordAPIBase, guildID, limit)
	body, err := fetchAsBot(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch guild members for %s: %w", guildID, err)
	}
	var members []Member
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, err
	}
	return members, nil
}
