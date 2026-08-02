package chat

import (
	"strconv"
	"testing"
)

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
