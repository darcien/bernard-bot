package chat

import (
	"context"
	"fmt"
	"strings"

	"bernard/llm"
)

// The digest is tagged so the model can tell a summary of the conversation
// from the conversation, and reads as a user turn because that is the role
// the endpoint accepts between exchanges.
const (
	summaryTagOpen  = "<compaction-summary>"
	summaryTagClose = "</compaction-summary>"
)

// summarySystemPrompt is Reasonix's, re-cut for this domain. Theirs briefs a
// coding agent — files touched, commands run, edits applied — none of which
// exists here. What a chat harness has to carry across a fold is who said
// what, what was looked up and where, and what is still owed an answer.
//
// The rules at the end are theirs verbatim: terse, exact identifiers, and
// nothing invented. A summariser that embellishes is worse than no summary,
// because the fabrication outlives the transcript that would disprove it.
const summarySystemPrompt = `You are compacting the earlier part of a Discord conversation to save context.
Your summary replaces those messages; the recent exchanges are kept verbatim alongside it.
Write under these exact headings, omitting a heading only if it has no content:

## Standing facts
What people stated that still holds — names, preferences, constraints, corrections, and anything they asked to be remembered, in their own words.

## Topics
What was discussed and what was concluded, so a settled question is not reopened.

## Sources
Pages that were fetched and what they said, with their URLs exactly as given.

## Open threads
Questions asked but not answered, and anything someone is still waiting on.

Rules: be terse — bullet points and fragments, not prose. Preserve names, URLs, identifiers and numbers exactly. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing.`

// summariseRegion folds the region into one digest message, keeping small
// user turns verbatim in front of it: a fact someone stated survives the fold
// whatever the summariser does with it, which is Reasonix's kept-verbatim
// floor.
//
// A failed or empty summary degrades to a mechanical digest rather than
// aborting. Aborting would leave the session over the trigger and re-fire the
// fold every turn — the loop the stuck latch exists to catch, entered
// deliberately.
func summariseRegion(ctx context.Context, client *llm.Client, region [][]llm.Message, tokPerByte float64) []llm.Message {
	kept, fold := partitionFold(region, tokPerByte)
	summary, err := summarise(ctx, client, fold)
	if err != nil || strings.TrimSpace(summary) == "" {
		summary = mechanicalDigest(len(fold))
	}
	unit := append([]llm.Message{}, kept...)
	return append(unit, llm.Message{
		Role: "user",
		Content: summaryTagOpen + "\n" +
			"Summary of earlier conversation (older messages were compacted to save context):\n" +
			summary + "\n" + summaryTagClose,
	})
}

// partitionFold splits the region into what is kept verbatim — small user
// turns and any earlier digest, so a fold never re-summarises a summary and
// loses what it already captured — and what folds.
func partitionFold(region [][]llm.Message, tokPerByte float64) (kept []llm.Message, fold []llm.Message) {
	for _, unit := range region {
		for _, m := range unit {
			pinned := m.Role == "user" &&
				(strings.HasPrefix(m.Content, summaryTagOpen) ||
					int(float64(unitSize([]llm.Message{m}))*tokPerByte) <= pinnedUserTokens)
			if pinned {
				kept = append(kept, m)
				continue
			}
			fold = append(fold, m)
		}
	}
	return kept, fold
}

// summarise asks the model for the digest, on its own bounded context: the
// turn's deadline belongs to the answer the user is waiting for, and this
// runs after they have it.
func summarise(ctx context.Context, client *llm.Client, fold []llm.Message) (string, error) {
	if len(fold) == 0 {
		return "", fmt.Errorf("nothing to fold")
	}
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()

	msgs := []llm.Message{
		{Role: "system", Content: summarySystemPrompt},
		{Role: "user", Content: renderTranscript(fold)},
	}
	m, _, err := client.Chat(ctx, msgs, nil)
	if err != nil {
		return "", err
	}
	return m.Content, nil
}

// renderTranscript flattens the fold into something readable. Tool calls and
// their results become plain lines: the digest is prose, so the wire shapes
// that carry them are noise, and sending them as real tool messages would
// need a matching assistant call the summariser has no use for.
func renderTranscript(fold []llm.Message) string {
	var b strings.Builder
	for _, m := range fold {
		switch {
		case m.Role == "tool":
			fmt.Fprintf(&b, "[tool result]\n%s\n\n", m.Content)
		case len(m.ToolCalls) > 0:
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "[tool call] %s %s\n", tc.Function.Name, tc.Function.Arguments)
			}
			if m.Content != "" {
				fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
			}
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "%s: %s\n\n", m.Role, m.Content)
		}
	}
	return b.String()
}

// mechanicalDigest stands in when the summariser fails. It says what was lost
// and how to recover it — the channel still holds every message — rather than
// pretending the conversation started here.
func mechanicalDigest(messages int) string {
	return fmt.Sprintf("%d earlier message(s) were folded here to free context, but the summary was unavailable. "+
		"Scroll back in the channel or ask if you need details from before this point.", messages)
}
