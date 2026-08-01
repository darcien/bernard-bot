package chat

import (
	"strings"
	"testing"

	"bernard/llm"
)

func textUnit(size int) []llm.Message {
	return []llm.Message{{Role: "user", Content: strings.Repeat("a", size)}}
}

// The floor is held by one turn at a time; a mention arriving mid-turn is
// recorded rather than starting a second one.
func TestSession_ClaimGrantsTheFloorToOneTurn(t *testing.T) {
	sess := &session{}
	if !sess.claim(mentionMsg()) {
		t.Fatal("want the first mention to take the floor")
	}
	if sess.claim(mentionMsg()) {
		t.Error("want the second mention queued, not running")
	}
}

// finish hands the waiting mention back to the running turn, which keeps the
// floor and runs again — that is how N mentions cost one follow-up turn.
func TestSession_FinishHandsBackTheWaitingMention(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())

	waiting := mentionMsg()
	waiting.ID = "10"
	sess.claim(waiting)

	got, ok := sess.finish()
	if !ok {
		t.Fatal("want the waiting mention handed back")
	}
	if got.ID != "10" {
		t.Errorf("got mention %q, want the one that waited", got.ID)
	}
	if !sess.running {
		t.Error("want the floor still held while the follow-up runs")
	}
}

// Newest wins: an overwritten mention is not lost, it is still a channel
// message and rides the follow-up turn's delta.
func TestSession_NewestWaitingMentionWins(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())
	for _, id := range []string{"10", "11", "12"} {
		msg := mentionMsg()
		msg.ID = id
		sess.claim(msg)
	}

	got, _ := sess.finish()
	if got.ID != "12" {
		t.Errorf("got mention %q, want the newest", got.ID)
	}
}

// With nothing waiting, finish releases the floor so the next mention runs
// straight away instead of being queued behind a turn that already ended.
func TestSession_FinishReleasesTheFloorWhenNothingWaits(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())

	if _, ok := sess.finish(); ok {
		t.Fatal("want no mention handed back")
	}
	if !sess.claim(mentionMsg()) {
		t.Error("want the next mention to take the released floor")
	}
}

func TestTrimUnits(t *testing.T) {
	t.Run("under budget stays untouched", func(t *testing.T) {
		units := [][]llm.Message{textUnit(10), textUnit(10)}
		if got := trimUnits(units, 100, 50); len(got) != 2 {
			t.Errorf("want 2 units, got %d", len(got))
		}
	})

	t.Run("over budget drops oldest until under floor", func(t *testing.T) {
		units := [][]llm.Message{textUnit(40), textUnit(40), textUnit(40)}
		got := trimUnits(units, 100, 50) // total 120+roles > 100
		if len(got) != 1 {
			t.Fatalf("want 1 unit left, got %d", len(got))
		}
		if got[0][0].Content != units[2][0].Content {
			t.Error("want newest unit kept, oldest dropped")
		}
	})

	t.Run("newest unit survives even alone over the floor", func(t *testing.T) {
		units := [][]llm.Message{textUnit(10), textUnit(500)}
		got := trimUnits(units, 100, 50)
		if len(got) != 1 || len(got[0][0].Content) != 500 {
			t.Errorf("want only the oversized newest unit kept, got %d units", len(got))
		}
	})
}

func TestUnitSize_CountsToolCallArguments(t *testing.T) {
	unit := []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://example.com"}`},
		}},
	}}
	if got := unitSize(unit); got < len(`{"url":"https://example.com"}`) {
		t.Errorf("want arguments counted in size, got %d", got)
	}
}
