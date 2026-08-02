package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bernard/logid"
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
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		// Client-level timeout (not just request context): it also bounds
		// reading the response body, which a bad gateway can stall after
		// sending headers.
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Message is one chat message on the wire.
// Content is a plain string with no omitempty on purpose: DeepSeek's strict
// deserializer rejects a message missing the content field, so an assistant
// message carrying only tool calls must still send "content": "".
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string as sent by the model
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []Message       `json:"messages"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	// Explicit output budget — provider defaults vary wildly (some
	// gateways default as low as 256 and silently cut replies mid-word).
	MaxTokens int `json:"max_tokens"`
	N         int `json:"n"`
	// DeepSeek enables thinking mode by default; a chat bot wants fast,
	// cheap, non-thinking replies. Other OpenAI-compatible providers
	// ignore the unknown field.
	Thinking thinkingConfig `json:"thinking"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

const maxOutputTokens = 2048 // ~8k chars; Discord overflow becomes an attachment

// Usage reports what one call cost and how it ended, for the caller to log
// or aggregate. FinishReason "length" means the reply was cut mid-word by
// the output budget.
type Usage struct {
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	// CacheHitTokens is how much of the prompt hit the provider's
	// server-side prefix cache (DeepSeek-specific; zero elsewhere).
	CacheHitTokens int
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens         int `json:"prompt_tokens"`
		CompletionTokens     int `json:"completion_tokens"`
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	} `json:"usage"`
}

// Chat sends a conversation (with optional tool schemas in the OpenAI
// "tools" format) and returns the assistant message, which may carry
// ToolCalls instead of or alongside Content, plus usage for the caller to
// log. A zero Message with nil error means the API returned no choices.
func (c *Client) Chat(ctx context.Context, msgs []Message, tools json.RawMessage) (Message, Usage, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:     c.Model,
		Messages:  sanitize(msgs),
		Tools:     tools,
		MaxTokens: maxOutputTokens,
		N:         1,
		Thinking:  thinkingConfig{Type: "disabled"},
	})
	if err != nil {
		return Message{}, Usage{}, err
	}

	body, err := c.send(ctx, c.BaseURL+"/chat/completions", reqBody)
	if err != nil {
		return Message{}, Usage{}, err
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Message{}, Usage{}, err
	}
	if len(parsed.Choices) == 0 {
		return Message{}, Usage{}, nil
	}
	choice := parsed.Choices[0]
	usage := Usage{
		FinishReason:     choice.FinishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheHitTokens:   parsed.Usage.PromptCacheHitTokens,
	}
	// Per-call detail; callers aggregate usage into their own summary.
	attrs := []any{
		"finish", usage.FinishReason,
		"prompt_tokens", usage.PromptTokens,
		"cache_hit_tokens", usage.CacheHitTokens,
		"completion_tokens", usage.CompletionTokens,
	}
	if n := len(choice.Message.ToolCalls); n > 0 {
		attrs = append(attrs, "tool_calls", n)
	}
	slog.Debug("llm call", append(attrs, logid.Attrs(ctx)...)...)
	return choice.Message, usage, nil
}

// Complete sends a single-turn completion request and returns the reply text.
// An empty string with nil error means the API returned no choices.
func (c *Client) Complete(systemPrompt, userMessage string) (string, error) {
	m, _, err := c.Chat(context.Background(), []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}, nil)
	return m.Content, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
