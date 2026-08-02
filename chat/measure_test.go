package chat

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"bernard/discord"
	"bernard/llm"
)

// These two are instruments, not guarantees. They run the harness against the
// real LLM endpoint and print what it logged, so the numbers context sizing is
// decided from — prompt tokens, cache hits, tokens per byte — come from the
// provider rather than from a stub that would only echo our own assumptions
// back. Their assertions are deliberately thin: enough to catch a run that
// silently failed, not enough to pretend a live session is a correctness test.
// Reading the output is the point.
//
// Opt-in because they spend money, need the network, and read pages other
// people control. A failure here can mean a site changed rather than that
// anything is broken:
//
//	set -a && . ./.env.test && set +a && MEASURE=1 go test ./chat -run TestMeasure -v
//
// Reference points from the run of 2026-08-02, for comparison rather than
// assertion: tok_per_byte settled at 0.309–0.341 after the opening turn,
// first-call cache hit ran 0.94–0.997 from the second turn on, and the gap
// between prompt_bytes and session_bytes — the system prompt and current
// question, which are not history — was 1.0–2.0 KB per turn.
//
// Discord is faked in both: the subject is the chat loop, and a live channel
// would add flakiness without adding evidence.

// measureClient builds the client from the environment, or skips.
func measureClient(t *testing.T) *llm.Client {
	t.Helper()
	if os.Getenv("MEASURE") == "" {
		t.Skip("set MEASURE=1 to run the live measurement")
	}
	base, key := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY")
	if base == "" || key == "" {
		t.Skip("LLM_BASE_URL and LLM_API_KEY needed")
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-v4-flash"
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	return llm.NewClient(base, key, model)
}

// TestMeasureLiveSession drives a session the shape a real channel takes —
// chat turns, fetch turns, and one page that renders its content in
// JavaScript, which is what the thin-extraction signal exists to catch — and
// prints the per-turn measurements. The `chat done` lines it emits are the
// output worth reading.
func TestMeasureLiveSession(t *testing.T) {
	client := measureClient(t)

	var replies []string
	fakeDiscord(t, &replies, "[]")
	s := New(client, testBotID)

	questions := []string{
		"hai bernard, kenalin gue darcien. inget ya nama gue.",
		"what time is it in tokyo right now?",
		"fetch https://news.ycombinator.com and tell me the top 3 story titles",
		"anything about go or rust in there?",
		"now fetch https://brutalist.report/ and summarise what the tech section is covering",
		"which of those two sites had more stories about AI?",
		"fetch https://knowyourmeme.com/memes and tell me what's trending",
		"ok last one: what's my name, and what did i ask you first?",
	}
	for i, q := range questions {
		msg := mentionMsg()
		msg.ID = fmt.Sprintf("%d", 100+i)
		msg.Content = "<@" + testBotID + "> " + q
		msg.Author = discord.User{ID: "u1", Username: "darcien"}
		if !s.HandleMention(msg) {
			t.Fatalf("mention %d not accepted", i)
		}
		if !s.Wait(3 * time.Minute) {
			t.Fatalf("turn %d did not finish", i)
		}
	}

	// A session that timed out or hit the busy path still prints
	// plausible-looking numbers, so rule that out before trusting them.
	if len(replies) != len(questions) {
		t.Fatalf("want a reply per question, got %d of %d", len(replies), len(questions))
	}
	for i, r := range replies {
		if r == timeoutReply || r == busyReply || strings.HasPrefix(r, "error bro") {
			t.Fatalf("turn %d did not answer: %s", i, r)
		}
	}

	sess := s.sessionFor("chan-mention")
	sess.mu.Lock()
	defer sess.mu.Unlock()
	t.Logf("MEASURED history=%d session_bytes=%d prompt_bytes=%d prompt_tokens=%d tok_per_byte=%v ctx_pct=%v",
		len(sess.units), sess.size(), sess.lastPromptBytes, sess.lastPromptTokens,
		round3(sess.tokPerByte()), contextPct(sess.lastPromptTokens))
	for i, r := range replies {
		t.Logf("REPLY %d: %s", i, truncateLog(r))
	}
}

// TestMeasureFold runs snip and the fold over a real fetched page against the
// real summariser and prints the digest for a person to judge — whether a
// summary is any good is not something an assertion settles. What it does
// assert is the part that must hold whatever the model writes: the summariser
// was actually reached, and a user's own words survived the fold verbatim.
//
// The trigger is fed a measured value rather than waited for, because ordinary
// traffic never reaches the snip tier at a 1M window. Everything downstream of
// the trigger is the real code on real content.
func TestMeasureFold(t *testing.T) {
	client := measureClient(t)
	page := fetchPage(t, "https://news.ycombinator.com")

	stated := "darcien: call me boss and remember the deploy is friday"
	region := [][]llm.Message{
		{
			{Role: "user", Content: stated},
			{Role: "assistant", Content: "sure boss, friday deploy noted"},
		},
		{
			{Role: "user", Content: "darcien: what's on hacker news"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function",
				Function: llm.FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://news.ycombinator.com"}`}}}},
			{Role: "tool", ToolCallID: "c1", Content: "[1] source: https://news.ycombinator.com\n\n" + page},
			{Role: "assistant", Content: "here are the top stories ..."},
		},
	}

	before := regionBytes(region)
	snipped := snipRegion(region, nil)
	t.Logf("SNIP results=%d saved=%d of %d bytes", len(snipped), savedBytes(snipped), before)

	folded := summariseRegion(t.Context(), client, region, 0.31)
	digest := folded[len(folded)-1].Content
	t.Logf("FOLD kept=%d region_bytes=%d digest_bytes=%d", len(folded)-1, regionBytes(region), len(digest))
	t.Logf("DIGEST:\n%s", digest)

	if strings.Contains(digest, "summary was unavailable") {
		t.Error("the summariser call failed, so this digest is the mechanical fallback and measures nothing")
	}
	if folded[0].Content != stated {
		t.Errorf("want the user's own words kept verbatim, got %q", folded[0].Content)
	}
}

func fetchPage(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Skipf("no network: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func truncateLog(s string) string {
	if len(s) <= 300 {
		return s
	}
	return s[:300] + "..."
}
