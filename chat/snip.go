package chat

import (
	"fmt"
	"strconv"
	"strings"

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

// snipRegion shortens tool results in the region, leaving the tail verbatim.
// Reports how many it rewrote and the bytes recovered, for the maintenance
// log line.
//
// Idempotent: an already snipped result is skipped rather than snipped again,
// which would compound markers and eventually lose the head too.
func snipRegion(region [][]llm.Message) (results, savedBytes int) {
	for _, unit := range region {
		for i, m := range unit {
			if m.Role != "tool" || len(m.Content) < minSnipBytes ||
				strings.HasPrefix(m.Content, snippedMarker) {
				continue
			}
			snipped := snipToolResult(m.Content)
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

// originalBytes reports the size of the result before any maintenance, read
// back out of a snip marker when there is one so a prune after a snip still
// names the real number.
func originalBytes(content string) int {
	if !strings.HasPrefix(content, snippedMarker) {
		return len(content)
	}
	rest := content[len(snippedMarker):]
	end := strings.Index(rest, " bytes")
	if end < 0 {
		return len(content)
	}
	n, err := strconv.Atoi(rest[:end])
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

// snipToolResult keeps the head and tail of a result and says what went. The
// marker carries the original size for the same reason the truncation marker
// does: the model has to know whether it is looking at a page or a corner of
// one.
//
// Splitting by lines because the output is line-structured — extracted page
// text, one link or paragraph per line — so a line boundary is a meaning
// boundary. Content with too few lines to split falls back to bytes.
func snipToolResult(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= snipHead+snipTail {
		head := tools.TruncateHeadTail(content, snipHeadBytes+snipTailBytes)
		return fmt.Sprintf("%s%d bytes, single large line truncated]\n%s",
			snippedMarker, len(content), head)
	}
	return fmt.Sprintf("%s%d bytes, showing first %d lines and last %d lines]\n%s\n[... %d lines omitted ...]\n%s",
		snippedMarker, len(content), snipHead, snipTail,
		strings.Join(lines[:snipHead], "\n"),
		len(lines)-snipHead-snipTail,
		strings.Join(lines[len(lines)-snipTail:], "\n"))
}
