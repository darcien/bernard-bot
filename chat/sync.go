package chat

import (
	"cmp"
	"log/slog"
	"slices"
	"strconv"

	"bernard/discord"
	"bernard/llm"
	"bernard/tools"
)

// Channel sync: what the session has not seen yet, and the watermark that
// decides it. Snowflake ordering lives here because Discord's fetch order
// differs by endpoint — see channelDelta.

// syncChannel fetches channel messages the session doesn't represent yet:
// recent history on the first sync (restart context), messages sent between
// turns afterwards. excludeID drops the triggering message. Best effort — a
// failed fetch just means answering without the gap.
func (s *Service) syncChannel(sess *session, channelID, excludeID string) ([]llm.Message, string) {
	var msgs []discord.Message
	var err error
	if sess.lastSeenID == "" {
		msgs, err = discord.GetMessagesFromChannel(channelID, initialSyncFetch)
	} else {
		msgs, err = discord.GetMessagesAfter(channelID, sess.lastSeenID, gapSyncFetch)
	}
	if err != nil {
		slog.Warn("chat channel sync failed", "channel", channelID, "err", err)
		return nil, sess.lastSeenID
	}
	delta := channelDelta(msgs, s.botID, !sess.synced, excludeID)
	seenID := newestMessageID(msgs, sess.lastSeenID)
	// fetched=0 → REST/watermark problem; fetched>0 delta=0 → filtering
	// problem; delta>0 → the messages made it into the prompt.
	slog.Debug("chat sync",
		"channel", channelID,
		"initial", !sess.synced,
		"after", sess.lastSeenID,
		"fetched", len(msgs),
		"delta", len(delta),
		"watermark", seenID)
	return delta, seenID
}

// channelDelta maps fetched channel messages to history turns, oldest
// first. Fetch order is normalized here — Discord returns newest-first for
// plain fetches but oldest-first for ?after= fetches, so no caller-side
// order assumption survives contact with the API. This is the single
// channel → history conversion, used for both the initial sync after a
// restart and the per-turn gap sync of messages sent between chats.
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
