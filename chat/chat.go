// Package chat is Bernard's conversation: channel-scoped history, a
// tool-calling loop over an LLM, and the @mention entry point that drives
// them. State lives in memory; the Discord channel is the durable backup.
package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"bernard/discord"
	"bernard/llm"
	"bernard/tools"
)

// Service owns everything one bot instance needs to chat: the LLM client,
// the tool registry, and per-channel conversation state. A nil llm client
// leaves chat offline.
type Service struct {
	llm   *llm.Client
	tools *tools.Registry
	botID string // application ID; the bot's own user ID on messages

	// admission bounds turns in flight across every channel; see admission.go.
	admission *admission

	mu       sync.Mutex
	sessions map[string]*session

	// background tracks in-flight replies so shutdown can wait for them —
	// the work outlives the gateway event that started it.
	background sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(client *llm.Client, botID string) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		llm:   client,
		botID: botID,
		tools: tools.NewRegistry(toolResultCap,
			tools.CurrentTime{},
			tools.NewWebFetch(webFetchTimeout, toolResultCap),
		),
		admission: newAdmission(maxConcurrentTurns),
		sessions:  make(map[string]*session),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// ToolNames lists the registered tools, for the startup log line.
func (s *Service) ToolNames() []string { return s.tools.Names() }

// Wait blocks until in-flight replies finish or timeout elapses.
// Returns false on timeout.
func (s *Service) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Cancel aborts in-flight replies so users get a "restarting" note instead
// of an eternal typing indicator. One-way: only call on the way down.
func (s *Service) Cancel() { s.cancel() }

// HandleMention processes a gateway MESSAGE_CREATE event: when the message
// @mentions the bot, reply in the channel. Returns whether the message was
// accepted, mostly for tests.
//
// Called from the gateway read loop, so it only filters synchronously and
// does the work in the background.
func (s *Service) HandleMention(msg discord.Message) bool {
	if msg.ChannelID == "" || msg.Author.ID == s.botID || msg.Author.Bot || msg.WebhookID != "" {
		return false // own replies, other bots, webhooks
	}
	if !slices.ContainsFunc(msg.Mentions, func(u discord.User) bool { return u.ID == s.botID }) {
		return false
	}

	question := stripMention(msg.Content, s.botID)
	s.goBackground(func() {
		switch {
		case s.llm == nil:
			s.reply(msg.ChannelID, offlineReply)
		case question == "":
			s.reply(msg.ChannelID, emptyAskReply)
		default:
			s.serve(msg, question)
		}
	})
	return true
}

// serve answers a mention and every mention that arrives while it works.
//
// Only one turn per channel ever runs: a mention landing mid-turn is recorded
// and collected here on exit, so N impatient mentions cost one follow-up turn
// rather than N turns. The collected turn re-syncs the channel, so the
// mentions it did not take as the current question are still in its prompt as
// ordinary delta — Discord is the queue, and this is only the bit that says
// something is waiting in it.
//
// A mention with an empty question never reaches here, so a waiting mention
// always has one.
func (s *Service) serve(msg discord.Message, question string) {
	sess := s.sessionFor(msg.ChannelID)
	if !sess.claim(msg) {
		slog.Debug("chat mention queued", "channel", msg.ChannelID, "user", msg.Author.Username)
		return
	}
	for {
		s.reply(msg.ChannelID, s.runTurn(msg, question))
		next, ok := sess.finish()
		if !ok {
			return
		}
		msg, question = next, stripMention(next.Content, s.botID)
	}
}

// runTurn answers one mention under admission. The typing indicator
// starts first, so a mention waiting for a slot still shows the bot is alive,
// and the slot is taken before answer starts its own clock, so queueing is
// never charged to the turn's reply budget.
//
// A refused turn never reaches logAnswer, so both refusals log here — that
// line is the only record they leave.
func (s *Service) runTurn(msg discord.Message, question string) string {
	stopTyping := keepTyping(s.ctx, msg.ChannelID)
	defer stopTyping()

	if err := s.admission.enter(s.ctx, admissionWait); err != nil {
		if errors.Is(err, errBusy) {
			slog.Warn("chat too busy", "channel", msg.ChannelID, "waited", admissionWait)
			return busyReply
		}
		slog.Debug("chat admission aborted", "channel", msg.ChannelID, "err", err)
		return restartingReply
	}
	defer s.admission.leave()

	return s.answer(msg, question)
}

// answer is the harness run for one question: sync the channel delta, run
// the tool loop, commit on success. Returns the reply text.
func (s *Service) answer(msg discord.Message, question string) string {
	start := time.Now()
	sess := s.sessionFor(msg.ChannelID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// The triggering message is usually in the fetch too; it enters the
	// prompt as the current turn instead.
	delta, seenID := s.syncChannel(sess, msg.ChannelID, msg.ID)

	current := userMessage(msg.Author.Username, question)
	ctx, cancel := context.WithTimeout(s.ctx, turnTimeout)
	defer cancel()

	// Mentions arriving mid-turn are folded into this run instead of costing
	// a follow-up turn. The loop takes a plain function, so it stays free of
	// Discord and session concepts; the IDs are kept here because only the
	// commit below knows what to do with them.
	var steered []discord.Message
	steer := func() (llm.Message, bool) {
		next, ok := sess.steer()
		if !ok {
			return llm.Message{}, false
		}
		// Tracked either way: the commit folds these into the watermark and
		// the error path hands them back.
		steered = append(steered, next)
		if snowflake(next.ID) <= snowflake(seenID) {
			// Already swept into this turn's delta by the sync above — it
			// queued before the sync ran. Folding it in again would put the
			// same question in the prompt twice.
			return llm.Message{}, false
		}
		return userMessage(next.Author.Username, stripMention(next.Content, s.botID)), true
	}

	prompt := buildContext(systemPrompt, "", append(sess.history(), delta...), current)
	res, err := runToolLoop(ctx, s.llm, s.tools, prompt, steer)
	reply := res.reply
	cited := 0
	switch {
	case errors.Is(err, context.Canceled):
		reply = restartingReply
	case errors.Is(err, context.DeadlineExceeded):
		reply = timeoutReply
	case err != nil:
		reply = fmt.Sprintf("error bro, katanya %q", err.Error())
	default:
		if reply == "" {
			reply = noAnswerReply
		}
		// Sources are rendered from the harness's record, not from whatever
		// URL the model may have typed. The session keeps the bare reply —
		// the footer is presentation, and repeating it in history would
		// teach the model to write footers itself.
		reply, cited = withSources(reply, res.sources)
		// Commit only on success: an unanswered question in history would
		// get re-answered next time, and a failed round could leave a
		// dangling tool_call pair (DeepSeek 400s on those). The channel
		// delta and its watermark commit with the exchange, so a failed
		// run refetches the same delta.
		unit := append(delta, current)
		sess.append(append(unit, res.produced...))
		sess.synced = true
		sess.lastSeenID = maxSnowflake(seenID, msg.ID)
		// Steered mentions were answered by this turn, so the watermark has
		// to cover them too — otherwise the next gap sync refetches them and
		// the model reads the same question twice.
		for _, sm := range steered {
			sess.lastSeenID = maxSnowflake(sess.lastSeenID, sm.ID)
		}
	}
	if err != nil {
		// Nothing committed, so a mention this turn absorbed is now owed an
		// answer by no one. Put it back for the follow-up turn.
		for _, sm := range steered {
			sess.requeue(sm)
		}
	}

	s.logAnswer(msg, sess, res, reply, len(delta), cited, time.Since(start), err)
	return reply
}

// logAnswer emits the one line per conversation turn. Fields that only
// describe the happy path are omitted — their presence is the signal.
func (s *Service) logAnswer(msg discord.Message, sess *session, res loopResult, reply string, synced, cited int, dur time.Duration, err error) {
	attrs := []any{
		"channel", msg.ChannelID,
		"user", msg.Author.Username,
		"history", len(sess.units),
		"rounds", res.rounds,
		"prompt_tokens", res.usage.PromptTokens,
		"cache_hit_tokens", res.usage.CacheHitTokens,
		"completion_tokens", res.usage.CompletionTokens,
		"reply_chars", len(reply),
		"dur", dur.Round(time.Millisecond),
	}
	if synced > 0 {
		attrs = append(attrs, "synced", synced)
	}
	if res.toolCalls > 0 {
		attrs = append(attrs, "tools", res.toolCalls)
	}
	if res.steers > 0 {
		attrs = append(attrs, "steers", res.steers)
	}
	if len(res.sources) > 0 {
		// cited < sources means the model ignored its citation markers and
		// the footer fell back to listing everything.
		attrs = append(attrs, "sources", len(res.sources), "cited", cited)
	}
	if res.grace {
		attrs = append(attrs, "grace", true)
	}
	if fr := res.usage.FinishReason; fr != "" && fr != "stop" {
		attrs = append(attrs, "finish", fr) // "length" means a cut-off reply
	}
	if err != nil {
		slog.Error("chat failed", append(attrs, "err", err)...)
		return
	}
	slog.Info("chat done", attrs...)
}

// syncChannel fetches channel messages the session doesn't represent yet:
// recent history on the first sync (restart context), messages sent between
// turns afterwards. excludeID drops the triggering message. Best effort — a
// failed fetch just means answering without the gap.
func (s *Service) syncChannel(sess *session, channelID, excludeID string) ([]llm.Message, string) {
	var msgs []discord.Message
	var err error
	if sess.lastSeenID == "" {
		msgs, err = discord.GetMessagesFromChannel(channelID, initialSyncFetch)
	} else {
		msgs, err = discord.GetMessagesAfter(channelID, sess.lastSeenID, gapSyncFetch)
	}
	if err != nil {
		slog.Warn("chat channel sync failed", "channel", channelID, "err", err)
		return nil, sess.lastSeenID
	}
	delta := channelDelta(msgs, s.botID, !sess.synced, excludeID)
	seenID := newestMessageID(msgs, sess.lastSeenID)
	// fetched=0 → REST/watermark problem; fetched>0 delta=0 → filtering
	// problem; delta>0 → the messages made it into the prompt.
	slog.Debug("chat sync",
		"channel", channelID,
		"initial", !sess.synced,
		"after", sess.lastSeenID,
		"fetched", len(msgs),
		"delta", len(delta),
		"watermark", seenID)
	return delta, seenID
}

func (s *Service) sessionFor(channelID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[channelID]
	if !ok {
		sess = &session{}
		s.sessions[channelID] = sess
	}
	return sess
}

func (s *Service) goBackground(fn func()) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		fn()
	}()
}

func (s *Service) reply(channelID, content string) {
	if err := discord.CreateMessage(channelID, content); err != nil {
		slog.Error("chat reply failed, retrying once", "err", err)
		time.Sleep(2 * time.Second)
		if err := discord.CreateMessage(channelID, content); err != nil {
			slog.Error("chat reply retry failed", "err", err)
		}
	}
}

// stripMention removes the bot's mention tokens (<@id> and <@!id>) and
// trims what remains.
func stripMention(content, botID string) string {
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return strings.TrimSpace(content)
}
