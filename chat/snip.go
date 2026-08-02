package chat

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"bernard/llm"
	"bernard/tools"
)

// Tool-result maintenance is the cheap half of context management: a stale
// tool result is re-derivable — the page can be fetched again — so shortening
// one costs no summariser call and drops no message. Only tool content is
// rewritten, so tool_call/result pairing survives untouched.
const (
	snippedMarker = "[snipped tool result — "
	prunedMarker  = "[elided tool result — "
)

// snipHintFunc is tools.Registry.SnipHintFor. A func rather than the registry
// itself — not to keep the package ignorant of it, which it is not, but so the
// session layer never holds one and a caller can pass nil. Nil, or a result
// with no name, takes the read-only default.
type snipHintFunc func(name string) tools.SnipHint

func (f snipHintFunc) hintFor(name string) tools.SnipHint {
	if f == nil {
		return tools.DefaultReadOnlySnip
	}
	return f(name)
}

// rewrite records one shortened result. The aggregate says how much was
// saved; this says on whose output and under which geometry, which is the only
// way to tell a resolved hint from a silent fall back to the default.
type rewrite struct {
	tool     string // "" when the message carried no name
	hint     tools.SnipHint
	before   int
	after    int
	fallback bool // took the single-large-line branch, so hint's lines went unused
}

// snipRegion shortens tool results in the region, leaving the tail verbatim.
//
// Idempotent: an already snipped result is skipped rather than snipped again,
// which would compound markers and eventually lose the head too.
func snipRegion(region [][]llm.Message, hintFor snipHintFunc) []rewrite {
	var done []rewrite
	for _, unit := range region {
		for i, m := range unit {
			if m.Role != "tool" || len(m.Content) < minSnipBytes ||
				strings.HasPrefix(m.Content, snippedMarker) {
				continue
			}
			h := hintFor.hintFor(m.Name)
			snipped, fallback := snipToolResult(m.Content, m.Name, h)
			if len(snipped) >= len(m.Content) {
				continue
			}
			done = append(done, rewrite{
				tool: m.Name, hint: h,
				before: len(m.Content), after: len(snipped),
				fallback: fallback,
			})
			unit[i].Content = snipped
		}
	}
	return done
}

func savedBytes(rs []rewrite) int {
	n := 0
	for _, r := range rs {
		n += r.before - r.after
	}
	return n
}

// byTool counts rewrites per producing tool, for the aggregate log line: it is
// the one field that shows the Name plumbing working end to end.
func byTool(rs []rewrite) string {
	order := make([]string, 0, len(rs))
	counts := make(map[string]int, len(rs))
	for _, r := range rs {
		name := r.tool
		if name == "" {
			name = "(unnamed)"
		}
		if _, seen := counts[name]; !seen {
			order = append(order, name)
		}
		counts[name]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return strings.Join(parts, ",")
}

// pruneRegion collapses tool results in the region to a one-line placeholder:
// the last free saving before anything has to leave the session. It upgrades
// a snipped result, which snipRegion deliberately will not touch again.
//
// Terminal — there is nothing left to shorten afterwards. Everything past
// this point costs a message or a summariser call.
func pruneRegion(region [][]llm.Message) []rewrite {
	var done []rewrite
	for _, unit := range region {
		for i, m := range unit {
			if m.Role != "tool" || strings.HasPrefix(m.Content, prunedMarker) {
				continue
			}
			if len(m.Content) < minSnipBytes && !strings.HasPrefix(m.Content, snippedMarker) {
				continue
			}
			pruned := pruneToolResult(m.Content)
			if len(pruned) >= len(m.Content) {
				continue
			}
			done = append(done, rewrite{tool: m.Name, before: len(m.Content), after: len(pruned)})
			unit[i].Content = pruned
		}
	}
	return done
}

// pruneToolResult states what was there and how to get it back. The source
// line survives because it is what makes the result re-derivable — a size
// alone tells the model something is missing but not how to fetch it again.
func pruneToolResult(content string) string {
	size := originalBytes(content)
	if src := sourceLine(content); src != "" {
		return fmt.Sprintf("%s%d bytes from %s; fetch it again if the content is needed]", prunedMarker, size, src)
	}
	return fmt.Sprintf("%s%d bytes; re-run the tool if the content is needed]", prunedMarker, size)
}

// originalBytes reports the size before any shortening, read back out of a snip
// marker so a prune after a snip still names the real number. Last field
// before " bytes" — a named marker puts the tool in front of it.
func originalBytes(content string) int {
	if !strings.HasPrefix(content, snippedMarker) {
		return len(content)
	}
	rest := content[len(snippedMarker):]
	end := strings.Index(rest, " bytes")
	if end < 0 {
		return len(content)
	}
	fields := strings.Fields(rest[:end])
	if len(fields) == 0 {
		return len(content)
	}
	n, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return len(content)
	}
	return n
}

// sourceLine returns the harness's citation prefix, the "[N] source: <url>"
// line runToolLoop puts in front of a Sourced result. Snipping pushes it down
// by a line, so look at the first few rather than only the first.
func sourceLine(content string) string {
	for i, line := range strings.SplitN(content, "\n", 4) {
		if i == 3 {
			break
		}
		if _, url, ok := strings.Cut(line, "] source: "); ok && strings.HasPrefix(line, "[") {
			return url
		}
	}
	return ""
}

// snipToolResult keeps head and tail in the geometry the producing tool asked
// for. The marker names the tool and the original size: the model has to know
// whether it is reading a page or a corner of one, and what to re-run.
//
// Splits on lines because the output is line-structured, so a line boundary is
// a meaning boundary. Too few lines to split falls back to bytes.
//
// The byte fallback is Reasonix's: head at most half the content, tail at most
// a quarter, so a snip always shrinks even when the hint's char budgets exceed
// what is there. Rune boundaries, or the JSON encode corrupts.
//
// Reports which branch it took: the byte one never reads the hint's line
// counts, so a log without this would name a geometry that never applied.
func snipToolResult(content, name string, h tools.SnipHint) (out string, fallback bool) {
	label := ""
	if name != "" {
		label = name + ", "
	}
	lines := strings.Split(content, "\n")
	// A hint keeping no lines would leave the line branch emitting markers
	// and nothing else.
	if h.Head <= 0 || h.Tail <= 0 || len(lines) <= h.Head+h.Tail {
		head := firstRunes(content, min(h.HeadChars, len(content)/2))
		tail := lastRunes(content, min(h.TailChars, len(content)/4))
		return fmt.Sprintf("%s%s%d bytes, single large line truncated]\n%s\n[... %d bytes omitted ...]\n%s",
			snippedMarker, label, len(content),
			head, len(content)-len(head)-len(tail), tail), true
	}
	return fmt.Sprintf("%s%s%d bytes, showing first %d lines and last %d lines]\n%s\n[... %d lines omitted ...]\n%s",
		snippedMarker, label, len(content), h.Head, h.Tail,
		strings.Join(lines[:h.Head], "\n"),
		len(lines)-h.Head-h.Tail,
		strings.Join(lines[len(lines)-h.Tail:], "\n")), false
}

func firstRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func lastRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
