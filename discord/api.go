package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// https://discord.com/developers/docs/reference
const (
	DiscordAPIBase = "https://discord.com/api/v10"
	repoURL        = "https://github.com/darcien/bernard-bot"
)

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
