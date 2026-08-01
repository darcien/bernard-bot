package chat

import (
	"context"
	"fmt"
	"slices"
	"time"

	"bernard/llm"
	"bernard/tools"
)

const graceNudge = "No more tool calls available. Answer with what you have."

// steerPrefix marks a message that arrived mid-turn. Without it the model
// cannot tell an interjection from a fresh question and may abandon the tool
// sequence it is halfway through. The prefix is transport only: the session
// keeps the plain message, so history reads as ordinary conversation.
const steerPrefix = "[Sent while you were working. This is extra guidance for " +
	"the question you are already answering, not a new task — finish the " +
	"current step, then take it into account.]"

// steerFunc reports a message that arrived mid-turn, if any. It returns false
// when nothing is waiting. The loop knows nothing about Discord or sessions;
// the caller decides what counts as a mid-turn message.
type steerFunc func() (llm.Message, bool)

// loopResult is what one harness run produced, including the numbers the
// caller folds into its summary log line.
type loopResult struct {
	reply     string
	produced  []llm.Message // session unit tail (excludes the grace nudge)
	rounds    int
	toolCalls int
	steers    int       // mid-turn messages folded into this run
	usage     llm.Usage // last call's finish reason, totals across calls
	grace     bool
	// sources are the places tool output came from, in citation order:
	// sources[0] is "[1]". Recorded by the harness, never by the model, so
	// a citation can't name a page that was never fetched.
	sources []string
}

// cite returns the citation number for a source, reusing the number if the
// same place was consulted twice in one turn.
func (r *loopResult) cite(source string) int {
	if i := slices.Index(r.sources, source); i >= 0 {
		return i + 1
	}
	r.sources = append(r.sources, source)
	return len(r.sources)
}

// runToolLoop drives rounds of completion + tool execution until the model
// answers without tool calls. The grace nudge is deliberately absent from
// produced — a synthetic user turn must not live in history forever.
func runToolLoop(ctx context.Context, client *llm.Client, reg *tools.Registry, prompt []llm.Message, steer steerFunc) (loopResult, error) {
	msgs := slices.Clone(prompt)
	var res loopResult
	record := func(m llm.Message) {
		msgs = append(msgs, m)
		res.produced = append(res.produced, m)
	}
	// A steered message goes to the model marked and to history plain, the
	// same split the grace nudge uses for the opposite reason.
	recordSteer := func(m llm.Message) {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: steerPrefix + "\n" + m.Content})
		res.produced = append(res.produced, m)
		res.steers++
	}
	accrue := func(u llm.Usage) {
		res.usage.FinishReason = u.FinishReason
		res.usage.PromptTokens += u.PromptTokens
		res.usage.CompletionTokens += u.CompletionTokens
		res.usage.CacheHitTokens += u.CacheHitTokens
	}

	for range maxToolRounds {
		res.rounds++
		m, usage, err := client.Chat(ctx, msgs, reg.Schemas())
		if err != nil {
			return loopResult{}, err
		}
		accrue(usage)
		record(m)
		if len(m.ToolCalls) == 0 {
			res.reply = m.Content
			return res, nil
		}
		for _, call := range m.ToolCalls {
			res.toolCalls++
			result, source := reg.Execute(ctx, call.Function.Name, call.Function.Arguments)
			if source != "" {
				// Hand the model the citation number along with the
				// content, so it can cite without inventing a URL.
				result = fmt.Sprintf("[%d] source: %s\n\n%s", res.cite(source), source, result)
			}
			record(llm.Message{Role: "tool", Content: result, ToolCallID: call.ID})
		}

		// Fold in anything said mid-turn, now that every tool call in this
		// round has its result: injecting here keeps the call/result pairing
		// intact, and the message rides the next round at no extra cost.
		if steer != nil {
			if m, ok := steer(); ok {
				recordSteer(m)
			}
		}
	}

	// Grace round: tools stripped, one last chance to answer from work done.
	// Skipped when the deadline can't fit another call — degrade to error.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < graceMinRemaining {
		return loopResult{}, fmt.Errorf("out of time after %d tool rounds", maxToolRounds)
	}
	res.grace = true
	res.rounds++
	m, usage, err := client.Chat(ctx, append(msgs, llm.Message{Role: "user", Content: graceNudge}), nil)
	if err != nil {
		return loopResult{}, err
	}
	accrue(usage)
	res.produced = append(res.produced, m)
	res.reply = m.Content
	return res, nil
}
