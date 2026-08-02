package chat

import (
	"math"
	"sync"

	"bernard/discord"
	"bernard/llm"
)

// session is one channel's conversation history: append-only units
// (everything one turn produced), oldest first. mu guards the history and is
// held for a whole turn.
//
// synced/lastSeenID are the channel-sync watermark: lastSeenID is the newest
// channel message already represented in the units. Both advance only when a
// turn commits, so a failed turn refetches the same delta.
//
// turnMu is a separate, briefly-held lock for the floor: which turn owns the
// channel and which mention is waiting. It cannot be mu — that one is held
// for the whole turn, so a second mention would block on it instead of
// recording itself.
type session struct {
	mu         sync.Mutex
	units      [][]llm.Message
	synced     bool
	lastSeenID string

	turnMu  sync.Mutex
	running bool
	pending []discord.Message

	// lastPrompt* are the most recent priced prompt: its unitSize and the
	// tokens the provider charged for it. Guarded by mu.
	lastPromptBytes  int
	lastPromptTokens int
	// What the last maintenance run did, for its log line: folded* is what
	// left the session, snipped* and pruned* what was shortened in place.
	// Guarded by mu.
	foldedUnits    int
	foldedBytes    int
	snippedResults int
	snippedBytes   int
	prunedResults  int
	prunedBytes    int
	// consecutiveCompacts and compactStuck are Reasonix's latch: a fold that
	// does not buy breathing room means the tail alone is over the trigger,
	// so folding every turn would burn a summariser call for nothing.
	// Guarded by mu.
	consecutiveCompacts int
	compactStuck        bool
}

// Token estimation mirrors Reasonix's tokPerChar: one sample from the last
// turn's real usage, no smoothing and no history, accepted only if plausible,
// with a fallback until any sample exists. Their bounds and fallback verbatim.
//
// Their name says chars, but they sum len() on strings, so the denominator is
// bytes — the same measure as unitSize.
const (
	fallbackTokPerByte = 0.25
	minTokPerByte      = 0.05
	maxTokPerByte      = 2.0
)

// contextTier names the escalation this prompt size earns, or "" below the
// first one. It labels the log line; maintain compares the same thresholds
// itself rather than switching on this string.
func contextTier(promptTokens int) string {
	switch {
	case promptTokens >= int(contextWindow*forceRatio):
		return "force"
	case promptTokens >= int(contextWindow*compactRatio):
		return "compact"
	case promptTokens >= int(contextWindow*snipRatio):
		return "snip"
	case promptTokens >= int(contextWindow*softRatio):
		return "soft"
	}
	return ""
}

// contextPct is the share of the window a prompt used, as a percentage.
func contextPct(promptTokens int) float64 {
	return round3(float64(promptTokens) * 100 / contextWindow)
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// observe records what the last call sent and what it cost. A failed run
// carries no measurement — the loop discards its result — and zeroes would
// throw away the last good sample, so only a real pair is kept.
// Caller must hold s.mu.
func (s *session) observe(promptBytes, promptTokens int) {
	if promptBytes <= 0 || promptTokens <= 0 {
		return
	}
	s.lastPromptBytes, s.lastPromptTokens = promptBytes, promptTokens
}

// tokPerByte converts a unitSize into tokens, for sizing history before any
// call has priced it. An implausible ratio means the measurement is broken,
// so it is discarded rather than used.
//
// Reasonix divides the last call's PromptTokens by the session's *current*
// size, so numerator and denominator come from different moments. Recording
// both from the same call costs one int and removes that drift.
// Caller must hold s.mu.
func (s *session) tokPerByte() float64 {
	if s.lastPromptBytes > 0 && s.lastPromptTokens > 0 {
		r := float64(s.lastPromptTokens) / float64(s.lastPromptBytes)
		if r > minTokPerByte && r < maxTokPerByte {
			return r
		}
	}
	return fallbackTokPerByte
}

// claim takes the floor for one turn. When a turn already holds it, msg joins
// the mentions waiting on that turn and false is returned — the caller must
// not start a turn, because the running one will take this mention either
// mid-flight (steer) or as its follow-up (finish).
func (s *session) claim(msg discord.Message) bool {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.running {
		s.queue(msg)
		return false
	}
	s.running = true
	return true
}

// queue records a mention waiting on the running turn. Every waiting mention
// is kept rather than only the newest: the commit folds steered IDs into the
// watermark, so a mention that was silently displaced would end up behind the
// watermark without ever having reached the model.
//
// Past pendingCap the oldest is dropped, which is safe for the same reason in
// reverse — a dropped mention was never steered, so the watermark never
// passes it and the next turn's gap sync still carries it.
//
// Caller holds turnMu.
func (s *session) queue(msg discord.Message) {
	s.pending = append(s.pending, msg)
	if len(s.pending) > pendingCap {
		s.pending = s.pending[1:]
	}
}

// steer takes the next waiting mention for the running turn to fold in
// mid-flight, oldest first, keeping the floor: the turn is absorbing the
// message rather than ending.
func (s *session) steer() (discord.Message, bool) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if len(s.pending) == 0 {
		return discord.Message{}, false
	}
	msg := s.pending[0]
	s.pending = s.pending[1:]
	return msg, true
}

// requeue puts a steered mention back when its turn produced nothing. Without
// it a mention absorbed by a turn that then failed would be answered by
// nobody: it is no longer waiting, so finish hands back nothing and no
// follow-up turn runs.
func (s *session) requeue(msg discord.Message) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.queue(msg)
}

// finish ends a turn, handing back the mention that arrived during it if
// there was one — in which case the floor stays claimed and the caller runs
// again. Releasing the floor and reading the waiting mention happen under one
// lock, so a mention arriving at that instant either claims the floor itself
// or is recorded for this turn to collect. Neither can be dropped.
//
// It is called on turn *exit*, not on commit: a turn that failed still has to
// hand off, or every mention that arrived while it was failing goes
// unanswered.
// The follow-up turn takes the newest waiting mention as its question,
// decided by snowflake rather than by arrival: each mention is handled on its
// own goroutine, so the order they reach this lock is not the order they were
// sent. The older ones ride that turn's delta — the watermark never passed
// them.
func (s *session) finish() (discord.Message, bool) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if len(s.pending) == 0 {
		s.running = false
		return discord.Message{}, false
	}
	newest := s.pending[0]
	for _, msg := range s.pending[1:] {
		if snowflake(msg.ID) > snowflake(newest.ID) {
			newest = msg
		}
	}
	s.pending = nil
	return newest, true
}

// size is the session's total unitSize, the quantity planRegion measures.
// Reported so the approach to the tail budget is visible before it matters.
// Caller must hold s.mu.
func (s *session) size() int {
	total := 0
	for _, u := range s.units {
		total += unitSize(u)
	}
	return total
}

// history flattens the units into the message list sent to the LLM.
// Caller must hold s.mu.
func (s *session) history() []llm.Message {
	var msgs []llm.Message
	for _, unit := range s.units {
		msgs = append(msgs, unit...)
	}
	return msgs
}

// append adds one turn's messages as a unit. Growth is not its problem —
// maintain handles that, after the reply and against a measured prompt.
// Caller must hold s.mu.
func (s *session) append(unit []llm.Message) {
	if len(unit) == 0 {
		return
	}
	s.units = append(s.units, unit)
}

// foldFunc turns the region into the unit that replaces it, or reports false
// when it could not — in which case the session is left alone rather than
// losing the region to an empty stand-in. Injected so the session owns the
// policy and knows nothing about LLM clients or contexts.
type foldFunc func(region [][]llm.Message, tokPerByte float64) ([]llm.Message, bool)

// needsMaintenance reports whether the last measured prompt earned any work,
// so a caller can skip the setup when there is nothing to do.
// Caller must hold s.mu.
func (s *session) needsMaintenance() bool {
	return s.lastPromptTokens >= int(contextWindow*snipRatio)
}

// maintain runs whatever the last measured prompt earned. The trigger is
// measured, the region estimated: Reasonix likewise acts on the usage of the
// round that ran and plans the kept tail with its calibrated ratio.
//
// The escalation is theirs. Below the snip tier nothing happens, because a
// rewrite here would crater the cached prefix for no reason. In the snip band
// stale tool results shrink, which is free — the page can be fetched again.
// At the compaction tier pruning gets one more free saving, and only if that
// fails to clear the trigger does the region fold into a summary.
//
// It runs between turns rather than inside one: mid-turn, the loop is still
// building tool_call/result pairs a rewrite would invalidate, and a fold is
// one more endpoint call the waiting user would be paying for in silence.
//
// What it did is recorded for its log line, because otherwise a fold is only
// visible by diffing consecutive turns — no way to verify that a change
// stopped one happening.
//
// Caller must hold s.mu.
func (s *session) maintain(hintFor snipHintFunc, fold foldFunc) {
	tokens := s.lastPromptTokens
	if tokens < int(contextWindow*compactRatio) {
		// Breathing room: whatever the last fold bought, it worked, so the
		// latch and the run count start over.
		s.consecutiveCompacts, s.compactStuck = 0, false
	}
	if tokens < int(contextWindow*snipRatio) {
		return
	}
	region, tail := planRegion(s.units, s.tokPerByte())
	if len(region) == 0 {
		return
	}
	if tokens < int(contextWindow*compactRatio) {
		s.snippedResults, s.snippedBytes = snipRegion(region, hintFor)
		return
	}

	// Prune first: eliding stale results is free, and when it alone clears
	// the trigger the conversation survives whole. Reasonix does the same
	// ahead of its (paid) summariser call.
	s.prunedResults, s.prunedBytes = pruneRegion(region)
	saved := int(float64(s.prunedBytes) * s.tokPerByte())
	force := tokens >= int(contextWindow*forceRatio)
	if !force && tokens-saved < int(contextWindow*compactRatio) {
		return
	}
	if s.compactStuck {
		return
	}
	// Below this the fold saves less than the call it costs.
	if !force && int(float64(regionBytes(region))*s.tokPerByte()) < minFoldTokens {
		return
	}

	folded, ok := fold(region, s.tokPerByte())
	if !ok {
		return
	}
	for _, u := range region {
		s.foldedBytes += unitSize(u)
	}
	s.foldedUnits = len(region)
	s.units = append([][]llm.Message{folded}, tail...)

	// A healthy fold drops the next prompt under the trigger. Folding on
	// consecutive turns means the kept tail alone exceeds it, so the loop is
	// stopped rather than run every turn.
	s.consecutiveCompacts++
	if s.consecutiveCompacts >= maxConsecutiveCompacts {
		s.compactStuck = true
	}
}

func regionBytes(region [][]llm.Message) int {
	total := 0
	for _, u := range region {
		total += unitSize(u)
	}
	return total
}

// planRegion splits units into the region maintenance may rewrite and the
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
		cost := int(float64(unitSize(units[i])) * tokPerByte)
		if len(units)-i > recentKeep && acc+cost > budget {
			break
		}
		acc += cost
		start = i
	}
	return units[:start], units[start:]
}

// unitSize approximates a unit's share of the prompt: every string the wire
// format serializes, including tool-call arguments.
func unitSize(unit []llm.Message) int {
	n := 0
	for _, m := range unit {
		n += len(m.Role) + len(m.Content) + len(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			n += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	return n
}
