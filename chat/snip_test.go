package chat

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"bernard/llm"
	"bernard/tools"
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
		results, saved := snipRegion([][]llm.Message{unit}, nil)
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
		// Unnamed result: the read-only default, exactly.
		d := tools.DefaultReadOnlySnip
		if !strings.Contains(got, fmt.Sprintf("showing first %d lines and last %d lines", d.Head, d.Tail)) {
			t.Errorf("want the read-only default %d/%d, got %q", d.Head, d.Tail, got[:120])
		}
		if n := strings.Count(got, "\n") + 1; n != d.Head+d.Tail+2 {
			// +2: the marker line and the "lines omitted" line.
			t.Errorf("want %d lines kept, got %d", d.Head+d.Tail, n)
		}
	})

	// Geometry comes from the producing tool, not a constant here.
	t.Run("uses the producing tool's geometry", func(t *testing.T) {
		reg := tools.NewRegistry(toolResultCap, tools.NewWebFetch(webFetchTimeout))
		unit := toolUnit(linesOf(500))
		unit[2].Name = "web_fetch"
		if results, _ := snipRegion([][]llm.Message{unit}, reg.SnipHintFor); results != 1 {
			t.Fatalf("want the result snipped, got %d", results)
		}
		got := unit[2].Content
		want := reg.SnipHintFor("web_fetch")
		if !strings.Contains(got, fmt.Sprintf("showing first %d lines and last %d lines", want.Head, want.Tail)) {
			t.Errorf("want web_fetch's %d/%d geometry, got %q", want.Head, want.Tail, got[:120])
		}
		// Named so a prune, or the model, knows what to re-run.
		if !strings.HasPrefix(got, snippedMarker+"web_fetch, ") {
			t.Errorf("want the tool named in the marker, got %q", got[:80])
		}
		body := got[strings.Index(got, "]\n")+2:]
		head, _, _ := strings.Cut(body, "\n[... ")
		if n := strings.Count(head, "\n") + 1; n != want.Head {
			t.Errorf("want %d head lines, got %d", want.Head, n)
		}
		_, tail, _ := strings.Cut(body, "lines omitted ...]\n")
		if n := strings.Count(tail, "\n") + 1; n != want.Tail {
			t.Errorf("want %d tail lines, got %d", want.Tail, n)
		}
	})

	// Only tool content is rewritten: touching the assistant message would
	// break the tool_call it carries.
	t.Run("leaves everything that is not a tool result", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		before := unit[0].Content + unit[1].Content
		snipRegion([][]llm.Message{unit}, nil)
		if unit[0].Content+unit[1].Content != before {
			t.Error("want user and assistant messages untouched")
		}
	})

	t.Run("small results are left alone", func(t *testing.T) {
		unit := toolUnit(strings.Repeat("x", minSnipBytes-1))
		if results, _ := snipRegion([][]llm.Message{unit}, nil); results != 0 {
			t.Errorf("want nothing snipped below minSnipBytes, got %d", results)
		}
	})

	// Snipping a snipped result would compound markers and eventually eat the
	// head it was meant to protect.
	t.Run("idempotent", func(t *testing.T) {
		unit := toolUnit(linesOf(500))
		snipRegion([][]llm.Message{unit}, nil)
		once := unit[2].Content
		if results, _ := snipRegion([][]llm.Message{unit}, nil); results != 0 {
			t.Errorf("want a second pass to be a no-op, got %d", results)
		}
		if unit[2].Content != once {
			t.Error("want the content unchanged by the second pass")
		}
	})

	// A single huge line has nothing to split on, so bytes stand in for lines.
	t.Run("falls back to bytes without enough lines", func(t *testing.T) {
		unit := toolUnit(strings.Repeat("x", 100_000))
		results, saved := snipRegion([][]llm.Message{unit}, nil)
		if results != 1 || saved <= 0 {
			t.Fatalf("want the single line snipped, got %d (%d bytes)", results, saved)
		}
		got := unit[2].Content
		if !strings.Contains(got, "single large line truncated") {
			t.Errorf("want the fallback marker, got %q", got[:80])
		}
		if !strings.Contains(got, "bytes omitted") {
			t.Errorf("want the omitted byte count, got %q", got)
		}
	})

	// Half head, quarter tail — Reasonix's asymmetry, and why a snip shrinks
	// even when the hint's char budgets exceed the content.
	t.Run("the byte fallback always shrinks", func(t *testing.T) {
		for _, size := range []int{minSnipBytes, 4000, 100_000} {
			content := strings.Repeat("x", size)
			got := snipToolResult(content, "", tools.DefaultReadOnlySnip)
			if len(got) >= len(content) {
				t.Errorf("size %d: want a shorter result, got %d bytes", size, len(got))
			}
		}
	})

	// Zero line counts would keep lines[:0] and lines[len:] — markers only.
	t.Run("a hint that keeps no lines still keeps content", func(t *testing.T) {
		content := linesOf(500)
		got := snipToolResult(content, "odd", tools.SnipHint{HeadChars: 8000, TailChars: 2000})
		if len(got) >= len(content) {
			t.Errorf("want a shorter result, got %d bytes", len(got))
		}
		if !strings.Contains(got, strings.Repeat("x", 40)) {
			t.Errorf("want content kept, got %q", got)
		}
	})

	// A cut inside a multibyte character corrupts the JSON encode.
	t.Run("the byte fallback cuts on rune boundaries", func(t *testing.T) {
		got := snipToolResult(strings.Repeat("日", 20_000), "web_fetch", tools.DefaultReadOnlySnip)
		if !utf8.ValidString(got) {
			t.Error("want valid UTF-8")
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
	sess.maintain(nil, failFold(t))

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
		snipRegion([][]llm.Message{unit}, nil)
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
	sess.maintain(nil, failFold(t))

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
	sess.maintain(nil, stubFold)

	if sess.foldedUnits == 0 {
		t.Fatal("want the region folded at the force ratio")
	}
	if got := sess.units[0][0].Content; !strings.HasPrefix(got, summaryTagOpen) {
		t.Errorf("want the digest first in history, got %q", got)
	}
}
