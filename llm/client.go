package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to any OpenAI-compatible chat completions endpoint.
type Client struct {
	BaseURL string // e.g. https://api.deepseek.com, no trailing /chat/completions
	APIKey  string
	Model   string

	httpClient *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	N        int           `json:"n"`
	// DeepSeek enables thinking mode by default; a chat bot wants fast,
	// cheap, non-thinking replies. Other OpenAI-compatible providers
	// ignore the unknown field.
	Thinking thinkingConfig `json:"thinking"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Complete sends a single-turn completion request and returns the reply text.
// An empty string with nil error means the API returned no choices.
func (c *Client) Complete(systemPrompt, userMessage string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		N:        1,
		Thinking: thinkingConfig{Type: "disabled"},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm API %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", nil
	}
	return parsed.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
