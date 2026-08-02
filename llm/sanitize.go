package llm

import "encoding/json"

// The wire contract both OpenAI-compatible and Anthropic APIs enforce: every
// assistant tool_calls entry must be answered by a following tool message
// carrying its id, and a tool message must answer such a call. Break it and
// the request 400s — after the model has already been paid for, and with a
// user waiting.
//
// The caller here commits history only on success, so it should never send a
// broken pairing. "Should never" is the argument that stops holding the first
// time a new caller appears, and the cost of being wrong is the whole turn.
// Reasonix runs the same repair before every request for the same reason.
const interruptedToolResult = "[tool result unavailable: the call was interrupted]"

// sanitize repairs a message list into something the endpoint will accept:
// results reordered to follow their calls, a placeholder for any call left
// unanswered, orphan tool messages dropped, and unparseable arguments
// replaced.
//
// A well-formed history returns the input slice itself, unchanged and
// unallocated — the common case, and the one where the prompt-cache prefix
// must stay byte-identical.
func sanitize(msgs []Message) []Message {
	if wellFormed(msgs) {
		return msgs
	}
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// The results answering this call are the tool messages that
			// immediately follow it.
			j := i + 1
			for j < len(msgs) && msgs[j].Role == "tool" {
				j++
			}
			out = append(out, repairArguments(m))
			out = append(out, pairResults(m.ToolCalls, msgs[i+1:j])...)
			i = j
			continue
		}
		if m.Role == "tool" {
			i++ // orphan: no call above it to answer
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

// wellFormed reports whether msgs already satisfies the contract, so the
// repair can be skipped entirely.
func wellFormed(msgs []Message) bool {
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == "tool" {
				j++
			}
			results := msgs[i+1 : j]
			if len(results) != len(m.ToolCalls) {
				return false
			}
			for k, tc := range m.ToolCalls {
				if results[k].ToolCallID != tc.ID {
					return false
				}
				if tc.Function.Arguments != "" && !json.Valid([]byte(tc.Function.Arguments)) {
					return false
				}
			}
			i = j
			continue
		}
		if m.Role == "tool" {
			return false
		}
		i++
	}
	return true
}

// pairResults returns one tool message per call, in call order, inventing a
// placeholder for a call nothing answered. Matching is by id; a result whose
// id names no call is dropped with it. A placeholder carries its call's name,
// so a repaired history still tells maintenance what produced the result.
func pairResults(calls []ToolCall, avail []Message) []Message {
	byID := make(map[string]Message, len(avail))
	for _, r := range avail {
		byID[r.ToolCallID] = r
	}
	out := make([]Message, 0, len(calls))
	for _, tc := range calls {
		if r, ok := byID[tc.ID]; ok {
			out = append(out, r)
			continue
		}
		out = append(out, Message{Role: "tool", Name: tc.Function.Name, ToolCallID: tc.ID, Content: interruptedToolResult})
	}
	return out
}

// repairArguments replaces arguments that are not valid JSON, copy-on-write so
// the caller's history is never mutated. Empty arguments pass through — some
// gateways send "" for a no-arg call.
//
// Reasonix reconstructs truncated argument JSON, which is worth it there
// because a half-streamed call can be replayed from a saved session. Nothing
// here streams, so unparseable arguments are not a truncated document but a
// broken one; the model gets an empty object and can call again.
func repairArguments(m Message) Message {
	broken := false
	for _, tc := range m.ToolCalls {
		if tc.Function.Arguments != "" && !json.Valid([]byte(tc.Function.Arguments)) {
			broken = true
			break
		}
	}
	if !broken {
		return m
	}
	calls := make([]ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if a := calls[i].Function.Arguments; a != "" && !json.Valid([]byte(a)) {
			calls[i].Function.Arguments = "{}"
		}
	}
	m.ToolCalls = calls
	return m
}
