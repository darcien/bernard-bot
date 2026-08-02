package chat

import (
	"strings"
	"testing"

	"bernard/discord"
)

func TestChannelDelta(t *testing.T) {
	// Newest first, as the Discord API returns them.
	msgs := []discord.Message{
		{ID: "5", Content: "latest", Author: discord.User{ID: "u1", Username: "alice"}},
		{ID: "4", Content: "", Author: discord.User{ID: "u2", Username: "carol"}},
		{ID: "3", Content: "beep", Author: discord.User{ID: "other-bot", Username: "clanker", Bot: true}},
		{ID: "2", Content: "sheep update", Author: discord.User{ID: "app123", Username: "bernard", Bot: true}},
		{ID: "1", Content: "oldest", Author: discord.User{ID: "u1", Username: "alice"}},
	}

	t.Run("initial sync rebuilds both sides, oldest first", func(t *testing.T) {
		got := channelDelta(msgs, "app123", true, "")
		if len(got) != 3 {
			t.Fatalf("want 3 turns (bots and empties skipped), got %+v", got)
		}
		if got[0].Content != "alice: oldest" || got[0].Role != "user" {
			t.Errorf("want oldest human turn first, got %+v", got[0])
		}
		if got[1].Role != "assistant" || got[1].Content != "sheep update" {
			t.Errorf("want own message as assistant turn on initial sync, got %+v", got[1])
		}
		if got[2].Content != "alice: latest" {
			t.Errorf("want newest turn last, got %+v", got[2])
		}
	})

	t.Run("gap sync skips own messages, session already has them", func(t *testing.T) {
		got := channelDelta(msgs, "app123", false, "")
		for _, m := range got {
			if m.Role == "assistant" {
				t.Errorf("want no assistant turns from gap sync, got %+v", m)
			}
		}
		if len(got) != 2 {
			t.Errorf("want 2 human turns, got %+v", got)
		}
	})

	// Discord returns newest-first for plain fetches but oldest-first for
	// ?after= fetches; mapping must not care.
	t.Run("oldest-first input produces identical turns", func(t *testing.T) {
		reversed := make([]discord.Message, len(msgs))
		for i, m := range msgs {
			reversed[len(msgs)-1-i] = m
		}
		a := channelDelta(msgs, "app123", true, "")
		b := channelDelta(reversed, "app123", true, "")
		if len(a) != len(b) {
			t.Fatalf("want same turn count, got %d vs %d", len(a), len(b))
		}
		for i := range a {
			if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
				t.Errorf("turn %d differs: %+v vs %+v", i, a[i], b[i])
			}
		}
	})

	t.Run("excludeID drops the triggering mention message", func(t *testing.T) {
		got := channelDelta(msgs, "app123", false, "5")
		for _, m := range got {
			if strings.Contains(m.Content, "latest") {
				t.Errorf("want message 5 excluded, got %+v", m)
			}
		}
	})
}

func TestMaxSnowflake(t *testing.T) {
	// Numeric compare: "100" > "99" even though it sorts lower as a string.
	if got := maxSnowflake("99", "100"); got != "100" {
		t.Errorf("want numeric compare, got %q", got)
	}
	if got := maxSnowflake("100", ""); got != "100" {
		t.Errorf("want non-empty side, got %q", got)
	}
	if got := maxSnowflake("", "9"); got != "9" {
		t.Errorf("want non-empty side, got %q", got)
	}
}

func TestNewestMessageID(t *testing.T) {
	msgs := []discord.Message{{ID: "9"}, {ID: "8"}}
	if got := newestMessageID(msgs, "old"); got != "9" {
		t.Errorf("want newest ID 9, got %q", got)
	}
	if got := newestMessageID(nil, "old"); got != "old" {
		t.Errorf("want fallback when nothing fetched, got %q", got)
	}
}
