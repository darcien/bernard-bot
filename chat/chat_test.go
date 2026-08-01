package chat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bernard/discord"
	"bernard/llm"
)

const testBotID = "app123"

// fakeDiscord serves channel-history GETs (channelHistory JSON, "[]" if
// empty) and captures posted replies. Restores DiscordAPIBase on cleanup.
func fakeDiscord(t *testing.T, replies *[]string, channelHistory string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if channelHistory == "" {
				channelHistory = "[]"
			}
			_, _ = w.Write([]byte(channelHistory))
		case strings.HasSuffix(r.URL.Path, "/typing"):
			// typing indicator, not a message
		default:
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(body, &payload)
			*replies = append(*replies, payload.Content)
		}
	}))
	t.Cleanup(srv.Close)

	orig := discord.DiscordAPIBase
	discord.DiscordAPIBase = srv.URL
	t.Cleanup(func() { discord.DiscordAPIBase = orig })
}

// newTestService wires a Service at a canned LLM server.
func newTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(llm.NewClient(srv.URL, "test-key", "test-model"), testBotID)
}

func finalReply(content string) string {
	b, _ := json.Marshal(content)
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%s}}]}`, b)
}

func mentionMsg() discord.Message {
	return discord.Message{
		ID:        "9",
		ChannelID: "chan-mention",
		Content:   "<@app123> what time is it",
		Author:    discord.User{ID: "u1", Username: "tester"},
		Mentions:  []discord.User{{ID: testBotID}},
	}
}

func TestHandleMention_Filters(t *testing.T) {
	s := New(&llm.Client{}, testBotID)

	cases := []struct {
		name string
		mut  func(*discord.Message)
	}{
		{"own message", func(m *discord.Message) { m.Author.ID = testBotID }},
		{"other bot", func(m *discord.Message) { m.Author.Bot = true }},
		{"webhook", func(m *discord.Message) { m.WebhookID = "wh1" }},
		{"no mention of the bot", func(m *discord.Message) { m.Mentions = []discord.User{{ID: "someone"}} }},
		{"missing channel", func(m *discord.Message) { m.ChannelID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := mentionMsg()
			tc.mut(&msg)
			if s.HandleMention(msg) {
				t.Error("want message rejected")
			}
		})
	}
}

func TestStripMention(t *testing.T) {
	if got := stripMention("<@app123> hello", testBotID); got != "hello" {
		t.Errorf("got %q", got)
	}
	if got := stripMention("hey <@!app123>, hi", testBotID); got != "hey , hi" {
		t.Errorf("got %q", got)
	}
	if got := stripMention("<@app123>", testBotID); got != "" {
		t.Errorf("want empty for bare mention, got %q", got)
	}
}

// Full path: accepted, pipeline runs, reply posted to the channel, and the
// watermark covers the mention so the next sync doesn't duplicate it.
func TestHandleMention_RepliesInChannel(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(finalReply("jam tidur domba bro")))
	})

	if !s.HandleMention(mentionMsg()) {
		t.Fatal("want mention accepted")
	}
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}

	if len(replies) != 1 || replies[0] != "jam tidur domba bro" {
		t.Errorf("want plain reply, got %v", replies)
	}
	sess := s.sessionFor("chan-mention")
	if sess.lastSeenID != "9" {
		t.Errorf("want watermark past the mention message, got %q", sess.lastSeenID)
	}
	if len(sess.units) != 1 {
		t.Fatalf("want 1 committed unit, got %d", len(sess.units))
	}
	if got := sess.units[0][0].Content; got != "tester: what time is it" {
		t.Errorf("want stripped question as current turn, got %q", got)
	}
}

func TestHandleMention_EmptyQuestion(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("want no LLM call for empty question")
	})

	msg := mentionMsg()
	msg.Content = "<@app123>"
	if !s.HandleMention(msg) {
		t.Fatal("want mention accepted")
	}
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}
	if len(replies) != 1 || replies[0] != emptyAskReply {
		t.Errorf("want static reply, got %v", replies)
	}
}

func TestHandleMention_Offline(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := New(nil, testBotID)

	if !s.HandleMention(mentionMsg()) {
		t.Fatal("want mention accepted")
	}
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}
	if len(replies) != 1 || replies[0] != offlineReply {
		t.Errorf("want offline reply, got %v", replies)
	}
}

// A mention that arrives while the service is shutting down is answered
// with the restarting note rather than hanging on admission: the
// turn's own context does not exist yet, so the admission is the only thing that
// can notice.
func TestHandleMention_RestartingWhenShutdownBeatsAdmission(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("LLM must not be called when admission fails")
	})
	s.admission = newAdmission(0) // no slot will ever open
	s.Cancel()

	if !s.HandleMention(mentionMsg()) {
		t.Fatal("want mention accepted")
	}
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}
	if len(replies) != 1 || replies[0] != restartingReply {
		t.Errorf("got %q, want %q", replies, restartingReply)
	}
}

// waitForPending blocks until the channel's waiting mention is the given ID,
// so a test can be sure a mention was queued rather than racing to run.
func waitForPending(t *testing.T, sess *session, id string) {
	t.Helper()
	waitFor(t, func() bool {
		sess.turnMu.Lock()
		defer sess.turnMu.Unlock()
		return sess.pending != nil && sess.pending.ID == id
	})
}

// Mentions arriving while a turn works are collected into one follow-up turn
// instead of each starting their own: three mentions, two turns.
func TestHandleMention_CollectsMentionsArrivingMidTurn(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")

	var calls atomic.Int32
	release := make(chan struct{})
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-release // hold the first turn open while more mentions land
		}
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	s.HandleMention(mentionMsg())
	waitFor(t, func() bool { return calls.Load() == 1 })

	sess := s.sessionFor("chan-mention")
	for _, id := range []string{"10", "11"} {
		msg := mentionMsg()
		msg.ID = id
		s.HandleMention(msg)
		waitForPending(t, sess, id)
	}

	close(release)
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("want 2 turns for 3 mentions, got %d", got)
	}
	if len(replies) != 2 {
		t.Errorf("want 2 replies, got %v", replies)
	}
}

// The mention that "newest wins" displaced is not lost: it is still a channel
// message, so the follow-up turn's gap sync puts it in the prompt. This is
// the safety net the whole collect design rests on — without it, dropping the
// older mention from pending really would drop the question.
func TestHandleMention_DisplacedMentionArrivesViaTheGapSync(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	var calls atomic.Int32
	release := make(chan struct{})

	// Discord: nothing on the initial sync, both later mentions on the gap
	// sync, so anything found in the second prompt got there via the delta.
	gap := `[{"id":"10","content":"<@app123> and this","author":{"id":"u2","username":"budi"}},
	         {"id":"11","content":"<@app123> and that","author":{"id":"u2","username":"budi"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if strings.Contains(r.URL.RawQuery, "after=") {
				_, _ = w.Write([]byte(gap))
				return
			}
			_, _ = w.Write([]byte("[]"))
		}
	}))
	t.Cleanup(srv.Close)
	orig := discord.DiscordAPIBase
	discord.DiscordAPIBase = srv.URL
	t.Cleanup(func() { discord.DiscordAPIBase = orig })

	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if calls.Add(1) == 1 {
			<-release
		}
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	s.HandleMention(mentionMsg())
	waitFor(t, func() bool { return calls.Load() == 1 })

	sess := s.sessionFor("chan-mention")
	for _, m := range []struct{ id, text string }{{"10", "and this"}, {"11", "and that"}} {
		msg := mentionMsg()
		msg.ID = m.id
		msg.Content = "<@app123> " + m.text
		msg.Author = discord.User{ID: "u2", Username: "budi"}
		s.HandleMention(msg)
		waitForPending(t, sess, m.id)
	}

	close(release)
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("want 2 turns, got %d", len(bodies))
	}
	if strings.Contains(bodies[0], "and this") {
		t.Fatal("setup wrong: the displaced mention must not be in the first prompt")
	}
	// The delta keeps the raw channel text, mention token and all; only the
	// current question is stripped.
	if !strings.Contains(bodies[1], "and this") {
		t.Error("displaced mention missing from the follow-up prompt")
	}
	if !strings.Contains(bodies[1], "budi: and that") {
		t.Error("waiting mention missing as the follow-up question")
	}
}

// The hand-off happens on turn exit, not on commit: a turn that fails still
// has to collect what arrived while it was failing, or those mentions are
// answered by nobody.
func TestHandleMention_CollectedMentionRunsAfterAFailedTurn(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")

	var calls atomic.Int32
	release := make(chan struct{})
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-release
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	s.HandleMention(mentionMsg())
	waitFor(t, func() bool { return calls.Load() == 1 })

	waiting := mentionMsg()
	waiting.ID = "10"
	s.HandleMention(waiting)
	waitForPending(t, s.sessionFor("chan-mention"), "10")

	close(release)
	if !s.Wait(5 * time.Second) {
		t.Fatal("background task did not finish")
	}

	if len(replies) != 2 {
		t.Fatalf("want the failed turn answered and the waiting one served, got %v", replies)
	}
	if !strings.Contains(replies[0], "error bro") {
		t.Errorf("want the first turn to report its failure, got %q", replies[0])
	}
	if replies[1] != "ok" {
		t.Errorf("want the collected mention answered, got %q", replies[1])
	}
}

func TestAnswer_LLMErrorLeavesSessionUnchanged(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	msg := mentionMsg()
	if got := s.answer(msg, "hi"); !strings.Contains(got, "error bro, katanya") {
		t.Errorf("want error reply, got %q", got)
	}
	sess := s.sessionFor(msg.ChannelID)
	if len(sess.units) != 0 {
		t.Errorf("want nothing committed on error, got %d units", len(sess.units))
	}
	if sess.synced || sess.lastSeenID != "" {
		t.Error("want sync watermark unchanged on error, so the delta is refetched")
	}
}

func TestAnswer_EmptyReply(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})

	if got := s.answer(mentionMsg(), "hi"); got != noAnswerReply {
		t.Errorf("want %q, got %q", noAnswerReply, got)
	}
}

func TestAnswer_ToolLoopCommitsWholeUnit(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")

	call := 0
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",
				"tool_calls":[{"id":"c1","type":"function","function":{"name":"current_time","arguments":""}}]}}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"tool_call_id":"c1"`) {
			t.Errorf("want tool result in second request, got %s", body)
		}
		_, _ = w.Write([]byte(finalReply("it is late, the sheep are asleep")))
	})

	msg := mentionMsg()
	if got := s.answer(msg, "what time is it"); !strings.Contains(got, "sheep are asleep") {
		t.Errorf("want final reply, got %q", got)
	}
	if call != 2 {
		t.Fatalf("want 2 LLM rounds, got %d", call)
	}

	units := s.sessionFor(msg.ChannelID).units
	if len(units) != 1 {
		t.Fatalf("want 1 committed unit, got %d", len(units))
	}
	// user + assistant(tool_calls) + tool result + final assistant
	roles := make([]string, 0, len(units[0]))
	for _, m := range units[0] {
		roles = append(roles, m.Role)
	}
	if got := strings.Join(roles, ","); got != "user,assistant,tool,assistant" {
		t.Errorf("want a full exchange committed, got %v", got)
	}
}

func TestAnswer_SecondTurnSeesHistory(t *testing.T) {
	var replies []string
	fakeDiscord(t, &replies, "")

	var lastReq string
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastReq = string(body)
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	first := mentionMsg()
	first.Author.Username = "alice"
	s.answer(first, "remember the picnic")

	second := mentionMsg()
	second.ID = "10"
	second.Author.Username = "bob"
	s.answer(second, "what did alice say?")

	if !strings.Contains(lastReq, "alice: remember the picnic") {
		t.Errorf("want first exchange in second request, got %s", lastReq)
	}
	if !strings.Contains(lastReq, "bob: what did alice say?") {
		t.Errorf("want speaker-prefixed current message, got %s", lastReq)
	}
}

func TestAnswer_InitialSyncSeedsFromChannel(t *testing.T) {
	var replies []string
	history := `[
		{"id":"3","content":"","author":{"id":"u2","username":"carol"}},
		{"id":"2","content":"beep","author":{"id":"other-bot","username":"clanker","bot":true}},
		{"id":"1","content":"hello there","author":{"id":"u1","username":"alice"}}
	]`
	fakeDiscord(t, &replies, history)

	var firstReq string
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		firstReq = string(body)
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	msg := mentionMsg()
	s.answer(msg, "hi")

	if !strings.Contains(firstReq, "alice: hello there") {
		t.Errorf("want channel history in prompt, got %s", firstReq)
	}
	if strings.Contains(firstReq, "clanker") || strings.Contains(firstReq, "beep") {
		t.Errorf("want other bots skipped, got %s", firstReq)
	}
	if got := s.sessionFor(msg.ChannelID).lastSeenID; got != "9" {
		t.Errorf("want watermark at the newest known message, got %q", got)
	}
}

// The gap: regular messages typed between turns must flow into the
// conversation, fetched via ?after=watermark on the next turn.
func TestAnswer_GapSyncSeesMessagesBetweenTurns(t *testing.T) {
	var lastReq string
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastReq = string(body)
		_, _ = w.Write([]byte(finalReply("ok")))
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			return
		}
		if r.URL.Query().Get("after") == "9" {
			_, _ = w.Write([]byte(`[{"id":"11","content":"picnic moved to sunday","author":{"id":"u3","username":"carol"}}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"5","content":"old chatter","author":{"id":"u1","username":"alice"}}]`))
	}))
	t.Cleanup(srv.Close)
	orig := discord.DiscordAPIBase
	discord.DiscordAPIBase = srv.URL
	t.Cleanup(func() { discord.DiscordAPIBase = orig })

	first := mentionMsg()
	s.answer(first, "hi")

	second := mentionMsg()
	second.ID = "12"
	s.answer(second, "when is the picnic?")

	if !strings.Contains(lastReq, "carol: picnic moved to sunday") {
		t.Errorf("want between-turn message in second prompt, got %s", lastReq)
	}
	if got := s.sessionFor(second.ChannelID).lastSeenID; got != "12" {
		t.Errorf("want watermark advanced to the newest message, got %q", got)
	}
}
