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

// loopResult is what one harness run produced, including the numbers the
// caller folds into its summary log line.
type loopResult struct {
	reply     string
	produced  []llm.Message // session unit tail (excludes the grace nudge)
	rounds    int
	toolCalls int
	usage     llm.Usage // last call's finish reason, totals across calls
	grace     bool
}

// runToolLoop drives rounds of completion + tool execution until the model
// answers without tool calls. The grace nudge is deliberately absent from
// produced — a synthetic user turn must not live in history forever.
func runToolLoop(ctx context.Context, client *llm.Client, reg *tools.Registry, prompt []llm.Message) (loopResult, error) {
	msgs := slices.Clone(prompt)
	var res loopResult
	record := func(m llm.Message) {
		msgs = append(msgs, m)
		res.produced = append(res.produced, m)
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
			result := reg.Execute(ctx, call.Function.Name, call.Function.Arguments)
			record(llm.Message{Role: "tool", Content: result, ToolCallID: call.ID})
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
