package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bernard/discord"
	"bernard/llm"
)

// The digest is tagged so the model can tell a summary of the conversation
// from the conversation, and reads as a user turn because that is the role
// the endpoint accepts between exchanges.
const (
	summaryTagOpen  = "<compaction-summary>"
	summaryTagClose = "</compaction-summary>"
)

// summarySystemPrompt is Reasonix's, re-cut for this domain. Theirs briefs a
// coding agent — files touched, commands run, edits applied — none of which
// exists here. What a chat harness has to carry across a fold is who said
// what, what was looked up and where, and what is still owed an answer.
//
// The rules at the end are theirs verbatim: terse, exact identifiers, and
// nothing invented. A summariser that embellishes is worse than no summary,
// because the fabrication outlives the transcript that would disprove it.
const summarySystemPrompt = `You are compacting the earlier part of a Discord conversation to save context.
Your summary replaces those messages; the recent exchanges are kept verbatim alongside it.
Write under these exact headings, omitting a heading only if it has no content:

## Standing facts
What people stated that still holds — names, preferences, constraints, corrections, and anything they asked to be remembered, in their own words.

## Topics
What was discussed and what was concluded, so a settled question is not reopened.

## Sources
Pages that were fetched and what they said, with their URLs exactly as given.

## Open threads
Questions asked but not answered, and anything someone is still waiting on.

Rules: be terse — bullet points and fragments, not prose. Preserve names, URLs, identifiers and numbers exactly. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing.`

// summariseRegion folds the region into one digest message, keeping small
// user turns verbatim in front of it: a fact someone stated survives the fold
// whatever the summariser does with it, which is Reasonix's kept-verbatim
// floor.
//
// A failed or empty summary degrades to a mechanical digest rather than
// aborting. Aborting would leave the session over the trigger and re-fire the
// fold every turn — the loop the stuck latch exists to catch, entered
// deliberately.
func summariseRegion(ctx context.Context, client *llm.Client, region [][]llm.Message, tokPerByte float64) []llm.Message {
	kept, fold := partitionFold(region, tokPerByte)
	summary, err := summarise(ctx, client, fold)
	if err != nil || strings.TrimSpace(summary) == "" {
		summary = mechanicalDigest(len(fold))
	}
	unit := append([]llm.Message{}, kept...)
	return append(unit, llm.Message{
		Role: "user",
		Content: summaryTagOpen + "\n" +
			"Summary of earlier conversation (older messages were compacted to save context):\n" +
			summary + "\n" + summaryTagClose,
	})
}

// partitionFold splits the region into what is kept verbatim — small user
// turns and any earlier digest, so a fold never re-summarises a summary and
// loses what it already captured — and what folds.
func partitionFold(region [][]llm.Message, tokPerByte float64) (kept []llm.Message, fold []llm.Message) {
	for _, unit := range region {
		for _, m := range unit {
			pinned := m.Role == "user" &&
				(strings.HasPrefix(m.Content, summaryTagOpen) ||
					int(float64(msgBytes([]llm.Message{m}))*tokPerByte) <= pinnedUserTokens)
			if pinned {
				kept = append(kept, m)
				continue
			}
			fold = append(fold, m)
		}
	}
	return kept, fold
}

// summarise asks the model for the digest, on its own bounded context: the
// turn's deadline belongs to the answer the user is waiting for, and this
// runs after they have it.
func summarise(ctx context.Context, client *llm.Client, fold []llm.Message) (string, error) {
	if len(fold) == 0 {
		return "", fmt.Errorf("nothing to fold")
	}
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()

	msgs := []llm.Message{
		{Role: "system", Content: summarySystemPrompt},
		{Role: "user", Content: renderTranscript(fold)},
	}
	m, _, err := client.Chat(ctx, msgs, nil)
	if err != nil {
		return "", err
	}
	return m.Content, nil
}

// renderTranscript flattens the fold into something readable. Tool calls and
// their results become plain lines: the digest is prose, so the wire shapes
// that carry them are noise, and sending them as real tool messages would
// need a matching assistant call the summariser has no use for.
func renderTranscript(fold []llm.Message) string {
	var b strings.Builder
	for _, m := range fold {
		switch {
		case m.Role == "tool":
			fmt.Fprintf(&b, "[tool result]\n%s\n\n", m.Content)
		case len(m.ToolCalls) > 0:
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "[tool call] %s %s\n", tc.Function.Name, tc.Function.Arguments)
			}
			if m.Content != "" {
				fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
			}
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "%s: %s\n\n", m.Role, m.Content)
		}
	}
	return b.String()
}

// mechanicalDigest stands in when the summariser fails. It says what was lost
// and how to recover it — the channel still holds every message — rather than
// pretending the conversation started here.
func mechanicalDigest(messages int) string {
	return fmt.Sprintf("%d earlier message(s) were folded here to free context, but the summary was unavailable. "+
		"Scroll back in the channel or ask if you need details from before this point.", messages)
}

// compactFunc turns the region into the unit that replaces it, or reports false
// when it could not — in which case the session is left alone rather than
// losing the region to an empty stand-in. Injected so the session owns the
// policy and knows nothing about LLM clients or contexts.
type compactFunc func(region [][]llm.Message, tokPerByte float64) ([]llm.Message, bool)

// compactAfterTurn runs the ladder once the answer is out, so the user never
// waits behind a summariser call. It is its own unit of work and logs its own
// lines; nothing about it belongs in the turn's.
//
// The paid part takes an admission slot like any other endpoint call, but only
// if one is free. Queueing for it would hold the session lock and the serve
// loop — so the next mention in this channel would wait out someone else's
// turn — to buy a compaction that is not urgent: the trigger is still over
// threshold next turn, and by then a slot may be free.
//
// The ladder itself is session.maybeCompact; this is admission, timing and the
// log lines around it.
func (s *Service) compactAfterTurn(msg discord.Message, sess *session) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.needsCompaction() {
		return
	}

	start := time.Now()
	tier := contextTier(sess.lastPromptTokens)
	summarised := false
	rep := sess.maybeCompact(s.tools.SnipHintFor, func(region [][]llm.Message, tokPerByte float64) ([]llm.Message, bool) {
		if !s.admission.tryEnter() {
			slog.Debug("chat compact skipped, no slot", "channel", msg.ChannelID)
			return nil, false
		}
		defer s.admission.leave()
		summarised = true
		return summariseRegion(s.ctx, s.llm, region, tokPerByte), true
	})

	// The region plan is the input to every decision after it, so it rides
	// both outcomes: without it a run that rewrote nothing cannot be told
	// from one that never had anything to rewrite.
	plan := []any{"tier", tier,
		"region_units", rep.region.units, "region_bytes", rep.region.bytes,
		"tail_units", rep.tail.units, "tail_bytes", rep.tail.bytes}

	if !rep.did() {
		// Over the trigger and nothing came of it. Previously silent, which
		// made "the tail swallowed the history" and "it never ran" the same
		// log line: none.
		slog.Debug("chat compact skipped", append([]any{"channel", msg.ChannelID,
			"turn", msg.ID, "reason", rep.skipReason}, plan...)...)
		return
	}
	attrs := append([]any{"channel", msg.ChannelID, "turn", msg.ID}, plan...)
	// _after because chat done logged session_bytes for the same session
	// seconds earlier, before any of this ran. One name for two measurements
	// is a plot with two points per turn that mean different things.
	attrs = append(attrs, "history", len(sess.units), "session_bytes_after", sess.size())
	if n := len(rep.snipped); n > 0 {
		attrs = append(attrs, "snipped_results", n,
			"snipped_bytes", savedBytes(rep.snipped),
			"snipped_tools", byTool(rep.snipped))
	}
	if n := len(rep.pruned); n > 0 {
		attrs = append(attrs, "pruned_results", n,
			"pruned_bytes", savedBytes(rep.pruned),
			"pruned_tools", byTool(rep.pruned))
	}
	if rep.compactedUnits > 0 {
		// No compacted_units: a compaction takes the whole region, so it would
		// repeat region_units on the one line where space is scarce.
		attrs = append(attrs, "compacted_bytes", rep.compactedBytes, "summarised", summarised)
	}
	if rep.stuck {
		attrs = append(attrs, "stuck", true)
	}
	slog.Info("chat compact", append(attrs, "dur", time.Since(start).Round(time.Millisecond))...)

	// Per result, because the aggregate cannot show which geometry applied.
	// Only the half of the hint that was used is logged: the byte branch never
	// reads the line counts, so printing them would name a geometry that did
	// not apply.
	for _, r := range rep.snipped {
		attrs := []any{"channel", msg.ChannelID, "turn", msg.ID, "tool", r.tool}
		if r.fallback {
			attrs = append(attrs, "fallback", true,
				"head_chars", r.hint.HeadChars, "tail_chars", r.hint.TailChars)
		} else {
			attrs = append(attrs, "head", r.hint.Head, "tail", r.hint.Tail)
		}
		slog.Debug("chat snip", append(attrs, "before", r.before, "after", r.after)...)
	}
	for _, r := range rep.pruned {
		slog.Debug("chat prune", "channel", msg.ChannelID, "turn", msg.ID,
			"tool", r.tool, "before", r.before, "after", r.after)
	}
}

// compactionReport is what one run of the ladder did, for the log lines. A
// value rather than fields on the session: it describes a run, not the
// conversation, and returning it keeps the ladder testable without a session.
type compactionReport struct {
	region, tail    span
	snipped, pruned []rewrite
	compactedUnits  int
	compactedBytes  int
	// stuck is the latch as of this run's end, tripped here or already on: the
	// tail alone exceeds the trigger, so compacting again would pay for a
	// summary that cannot help.
	stuck bool
	// skipReason names why compaction did not happen: region_empty,
	// nothing_eligible, compact_stuck, compact_too_small, compact_declined.
	// Not the inverse of did() — a run that snipped or pruned and then hit the
	// latch sets both. Empty only when the compaction itself went through.
	skipReason string
}

// span is one side of planRegion's split, measured. Bytes as well as units
// because tailBudget is denominated in tokens, which a unit count cannot be
// checked against.
type span struct {
	units int
	bytes int
}

// did reports whether the run is worth an Info line.
func (r compactionReport) did() bool {
	return len(r.snipped) > 0 || len(r.pruned) > 0 || r.compactedUnits > 0
}

// maybeCompact is the escalation ladder, Reasonix's maybeCompact: it decides
// which tier the last measured prompt earns and does the cheapest thing that
// tier licenses. Snip below the compaction trigger, since a shortened tool
// result costs nothing to re-derive. Prune first at the trigger, because when
// eliding alone clears it the conversation survives whole and the summariser
// call is skipped. Only then fold, and only if the region is worth the call.
//
// It runs between turns, never inside one: mid-turn the loop is still building
// tool_call/result pairs a rewrite would invalidate, and a summariser call is
// one more round-trip the waiting user pays for in silence.
//
// Returns what it did rather than recording it on the session — the report
// describes one run, not the conversation. What does persist is the latch:
// two compactions in a row that fail to buy breathing room mean the tail alone
// exceeds the trigger, so re-firing every turn is the loop it prevents.
//
// Caller must hold s.mu.
func (s *session) maybeCompact(hintFor snipHintFunc, compact compactFunc) (rep compactionReport) {
	// The latch is a property of the session, not of this run, but the report
	// has to carry it: a run that only prunes still reaches the aggregate line,
	// and without this that line would say a prune happened and stay silent
	// about auto-compaction being paused. Deferred so every return carries it.
	defer func() { rep.stuck = s.compactStuck }()

	tokens := s.lastPromptTokens
	if tokens < int(contextWindow*compactRatio) {
		// Breathing room: whatever the last compaction bought, it worked, so
		// the latch and the run count start over.
		s.consecutiveCompacts, s.compactStuck = 0, false
	}
	if tokens < int(contextWindow*snipRatio) {
		return rep
	}
	region, tail := planRegion(s.units, s.tokPerByte())
	// tail is measured in bytes because tailBudget is denominated in tokens: a
	// unit count cannot be checked against it, and "the tail alone is over the
	// trigger" is the diagnosis behind both region_empty and the stuck latch.
	rep.region = span{len(region), regionBytes(region)}
	rep.tail = span{len(tail), regionBytes(tail)}
	if len(region) == 0 {
		rep.skipReason = "region_empty"
		return rep
	}
	if tokens < int(contextWindow*compactRatio) {
		rep.snipped = snipRegion(region, hintFor)
		if len(rep.snipped) == 0 {
			rep.skipReason = "nothing_eligible"
		}
		return rep
	}

	// Prune first: eliding stale results is free, and when it alone clears
	// the trigger the conversation survives whole. Reasonix does the same
	// ahead of its (paid) summariser call.
	rep.pruned = pruneRegion(region)
	if len(rep.pruned) == 0 {
		// Provisional: the compaction below may still act, and each return
		// there names itself more precisely.
		rep.skipReason = "nothing_eligible"
	}
	saved := int(float64(savedBytes(rep.pruned)) * s.tokPerByte())
	force := tokens >= int(contextWindow*forceRatio)
	if !force && tokens-saved < int(contextWindow*compactRatio) {
		return rep // the prune cleared the trigger; it logs as work done
	}
	if s.compactStuck {
		rep.skipReason = "compact_stuck"
		return rep
	}
	// Below this the fold saves less than the call it costs.
	if !force && int(float64(regionBytes(region))*s.tokPerByte()) < minFoldTokens {
		rep.skipReason = "compact_too_small"
		return rep
	}

	compacted, ok := compact(region, s.tokPerByte())
	if !ok {
		rep.skipReason = "compact_declined"
		return rep
	}
	rep.skipReason = ""
	for _, u := range region {
		rep.compactedBytes += msgBytes(u)
	}
	rep.compactedUnits = len(region)
	s.units = append([][]llm.Message{compacted}, tail...)

	// A healthy compaction drops the next prompt under the trigger. Compacting
	// on consecutive turns means the kept tail alone exceeds it, so the loop
	// is stopped rather than run every turn.
	s.consecutiveCompacts++
	if s.consecutiveCompacts >= maxConsecutiveCompacts {
		s.compactStuck = true
	}
	return rep
}

// planRegion splits units into the region compaction may rewrite and the
// tail it keeps verbatim, walking back from the newest until tailBudget
// tokens are spoken for. Reasonix's tailStart, over units instead of
// messages: a unit is one turn, so the boundary can never separate a tool
// result from the call that asked for it — the alignment step they need
// falls out of the shape.
//
// recentKeep units survive whatever they estimate at. A budget in tokens
// rather than a unit count is what stops two big fetch turns holding the
// session over the trigger and re-firing the fold every turn, and
// compactTarget keeps that budget under half the window whatever the window
// turns out to be.
//
// The budget covers history only. The system prompt and the current question
// ride on top of it, as they do for Reasonix, whose pinned prefix is likewise
// outside the tail.
func planRegion(units [][]llm.Message, tokPerByte float64) (region, tail [][]llm.Message) {
	budget := min(tailBudget, int(contextWindow*compactTarget))
	start, acc := len(units), 0
	for i := len(units) - 1; i >= 0; i-- {
		cost := int(float64(msgBytes(units[i])) * tokPerByte)
		if len(units)-i > recentKeep && acc+cost > budget {
			break
		}
		acc += cost
		start = i
	}
	return units[:start], units[start:]
}

func regionBytes(region [][]llm.Message) int {
	total := 0
	for _, u := range region {
		total += msgBytes(u)
	}
	return total
}
