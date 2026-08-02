package chat

import (
	"strconv"
	"strings"
	"testing"

	"bernard/llm"
)

func toolUnit(content string) []llm.Message {
	return []llm.Message{
		{Role: "user", Content: "u: fetch it"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},
		{Role: "tool", ToolCallID: "c1", Content: content},
	}
}

func linesOf(n int) string {
	return strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 40)+"\n", n), "\n")
}

func TestSnipRegion(t *testing.T) {
	t.Run("keeps head and tail and says what went", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		results, saved := snipRegion([][]llm.Message{unit})
		if results != 1 || saved <= 0 {
			t.Fatalf("want one result snipped with bytes saved, got %d (%d bytes)", results, saved)
		}
		got := unit[2].Content
		if !strings.HasPrefix(got, snippedMarker) {
			t.Errorf("want the marker first, got %q", got[:60])
		}
		if !strings.Contains(got, "lines omitted") {
			t.Errorf("want the omitted count, got %q", got)
		}
		if n := strings.Count(got, "\n") + 1; n > snipHead+snipTail+4 {
			t.Errorf("want roughly %d lines kept, got %d", snipHead+snipTail, n)
		}
	})

	// Only tool content is rewritten: touching the assistant message would
	// break the tool_call it carries.
	t.Run("leaves everything that is not a tool result", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		before := unit[0].Content + unit[1].Content
		snipRegion([][]llm.Message{unit})
		if unit[0].Content+unit[1].Content != before {
			t.Error("want user and assistant messages untouched")
		}
	})

	t.Run("small results are left alone", func(t *testing.T) {
		unit := toolUnit(strings.Repeat("x", minSnipBytes-1))
		if results, _ := snipRegion([][]llm.Message{unit}); results != 0 {
			t.Errorf("want nothing snipped below minSnipBytes, got %d", results)
		}
	})

	// Snipping a snipped result would compound markers and eventually eat the
	// head it was meant to protect.
	t.Run("idempotent", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		snipRegion([][]llm.Message{unit})
		once := unit[2].Content
		if results, _ := snipRegion([][]llm.Message{unit}); results != 0 {
			t.Errorf("want a second pass to be a no-op, got %d", results)
		}
		if unit[2].Content != once {
			t.Error("want the content unchanged by the second pass")
		}
	})

	// A single huge line has nothing to split on, so bytes stand in for lines.
	t.Run("falls back to bytes without enough lines", func(t *testing.T) {
		unit := toolUnit(strings.Repeat("x", 100_000))
		results, saved := snipRegion([][]llm.Message{unit})
		if results != 1 || saved <= 0 {
			t.Fatalf("want the single line snipped, got %d (%d bytes)", results, saved)
		}
		if !strings.Contains(unit[2].Content, "single large line truncated") {
			t.Errorf("want the fallback marker, got %q", unit[2].Content[:80])
		}
	})
}

// The snip band shortens tool results and drops nothing; only the compaction
// tier removes units.
func TestSession_AppendSnipsInTheSnipBand(t *testing.T) {
	sess := &session{}
	sess.observe(100_000, int(contextWindow*snipRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(500)))
	}
	sess.maintain(failFold(t))

	if len(sess.units) != 4 || sess.foldedUnits != 0 {
		t.Fatalf("want every unit kept, got %d (trimmed %d)", len(sess.units), sess.foldedUnits)
	}
	if sess.snippedResults == 0 || sess.snippedBytes == 0 {
		t.Fatalf("want tool results snipped, got %d (%d bytes)", sess.snippedResults, sess.snippedBytes)
	}
	// The tail is what the model still needs verbatim.
	if newest := sess.units[len(sess.units)-1][2].Content; strings.HasPrefix(newest, snippedMarker) {
		t.Error("want the newest unit left verbatim")
	}
}

func TestPruneRegion(t *testing.T) {
	// The source is what makes the result re-derivable, so it outlives the
	// content it came with.
	t.Run("keeps the source and the size", func(t *testing.T) {
		unit := toolUnit("[1] source: https://example.com/page\n\n" + linesOf(500))
		size := len(unit[2].Content)
		results, saved := pruneRegion([][]llm.Message{unit})
		if results != 1 || saved <= 0 {
			t.Fatalf("want one result pruned, got %d (%d bytes)", results, saved)
		}
		got := unit[2].Content
		if !strings.HasPrefix(got, prunedMarker) || !strings.Contains(got, "https://example.com/page") {
			t.Errorf("want the marker and the source, got %q", got)
		}
		if !strings.Contains(got, strconv.Itoa(size)) {
			t.Errorf("want the original size %d, got %q", size, got)
		}
	})

	// Snipping already took the middle; pruning has to report what was there
	// before either pass, not what the snip left.
	t.Run("upgrades a snipped result and reports the original size", func(t *testing.T) {
		unit := toolUnit("[1] source: https://example.com/page\n" + linesOf(500))
		size := len(unit[2].Content)
		snipRegion([][]llm.Message{unit})
		results, _ := pruneRegion([][]llm.Message{unit})
		if results != 1 {
			t.Fatalf("want the snipped result pruned, got %d", results)
		}
		if !strings.Contains(unit[2].Content, strconv.Itoa(size)) {
			t.Errorf("want the pre-snip size %d, got %q", size, unit[2].Content)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		pruneRegion([][]llm.Message{unit})
		if results, _ := pruneRegion([][]llm.Message{unit}); results != 0 {
			t.Errorf("want a second pass to be a no-op, got %d", results)
		}
	})
}

// failFold fails the test if a fold is attempted: these tiers must not reach
// the summariser.
func failFold(t *testing.T) foldFunc {
	return func(region [][]llm.Message, _ float64) ([]llm.Message, bool) {
		t.Helper()
		t.Errorf("want no fold, got one over %d units", len(region))
		return nil, false
	}
}

func stubFold(region [][]llm.Message, _ float64) ([]llm.Message, bool) {
	return []llm.Message{{Role: "user", Content: summaryTagOpen + "\ndigest\n" + summaryTagClose}}, true
}

// Pruning is free; dropping units is not. A turn whose prune clears the
// trigger keeps its whole conversation.
func TestSession_AppendPrunesBeforeDropping(t *testing.T) {
	sess := &session{}
	// Ratio chosen so the pruned bytes convert to more than the overshoot.
	sess.observe(100_000, int(contextWindow*compactRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(2000)))
	}
	sess.maintain(failFold(t))

	if sess.prunedResults == 0 {
		t.Fatal("want tool results pruned at the compaction tier")
	}
	if sess.foldedUnits != 0 {
		t.Errorf("want nothing dropped once the prune cleared the trigger, dropped %d", sess.foldedUnits)
	}
	if len(sess.units) != 4 {
		t.Errorf("want every unit kept, got %d", len(sess.units))
	}
}

// Past the force ratio the prune runs but no longer buys a reprieve.
func TestSession_AppendDropsAtTheForceRatio(t *testing.T) {
	sess := &session{}
	sess.observe(100_000, int(contextWindow*forceRatio))
	for range 4 {
		sess.append(toolUnit(linesOf(2000)))
	}
	sess.maintain(stubFold)

	if sess.foldedUnits == 0 {
		t.Fatal("want the region folded at the force ratio")
	}
	if got := sess.units[0][0].Content; !strings.HasPrefix(got, summaryTagOpen) {
		t.Errorf("want the digest first in history, got %q", got)
	}
}
