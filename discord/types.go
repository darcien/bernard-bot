package discord

import "encoding/json"

type InteractionType int

const (
	InteractionTypePing               InteractionType = 1
	InteractionTypeApplicationCommand InteractionType = 2
)

type InteractionResponseType int

const (
	InteractionResponseTypePong                             InteractionResponseType = 1
	InteractionResponseTypeChannelMessageWithSource         InteractionResponseType = 4
	InteractionResponseTypeDeferredChannelMessageWithSource InteractionResponseType = 5
)

type OptionType int

const (
	OptionTypeSubCommand OptionType = 1
	OptionTypeString     OptionType = 3
	OptionTypeInteger    OptionType = 4
	OptionTypeBoolean    OptionType = 5
	OptionTypeUser       OptionType = 6
)

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot,omitempty"`
}

type Message struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Author    User   `json:"author"`
	Timestamp string `json:"timestamp"` // ISO8601, e.g. "2021-01-01T00:00:00.000000+00:00"
	Mentions  []User `json:"mentions"`
	WebhookID string `json:"webhook_id,omitempty"`
}

type Member struct {
	User *User  `json:"user,omitempty"`
	Nick string `json:"nick,omitempty"`
}

type Channel struct {
	ID string `json:"id"`
}

// InteractionDataOption value is dynamically typed: string, int, bool, or snowflake (user).
// Use the typed accessor methods after checking Type.
type InteractionDataOption struct {
	Name    string                  `json:"name"`
	Type    OptionType              `json:"type"`
	Value   json.RawMessage         `json:"value,omitempty"`
	Options []InteractionDataOption `json:"options,omitempty"`
}

func (o InteractionDataOption) StringValue() string {
	var s string
	_ = json.Unmarshal(o.Value, &s)
	return s
}

func (o InteractionDataOption) IntValue() int {
	var n int
	_ = json.Unmarshal(o.Value, &n)
	return n
}

func (o InteractionDataOption) BoolValue() bool {
	var b bool
	_ = json.Unmarshal(o.Value, &b)
	return b
}

type ApplicationCommandData struct {
	Name    string                  `json:"name"`
	Options []InteractionDataOption `json:"options,omitempty"`
}

type Interaction struct {
	Type    InteractionType         `json:"type"`
	Data    *ApplicationCommandData `json:"data,omitempty"`
	Member  *Member                 `json:"member,omitempty"`
	Channel *Channel                `json:"channel,omitempty"`
	GuildID string                  `json:"guild_id,omitempty"`
	ID      string                  `json:"id"`
	Token   string                  `json:"token"`
}

// Response types sent back to Discord.

type PongResponse struct {
	Type InteractionResponseType `json:"type"`
}

type MessageResponse struct {
	Type InteractionResponseType `json:"type"`
	Data MessageResponseData     `json:"data"`
}

type MessageResponseData struct {
	Content     string       `json:"content,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type PingResponse struct {
	Message string `json:"message"`
}
