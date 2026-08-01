package chat

import (
	"sync"

	"bernard/llm"
)

// session is one channel's conversation history: append-only units
// (everything one turn produced), oldest first. The mutex is held for a
// whole turn so concurrent mentions in one channel serialize.
//
// synced/lastSeenID are the channel-sync watermark: lastSeenID is the newest
// channel message already represented in the units. Both advance only when a
// turn commits, so a failed turn refetches the same delta.
type session struct {
	mu         sync.Mutex
	units      [][]llm.Message
	synced     bool
	lastSeenID string
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
