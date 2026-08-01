package chat

import (
	"cmp"
	"slices"
	"strconv"

	"bernard/discord"
	"bernard/llm"
	"bernard/tools"
)

// Pure prompt-assembly functions, kept free of IO so they test directly.

// buildContext assembles the wire message list:
//
//	[system message]  = system prompt + guild memory block (empty in v1)
//	[channel history] append-only session
//	[current message]
//
// Memory is concatenated onto the system prompt string, not a separate
// message — the prefix cache keys off literal bytes, and one system message
// is simpler. Empty memory leaves the system message byte-identical.
func buildContext(systemPrompt, memory string, history []llm.Message, current llm.Message) []llm.Message {
	system := systemPrompt
	if memory != "" {
		system += "\n\n" + memory
	}
	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, llm.Message{Role: "system", Content: system})
	msgs = append(msgs, history...)
	return append(msgs, current)
}

// userMessage prefixes content with the speaker's name — the OpenAI "name"
// field is not reliably honored by DeepSeek, so the prefix lives in content.
// Same shape as the bootstrap transcript lines.
func userMessage(username, content string) llm.Message {
	return llm.Message{Role: "user", Content: username + ": " + content}
}

// channelDelta maps fetched channel messages to history turns, oldest
// first. Fetch order is normalized here — Discord returns newest-first for
// plain fetches but oldest-first for ?after= fetches, so no caller-side
// order assumption survives contact with the API. This is the single
// channel → history conversion, used for both the initial sync after a
// restart and the per-invocation gap sync of messages sent between chats.
//
//   - humans → user turns, "name: content" prefixed like the live path
//   - the bot's own messages (matched by user ID == application ID; not
//     Author.Bot, which would match any bot) → assistant turns, but only on
//     the initial sync — a live session already holds them as real assistant
//     turns, so later syncs skipping them is "already represented", not loss
//   - other bots and empty contents (attachment-only, stubs) → skipped
//   - excludeID (the triggering message on the mention entry) → skipped,
//     the caller adds it as the current turn
func channelDelta(msgs []discord.Message, botUserID string, initial bool, excludeID string) []llm.Message {
	sorted := slices.Clone(msgs)
	slices.SortFunc(sorted, func(a, b discord.Message) int {
		return cmp.Compare(snowflake(a.ID), snowflake(b.ID))
	})
	var turns []llm.Message
	for _, m := range sorted { // oldest first
		if m.Content == "" || (excludeID != "" && m.ID == excludeID) {
			continue
		}
		content := tools.TruncateRunes(m.Content, channelLineCap)
		switch {
		case m.Author.ID == botUserID:
			if initial {
				turns = append(turns, llm.Message{Role: "assistant", Content: content})
			}
		case m.Author.Bot:
			// skip
		default:
			turns = append(turns, userMessage(m.Author.Username, content))
		}
	}
	return turns
}

// newestMessageID returns the ID of the newest fetched message (scanned,
// never position-based — see channelDelta on fetch ordering), or fallback
// when nothing was fetched (the watermark must not move backwards).
func newestMessageID(msgs []discord.Message, fallback string) string {
	newest := fallback
	for _, m := range msgs {
		newest = maxSnowflake(newest, m.ID)
	}
	return newest
}

// maxSnowflake picks the numerically larger Discord snowflake ID — string
// comparison breaks across IDs of different lengths.
func maxSnowflake(a, b string) string {
	if snowflake(b) > snowflake(a) {
		return b
	}
	return a
}

// snowflake parses a Discord ID for numeric comparison; malformed or empty
// IDs come back 0 and lose every comparison.
func snowflake(id string) uint64 {
	n, _ := strconv.ParseUint(id, 10, 64)
	return n
}
