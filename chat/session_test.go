package chat

import (
	"strconv"
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

// The follow-up turn's question is the newest waiting mention; the rest are
// still channel messages and ride that turn's delta.
func TestSession_FollowUpTakesTheNewestWaitingMention(t *testing.T) {
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

// steer takes the waiting mention without giving up the floor: the turn is
// absorbing the message, not ending.
func TestSession_SteerTakesTheWaitingMentionAndKeepsTheFloor(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())

	waiting := mentionMsg()
	waiting.ID = "10"
	sess.claim(waiting)

	got, ok := sess.steer()
	if !ok || got.ID != "10" {
		t.Fatalf("want the waiting mention, got %v %v", got.ID, ok)
	}
	if !sess.running {
		t.Error("want the floor still held — steering is not finishing")
	}
	if _, ok := sess.steer(); ok {
		t.Error("want nothing left to steer")
	}
	if _, ok := sess.finish(); ok {
		t.Error("want no follow-up turn for a mention already folded in")
	}
}

// A steered mention whose turn then failed is owed an answer by nobody, so
// requeue puts it back for the follow-up turn to pick up.
func TestSession_RequeuedMentionIsHandedBackByFinish(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())

	waiting := mentionMsg()
	waiting.ID = "10"
	sess.claim(waiting)
	steered, _ := sess.steer()

	sess.requeue(steered)

	got, ok := sess.finish()
	if !ok || got.ID != "10" {
		t.Errorf("want the requeued mention handed back, got %v %v", got.ID, ok)
	}
}

// Every waiting mention is kept, not just the newest. Keeping only the newest
// loses the others: the commit folds steered IDs into the watermark, so a
// displaced mention ends up behind the watermark having never reached the
// model.
func TestSession_KeepsEveryWaitingMention(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())
	for _, id := range []string{"10", "11"} {
		msg := mentionMsg()
		msg.ID = id
		sess.claim(msg)
	}

	var got []string
	for {
		msg, ok := sess.steer()
		if !ok {
			break
		}
		got = append(got, msg.ID)
	}
	if len(got) != 2 || got[0] != "10" || got[1] != "11" {
		t.Errorf("want both mentions oldest first, got %v", got)
	}
}

// Past pendingCap the oldest is dropped rather than growing without bound.
func TestSession_QueueDropsOldestPastCap(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())
	for i := range pendingCap + 2 {
		msg := mentionMsg()
		msg.ID = strconv.Itoa(100 + i)
		sess.claim(msg)
	}

	if len(sess.pending) != pendingCap {
		t.Fatalf("want %d waiting, got %d", pendingCap, len(sess.pending))
	}
	if sess.pending[0].ID != "102" {
		t.Errorf("want the two oldest dropped, got %q first", sess.pending[0].ID)
	}
}

// Between steering and failing there is a window where nothing is waiting and
// a new mention can land. Requeueing the steered one must not displace the
// newer arrival — that one has never been seen by any turn, while the steered
// one is still behind the watermark and comes back via the gap sync.
func TestSession_RequeueDoesNotDisplaceANewerArrival(t *testing.T) {
	sess := &session{}
	sess.claim(mentionMsg())

	waiting := mentionMsg()
	waiting.ID = "10"
	sess.claim(waiting)
	steered, _ := sess.steer()

	arrived := mentionMsg()
	arrived.ID = "11"
	sess.claim(arrived) // lands while the turn is still running

	sess.requeue(steered)

	got, ok := sess.finish()
	if !ok || got.ID != "11" {
		t.Errorf("want the newer arrival kept, got %v %v", got.ID, ok)
	}
}

// The ratio is the last observation, used raw — no smoothing, no history.
func TestSession_TokPerByteUsesTheLastObservation(t *testing.T) {
	sess := &session{}
	if got := sess.tokPerByte(); got != fallbackTokPerByte {
		t.Fatalf("want the fallback before any call, got %v", got)
	}

	sess.observe(1000, 320)
	if got := sess.tokPerByte(); got != 0.32 {
		t.Errorf("want the sample used whole, got %v", got)
	}

	sess.observe(1000, 420)
	if got := sess.tokPerByte(); got != 0.42 {
		t.Errorf("want the newest sample, not an average, got %v", got)
	}
}

// A failed turn returns an empty loopResult, so it observes zeros. Those must
// not evict a good sample — a failure arrives when the session is largest,
// which is exactly when the ratio is about to matter.
func TestSession_ObserveKeepsTheLastGoodSample(t *testing.T) {
	sess := &session{}
	sess.observe(1000, 320)
	sess.observe(0, 0)
	if got := sess.tokPerByte(); got != 0.32 {
		t.Errorf("want the last good sample kept, got %v", got)
	}
}

// An implausible ratio means the measurement is broken, so the fallback
// stands rather than a nonsense number propagating into tail sizing.
func TestSession_TokPerByteRejectsNonsense(t *testing.T) {
	cases := []struct {
		name          string
		bytes, tokens int
	}{
		{"no bytes", 0, 100},
		{"no tokens", 1000, 0},
		{"denser than two tokens per byte", 100, 900},
		{"impossibly sparse", 100000, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := &session{}
			sess.observe(tc.bytes, tc.tokens)
			if got := sess.tokPerByte(); got != fallbackTokPerByte {
				t.Errorf("want the fallback, got %v", got)
			}
		})
	}
}

// Tiers name the escalation Reasonix would run; below the first one there is
// nothing to report.
func TestContextTier(t *testing.T) {
	cases := []struct {
		tokens int
		want   string
	}{
		{0, ""},
		{contextWindow*softRatio - 1, ""},
		{contextWindow * softRatio, "soft"},
		{contextWindow * snipRatio, "snip"},
		{contextWindow * compactRatio, "compact"},
		{contextWindow * forceRatio, "force"},
		{contextWindow, "force"},
	}
	for _, tc := range cases {
		if got := contextTier(tc.tokens); got != tc.want {
			t.Errorf("%d tokens: got %q, want %q", tc.tokens, got, tc.want)
		}
	}
}

func TestPlanRegion(t *testing.T) {
	// One unit per turn, so the tail boundary is always a turn boundary and a
	// tool result can never be split from its call.
	t.Run("everything fits the budget", func(t *testing.T) {
		units := [][]llm.Message{textUnit(100), textUnit(100)}
		region, tail := planRegion(units, 0.32)
		if len(region) != 0 || len(tail) != 2 {
			t.Errorf("want nothing to compact, got region %d tail %d", len(region), len(tail))
		}
	})

	t.Run("older units fall outside the tail", func(t *testing.T) {
		big := tailBudget * 4 // bytes; at 0.5 tok/byte, two of these overflow
		units := [][]llm.Message{textUnit(big), textUnit(big), textUnit(big), textUnit(big)}
		region, tail := planRegion(units, 0.5)
		if len(region) == 0 {
			t.Fatal("want a region to compact")
		}
		if len(region)+len(tail) != len(units) {
			t.Errorf("want the split to account for every unit, got %d + %d of %d", len(region), len(tail), len(units))
		}
		if tail[len(tail)-1][0].Content != units[len(units)-1][0].Content {
			t.Error("want the newest unit in the tail")
		}
	})

	t.Run("recentKeep units survive any budget", func(t *testing.T) {
		huge := tailBudget * 100
		units := [][]llm.Message{textUnit(huge), textUnit(huge), textUnit(huge)}
		region, tail := planRegion(units, 1.0)
		if len(tail) != recentKeep || len(region) != len(units)-recentKeep {
			t.Errorf("want %d units kept, got tail %d region %d", recentKeep, len(tail), len(region))
		}
	})
}

// Maintenance fires on the measured prompt, not on a character count: a session
// far under the window keeps everything however many bytes it holds.
func TestSession_MaintainOnlyOverTheTrigger(t *testing.T) {
	sess := &session{}
	sess.observe(60000, 20000) // 2% of the window, the old char budget's scale
	for range 5 {
		sess.append([]llm.Message{{Role: "user", Content: strings.Repeat("x", 20000)}})
		sess.maintain(failFold(t))
	}
	if len(sess.units) != 5 || sess.foldedUnits != 0 {
		t.Errorf("want all 5 units kept, got %d (trimmed %d)", len(sess.units), sess.foldedUnits)
	}

	// Over the trigger, maintenance folds down to the tail budget.
	sess.observe(60000, int(contextWindow*forceRatio))
	sess.append([]llm.Message{{Role: "user", Content: "over"}})
	sess.maintain(stubFold)
	if sess.foldedUnits == 0 {
		t.Fatal("want a fold once the measured prompt crosses the trigger")
	}
	if got := int(float64(sess.size()) * sess.tokPerByte()); got > tailBudget {
		t.Errorf("want the session at or under the tail budget %d tokens, got %d", tailBudget, got)
	}
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
