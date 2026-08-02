package chat

import (
	"strings"
	"testing"

	"bernard/llm"
)

// Tiers name the escalation Reasonix would run; below the first one there is
// nothing to report.
func TestContextTier(t *testing.T) {
	cases := []struct {
		tokens int
		want   string
	}{
		{0, ""},
		{contextWindow*softRatio - 1, ""},
		{contextWindow * softRatio, "soft"},
		{contextWindow * snipRatio, "snip"},
		{contextWindow * compactRatio, "compact"},
		{contextWindow * forceRatio, "force"},
		{contextWindow, "force"},
	}
	for _, tc := range cases {
		if got := contextTier(tc.tokens); got != tc.want {
			t.Errorf("%d tokens: got %q, want %q", tc.tokens, got, tc.want)
		}
	}
}

// The ratio is the last observation, used raw — no smoothing, no history.
func TestSession_TokPerByteUsesTheLastObservation(t *testing.T) {
	sess := &session{}
	if got := sess.tokPerByte(); got != fallbackTokPerByte {
		t.Fatalf("want the fallback before any call, got %v", got)
	}

	sess.observe(1000, 320)
	if got := sess.tokPerByte(); got != 0.32 {
		t.Errorf("want the sample used whole, got %v", got)
	}

	sess.observe(1000, 420)
	if got := sess.tokPerByte(); got != 0.42 {
		t.Errorf("want the newest sample, not an average, got %v", got)
	}
}

// A failed turn returns an empty loopResult, so it observes zeros. Those must
// not evict a good sample — a failure arrives when the session is largest,
// which is exactly when the ratio is about to matter.
func TestSession_ObserveKeepsTheLastGoodSample(t *testing.T) {
	sess := &session{}
	sess.observe(1000, 320)
	sess.observe(0, 0)
	if got := sess.tokPerByte(); got != 0.32 {
		t.Errorf("want the last good sample kept, got %v", got)
	}
}

// An implausible ratio means the measurement is broken, so the fallback
// stands rather than a nonsense number propagating into tail sizing.
func TestSession_TokPerByteRejectsNonsense(t *testing.T) {
	cases := []struct {
		name          string
		bytes, tokens int
	}{
		{"no bytes", 0, 100},
		{"no tokens", 1000, 0},
		{"denser than two tokens per byte", 100, 900},
		{"impossibly sparse", 100000, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := &session{}
			sess.observe(tc.bytes, tc.tokens)
			if got := sess.tokPerByte(); got != fallbackTokPerByte {
				t.Errorf("want the fallback, got %v", got)
			}
		})
	}
}

func TestMsgBytes_CountsToolCallArguments(t *testing.T) {
	unit := []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://example.com"}`},
		}},
	}}
	if got := msgBytes(unit); got < len(`{"url":"https://example.com"}`) {
		t.Errorf("want arguments counted in size, got %d", got)
	}
}

func textUnit(size int) []llm.Message {
	return []llm.Message{{Role: "user", Content: strings.Repeat("a", size)}}
}
