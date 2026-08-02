package chat

import (
	"math"

	"bernard/llm"
)

// How big is this prompt against the window. The sample the provider charged
// for, the ratio derived from it, and the tiers that ratio earns.
//
// What to do about it is compaction's business, not this file's.

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

// tokPerByte converts a byte count into tokens, for sizing history before any
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

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// contextTier names the escalation this prompt size earns, or "" below the
// first one. It labels the log line; the ladder compares the same thresholds
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

// tierName is contextTier for a log line, naming the below-soft case rather
// than leaving the field off — an absent field reads as a missing value.
func tierName(promptTokens int) string {
	if t := contextTier(promptTokens); t != "" {
		return t
	}
	return "none"
}

// contextPct is the share of the window a prompt used, as a percentage.
func contextPct(promptTokens int) float64 {
	return round3(float64(promptTokens) * 100 / contextWindow)
}

// needsCompaction reports whether the last measured prompt earned any work,
// so a caller can skip the setup when there is nothing to do.
// Caller must hold s.mu.
func (s *session) needsCompaction() bool {
	return s.lastPromptTokens >= int(contextWindow*snipRatio)
}

// msgBytes sums every string the wire format serializes, tool-call arguments
// included. Callers hand it a unit, a whole prompt, or one message.
func msgBytes(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Role) + len(m.Content) + len(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			n += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	return n
}
