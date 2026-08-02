package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bernard/llm"
)

// A fact someone stated survives the fold whatever the summariser does with
// it: the digest is a paraphrase, and a paraphrase of "call me X" is not
// "call me X".
func TestSummariseRegion_KeepsSmallUserTurnsVerbatim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"## Topics\n- things"}}]}`))
	}))
	defer srv.Close()

	region := [][]llm.Message{{
		{Role: "user", Content: "darcien: remember the deploy is friday"},
		{Role: "assistant", Content: "noted"},
	}}
	got := summariseRegion(context.Background(), llm.NewClient(srv.URL, "k", "m"), region, 0.32)

	if got[0].Content != "darcien: remember the deploy is friday" {
		t.Errorf("want the user turn kept verbatim and first, got %q", got[0].Content)
	}
	last := got[len(got)-1]
	if last.Role != "user" || !strings.HasPrefix(last.Content, summaryTagOpen) {
		t.Errorf("want a tagged digest as a user turn, got %+v", last)
	}
	if !strings.Contains(last.Content, "## Topics") {
		t.Errorf("want the model's digest inside, got %q", last.Content)
	}
}

// A failed summariser must still free the context: returning the region
// unchanged would leave the session over the trigger and fold again next
// turn, paying for the same failure repeatedly.
func TestSummariseRegion_FallsBackToAMechanicalDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	region := [][]llm.Message{{
		{Role: "assistant", Content: strings.Repeat("long assistant answer ", 200)},
	}}
	got := summariseRegion(context.Background(), llm.NewClient(srv.URL, "k", "m"), region, 0.32)

	if len(got) != 1 {
		t.Fatalf("want just the digest, got %d messages", len(got))
	}
	if !strings.Contains(got[0].Content, "summary was unavailable") {
		t.Errorf("want the mechanical digest, got %q", got[0].Content)
	}
}

// An earlier digest is kept verbatim rather than folded again: re-summarising
// a summary loses whatever it already captured.
func TestPartitionFold_KeepsAnEarlierDigest(t *testing.T) {
	digest := llm.Message{Role: "user", Content: summaryTagOpen + "\nolder\n" + summaryTagClose}
	region := [][]llm.Message{{digest, {Role: "assistant", Content: "work"}}}

	kept, fold := partitionFold(region, 0.32)
	if len(kept) != 1 || kept[0].Content != digest.Content {
		t.Errorf("want the earlier digest kept, got %v", kept)
	}
	if len(fold) != 1 || fold[0].Role != "assistant" {
		t.Errorf("want only the assistant work folded, got %v", fold)
	}
}

// A pasted wall of text is not a fact to pin; it folds like any other message
// so the kept-verbatim floor cannot starve the window.
func TestPartitionFold_FoldsHugeUserTurns(t *testing.T) {
	region := [][]llm.Message{{
		{Role: "user", Content: strings.Repeat("x", pinnedUserTokens*100)},
	}}
	kept, fold := partitionFold(region, 0.32)
	if len(kept) != 0 || len(fold) != 1 {
		t.Errorf("want the oversized turn folded, kept %d folded %d", len(kept), len(fold))
	}
}

// failFold fails the test if a fold is attempted: these tiers must not reach
// the summariser.
func failFold(t *testing.T) compactFunc {
	return func(region [][]llm.Message, _ float64) ([]llm.Message, bool) {
		t.Helper()
		t.Errorf("want no fold, got one over %d units", len(region))
		return nil, false
	}
}

func stubFold(region [][]llm.Message, _ float64) ([]llm.Message, bool) {
	return []llm.Message{{Role: "user", Content: summaryTagOpen + "\ndigest\n" + summaryTagClose}}, true
}

// Compaction fires on the measured prompt, not on a character count: a session
// far under the window keeps everything however many bytes it holds.
func TestMaybeCompact_OnlyOverTheTrigger(t *testing.T) {
	sess := &session{}
	sess.observe(60000, 20000) // 2% of the window, the old char budget's scale
	var rep compactionReport
	for range 5 {
		sess.append([]llm.Message{{Role: "user", Content: strings.Repeat("x", 20000)}})
		rep = sess.maybeCompact(nil, failFold(t))
	}
	if len(sess.units) != 5 || rep.compactedUnits != 0 {
		t.Errorf("want all 5 units kept, got %d (trimmed %d)", len(sess.units), rep.compactedUnits)
	}

	// Over the trigger, compaction folds down to the tail budget.
	sess.observe(60000, int(contextWindow*forceRatio))
	sess.append([]llm.Message{{Role: "user", Content: "over"}})
	rep = sess.maybeCompact(nil, stubFold)
	if rep.compactedUnits == 0 {
		t.Fatal("want a fold once the measured prompt crosses the trigger")
	}
	if got := int(float64(sess.size()) * sess.tokPerByte()); got > tailBudget {
		t.Errorf("want the session at or under the tail budget %d tokens, got %d", tailBudget, got)
	}
}

// The snip band shortens tool results and drops nothing; only the compaction
// tier removes units.
func TestMaybeCompact_SnipsInTheSnipBand(t *testing.T) {
	sess := &session{}
	sess.observe(100_000, int(contextWindow*snipRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(500)))
	}
	rep := sess.maybeCompact(nil, failFold(t))

	if len(sess.units) != 4 || rep.compactedUnits != 0 {
		t.Fatalf("want every unit kept, got %d (trimmed %d)", len(sess.units), rep.compactedUnits)
	}
	if len(rep.snipped) == 0 || savedBytes(rep.snipped) == 0 {
		t.Fatalf("want tool results snipped, got %d (%d bytes)", len(rep.snipped), savedBytes(rep.snipped))
	}
	// The tail is what the model still needs verbatim.
	if newest := sess.units[len(sess.units)-1][2].Content; strings.HasPrefix(newest, snippedMarker) {
		t.Error("want the newest unit left verbatim")
	}
}

// Pruning is free; dropping units is not. A turn whose prune clears the
// trigger keeps its whole conversation.
func TestMaybeCompact_PrunesBeforeFolding(t *testing.T) {
	sess := &session{}
	// Ratio chosen so the pruned bytes convert to more than the overshoot.
	sess.observe(100_000, int(contextWindow*compactRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(2000)))
	}
	rep := sess.maybeCompact(nil, failFold(t))

	if len(rep.pruned) == 0 {
		t.Fatal("want tool results pruned at the compaction tier")
	}
	if rep.compactedUnits != 0 {
		t.Errorf("want nothing dropped once the prune cleared the trigger, dropped %d", rep.compactedUnits)
	}
	if len(sess.units) != 4 {
		t.Errorf("want every unit kept, got %d", len(sess.units))
	}
}

// Past the force ratio the prune runs but no longer buys a reprieve.
func TestMaybeCompact_FoldsAtTheForceRatio(t *testing.T) {
	sess := &session{}
	sess.observe(100_000, int(contextWindow*forceRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(2000)))
	}
	rep := sess.maybeCompact(nil, stubFold)

	if rep.compactedUnits == 0 {
		t.Fatal("want the region folded at the force ratio")
	}
	if got := sess.units[0][0].Content; !strings.HasPrefix(got, summaryTagOpen) {
		t.Errorf("want the digest first in history, got %q", got)
	}
}

// The region plan is what explains every later decision, so it is recorded
// whether or not anything came of the run.
func TestMaybeCompact_RecordsTheRegionPlan(t *testing.T) {
	sess := &session{}
	sess.observe(100_000, int(contextWindow*snipRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(500)))
	}
	before := sess.size()
	rep := sess.maybeCompact(nil, failFold(t))

	if rep.region.units == 0 || rep.tail.units == 0 {
		t.Fatalf("want both sides of the split recorded, got region=%d tail=%d",
			rep.region.units, rep.tail.units)
	}
	if rep.region.units+rep.tail.units != 4 {
		t.Errorf("want the split to cover every unit, got %d+%d of 4",
			rep.region.units, rep.tail.units)
	}
	if rep.region.bytes == 0 || rep.tail.bytes == 0 {
		t.Errorf("want both sides sized, got region=%d tail=%d bytes",
			rep.region.bytes, rep.tail.bytes)
	}
	// tailBudget is in tokens, so a unit count alone cannot be checked
	// against it; the byte figure is what makes the tail measurable.
	//
	// Against the pre-compaction size, because the plan is recorded before
	// anything is rewritten — which is why the log calls the other number
	// session_bytes_after. The two are not the same measurement and the gap
	// between them is what this run saved.
	if got := rep.region.bytes + rep.tail.bytes; got != before {
		t.Errorf("want the split to account for the whole session %d, got %d", before, got)
	}
	if sess.size() >= before {
		t.Errorf("want the session smaller after snipping, got %d from %d", sess.size(), before)
	}
	if rep.tail.units < recentKeep {
		t.Errorf("want at least recentKeep units held verbatim, got %d", rep.tail.units)
	}
}

// A run that rewrites nothing used to be indistinguishable from a run that
// never happened. Both no-op paths now name themselves.
func TestMaybeCompact_NamesWhyItDidNothing(t *testing.T) {
	// Bulk in the user's own words, not in tool results: the region exists and
	// is large, but nothing in it is a result compaction may rewrite.
	t.Run("nothing eligible", func(t *testing.T) {
		sess := &session{}
		sess.observe(100_000, int(contextWindow*snipRatio))
		for range 4 {
			sess.append([]llm.Message{
				{Role: "user", Content: strings.Repeat("x", 20_000)},
				{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},
				{Role: "tool", ToolCallID: "c1", Content: "small"},
			})
		}
		rep := sess.maybeCompact(nil, failFold(t))

		if len(rep.snipped) != 0 {
			t.Fatalf("want nothing snipped, got %d", len(rep.snipped))
		}
		if rep.skipReason != "nothing_eligible" {
			t.Errorf("want nothing_eligible, got %q", rep.skipReason)
		}
	})

	// recentKeep alone holds everything, so there is no region to work on.
	t.Run("region empty", func(t *testing.T) {
		sess := &session{}
		sess.observe(100_000, int(contextWindow*snipRatio))
		for range recentKeep {
			sess.append(toolUnit(linesOf(500)))
		}
		rep := sess.maybeCompact(nil, failFold(t))

		if rep.region.units != 0 {
			t.Fatalf("want an empty region, got %d units", rep.region.units)
		}
		if rep.skipReason != "region_empty" {
			t.Errorf("want region_empty, got %q", rep.skipReason)
		}
	})

	// A run that acted leaves no reason behind — the aggregate line speaks.
	t.Run("no reason when it acted", func(t *testing.T) {
		sess := &session{}
		sess.observe(100_000, int(contextWindow*snipRatio))
		for range 4 {
			sess.append(toolUnit(linesOf(500)))
		}
		rep := sess.maybeCompact(nil, failFold(t))

		if len(rep.snipped) == 0 {
			t.Fatal("want results snipped")
		}
		if rep.skipReason != "" {
			t.Errorf("want no skip reason, got %q", rep.skipReason)
		}
	})
}

// Every path that returns without acting must name itself. An unnamed skip
// logs reason="" — the ambiguity the line was added to remove, reintroduced.
func TestMaybeCompact_EveryNoOpPathNamesItself(t *testing.T) {
	// Bulk in user prose, so the region is large but holds nothing prunable.
	bulk := func() []llm.Message {
		return []llm.Message{{Role: "user", Content: strings.Repeat("x", 25_000)}}
	}
	atCompactTier := func() *session {
		sess := &session{}
		sess.observe(1_000_000, int(contextWindow*compactRatio))
		for range 4 {
			sess.append(bulk())
		}
		return sess
	}

	t.Run("compact declined", func(t *testing.T) {
		sess := atCompactTier()
		rep := sess.maybeCompact(nil, func([][]llm.Message, float64) ([]llm.Message, bool) {
			return nil, false // no admission slot
		})
		if rep.skipReason != "compact_declined" {
			t.Errorf("want compact_declined, got %q", rep.skipReason)
		}
	})

	t.Run("compact stuck", func(t *testing.T) {
		sess := atCompactTier()
		sess.compactStuck = true
		rep := sess.maybeCompact(nil, failFold(t))
		if rep.skipReason != "compact_stuck" {
			t.Errorf("want compact_stuck, got %q", rep.skipReason)
		}
	})

	// A region too small to be worth a summariser call.
	t.Run("compact too small", func(t *testing.T) {
		sess := &session{}
		sess.observe(1_000_000, int(contextWindow*compactRatio))
		sess.append([]llm.Message{{Role: "user", Content: "tiny"}})
		for range 2 {
			sess.append(bulk())
		}
		rep := sess.maybeCompact(nil, failFold(t))
		if rep.skipReason != "compact_too_small" {
			t.Errorf("want compact_too_small, got %q", rep.skipReason)
		}
	})

	// A fold that ran leaves no reason behind.
	t.Run("cleared when the fold acts", func(t *testing.T) {
		sess := atCompactTier()
		rep := sess.maybeCompact(nil, stubFold)
		if rep.compactedUnits == 0 {
			t.Fatal("want a fold")
		}
		if rep.skipReason != "" {
			t.Errorf("want no reason after acting, got %q", rep.skipReason)
		}
	})
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

// The tail budget is capped against the window, not just fixed: a small
// window with a fixed 16384-token tail could keep more than the fold is
// trying to free, and never clear its own trigger.
func TestPlanRegion_TailBudgetIsCappedByTheWindow(t *testing.T) {
	if got, want := min(tailBudget, int(contextWindow*compactTarget)), tailBudget; got != want {
		t.Fatalf("at this window the fixed budget should win, got %d", got)
	}
	// A tail worth more than the budget must still leave a region to fold.
	big := tailBudget * 8 // bytes; at 1 tok/byte each unit alone exceeds it
	units := [][]llm.Message{textUnit(big), textUnit(big), textUnit(big), textUnit(big)}
	region, tail := planRegion(units, 1.0)
	if len(tail) != recentKeep {
		t.Errorf("want the tail held at recentKeep, got %d", len(tail))
	}
	if len(region) != len(units)-recentKeep {
		t.Errorf("want everything older folded, got %d", len(region))
	}
}

// A stuck session still prunes, so the run reaches the Info line with work to
// report. The latch has to ride the report or that line says a prune happened
// and stays silent about compaction being blocked.
func TestMaybeCompact_StuckRunStillReportsTheLatch(t *testing.T) {
	sess := &session{compactStuck: true}
	sess.observe(100_000, int(contextWindow*compactRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(2000)))
	}
	rep := sess.maybeCompact(nil, failFold(t))

	if len(rep.pruned) == 0 {
		t.Fatal("want the prune to run ahead of the latch")
	}
	if !rep.did() {
		t.Fatal("want the run to reach the aggregate line")
	}
	if !rep.stuck {
		t.Error("want the latch reported on a run that did work")
	}
	// The prune cleared the trigger, so this run acted; skipReason names why
	// nothing happened and has no business being set here.
	if rep.skipReason != "" {
		t.Errorf("want no skip reason on a run that acted, got %q", rep.skipReason)
	}
}
