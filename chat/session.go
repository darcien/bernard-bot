package chat

import (
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

	// lastPrompt* are the most recent priced prompt: its size in bytes and the
	// tokens the provider charged for it. Guarded by mu.
	lastPromptBytes  int
	lastPromptTokens int
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
// bytes — the same measure as msgBytes.
const (
	fallbackTokPerByte = 0.25
	minTokPerByte      = 0.05
	maxTokPerByte      = 2.0
)

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

// Reported so the approach to the tail budget is visible before it matters.
// Caller must hold s.mu.
func (s *session) size() int {
	total := 0
	for _, u := range s.units {
		total += msgBytes(u)
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
// compaction handles that, after the reply and against a measured prompt.
// Caller must hold s.mu.
func (s *session) append(unit []llm.Message) {
	if len(unit) == 0 {
		return
	}
	s.units = append(s.units, unit)
}
