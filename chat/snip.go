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

// snipHintFunc is tools.Registry.SnipHintFor, passed in rather than reached
// for so this package stays ignorant of the registry. Nil, or a result with no
// name, takes the read-only default.
type snipHintFunc func(name string) tools.SnipHint

func (f snipHintFunc) hintFor(name string) tools.SnipHint {
	if f == nil {
		return tools.DefaultReadOnlySnip
	}
	return f(name)
}

// snipRegion shortens tool results in the region, leaving the tail verbatim.
// Reports how many it rewrote and the bytes recovered, for the maintenance
// log line.
//
// Idempotent: an already snipped result is skipped rather than snipped again,
// which would compound markers and eventually lose the head too.
func snipRegion(region [][]llm.Message, hintFor snipHintFunc) (results, savedBytes int) {
	for _, unit := range region {
		for i, m := range unit {
			if m.Role != "tool" || len(m.Content) < minSnipBytes ||
				strings.HasPrefix(m.Content, snippedMarker) {
				continue
			}
			snipped := snipToolResult(m.Content, m.Name, hintFor.hintFor(m.Name))
			if len(snipped) >= len(m.Content) {
				continue
			}
			savedBytes += len(m.Content) - len(snipped)
			results++
			unit[i].Content = snipped
		}
	}
	return results, savedBytes
}

// pruneRegion collapses tool results in the region to a one-line placeholder:
// the last free saving before anything has to leave the session. It upgrades
// a snipped result, which snipRegion deliberately will not touch again.
//
// Terminal — there is nothing left to shorten afterwards. Everything past
// this point costs a message or a summariser call.
func pruneRegion(region [][]llm.Message) (results, savedBytes int) {
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
			savedBytes += len(m.Content) - len(pruned)
			results++
			unit[i].Content = pruned
		}
	}
	return results, savedBytes
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

// originalBytes reports the pre-maintenance size, read back out of a snip
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
func snipToolResult(content, name string, h tools.SnipHint) string {
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
			head, len(content)-len(head)-len(tail), tail)
	}
	return fmt.Sprintf("%s%s%d bytes, showing first %d lines and last %d lines]\n%s\n[... %d lines omitted ...]\n%s",
		snippedMarker, label, len(content), h.Head, h.Tail,
		strings.Join(lines[:h.Head], "\n"),
		len(lines)-h.Head-h.Tail,
		strings.Join(lines[len(lines)-h.Tail:], "\n"))
}

// firstRunes returns the first n bytes, back to a rune boundary.
func firstRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// lastRunes returns the last n bytes, forward to a rune boundary.
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
