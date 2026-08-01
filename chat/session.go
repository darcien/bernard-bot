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
	pending *discord.Message
}

// claim takes the floor for one turn. When a turn already holds it, msg is
// recorded as the waiting mention instead and false is returned — the caller
// must not start a turn, because the running one will collect this mention
// when it finishes.
//
// Newest wins, decided by snowflake rather than by arrival: each mention is
// handled on its own goroutine, so the order they reach this lock is not the
// order they were sent. A displaced mention is not lost — it is still a
// channel message and rides the next turn's delta.
func (s *session) claim(msg discord.Message) bool {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.running {
		if s.pending == nil || snowflake(msg.ID) > snowflake(s.pending.ID) {
			s.pending = &msg
		}
		return false
	}
	s.running = true
	return true
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
func (s *session) finish() (discord.Message, bool) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.pending == nil {
		s.running = false
		return discord.Message{}, false
	}
	msg := *s.pending
	s.pending = nil
	return msg, true
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

// append adds one turn's messages as a unit and trims to budget.
// Caller must hold s.mu.
func (s *session) append(unit []llm.Message) {
	if len(unit) == 0 {
		return
	}
	s.units = trimUnits(append(s.units, unit), sessionBudget, sessionFloor)
}

// trimUnits drops oldest units until the total size is at or under floor —
// but only when the total exceeds budget (hysteresis: rare, big trims), and
// never the newest unit, even if it alone exceeds the floor.
func trimUnits(units [][]llm.Message, budget, floor int) [][]llm.Message {
	total := 0
	for _, u := range units {
		total += unitSize(u)
	}
	if total <= budget {
		return units
	}
	for len(units) > 1 && total > floor {
		total -= unitSize(units[0])
		units = units[1:]
	}
	return units
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
