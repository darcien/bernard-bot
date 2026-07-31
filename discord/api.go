package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	dur := time.Since(start)
	if err != nil {
		slog.Error("discord API request failed", "url", url, "dur", dur, "err", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	slog.Info("discord API", "url", url, "status", resp.StatusCode, "dur", dur)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord API %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// CreateFollowupMessage posts a followup message for a deferred interaction
// response. Auth is the interaction token in the URL; no bot token needed.
// Content over the message length limit is sent as a markdown file attachment,
// same as RespondFromResult.
// https://discord.com/developers/docs/interactions/receiving-and-responding#followup-messages
func CreateFollowupMessage(applicationID, interactionToken, content string) error {
	url := fmt.Sprintf("%s/webhooks/%s/%s", DiscordAPIBase, applicationID, interactionToken)

	var contentType string
	var body []byte
	if utf16Len(content) > maxMessageLength {
		payloadJSON, _ := json.Marshal(MessageResponseData{
			Attachments: []Attachment{{ID: 0, Filename: "response.md"}},
		})
		contentType, body = multipartAttachment(payloadJSON, content)
	} else {
		body, _ = json.Marshal(MessageResponseData{Content: content})
		contentType = "application/json"
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("DiscordBot (%s)", repoURL))
	req.Header.Set("Content-Type", contentType)

	start := time.Now()
	resp, err := apiClient.Do(req)
	dur := time.Since(start)
	if err != nil {
		slog.Error("discord API request failed", "url", "followup", "dur", dur, "err", err)
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	slog.Info("discord API", "url", "followup", "status", resp.StatusCode, "dur", dur)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord API %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
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
