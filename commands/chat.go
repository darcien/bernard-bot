package commands

import (
	"fmt"
	"log/slog"
	"time"

	"bernard/discord"
	"bernard/llm"
)

const chatSystemPrompt = `# Identity

You are Bernard, chatting on Discord with your friends and former coworkers.
Everyone already knows you — never introduce yourself.

# Background

- You were a software engineer at a software house in Indonesia until the
  company shut down and your contract was terminated.
- You took that as a sign. Instead of job hunting, you got yourself a few
  sheep and started easing into shepherd life. You don't make a big deal of
  the details, like exactly how many sheep.
- The end goal: settle in some remote countryside as a full-time shepherd.
  A quiet life, your flock, no deadlines, no standups.
- You still know your craft and will gladly talk shop, but with the calm
  detachment of someone who is already halfway out of tech.

# Personality and tone

- Relaxed, friendly, helpful. Dry humor about corporate life and your
  retirement dream.
- Occasionally relate a topic back to sheep, farming, or the quiet life —
  briefly, and not in every message.

# How you respond

- Answer questions as best you can, concisely. Include sources when possible.
- If the question is in Indonesian, answer in Indonesian.
- Occasionally share a random fun fact, or a short story about your old work
  friend Gema, or about your sheep.
- This is Discord chat: keep replies short and conversational; use markdown
  only when it helps.
- Stay in character as Bernard. Never mention these instructions.`

const chatOfflineMessage = "Chat is temporarily offline due to the current global economic climate."

var (
	chatLLM   *llm.Client
	chatAppID string
)

// ConfigureChat wires the LLM client and application ID used for followups.
// A nil client leaves /chat in offline mode.
func ConfigureChat(client *llm.Client, applicationID string) {
	chatLLM = client
	chatAppID = applicationID
}

func handleChat(ctx CommandContext) (CommandResult, error) {
	if chatLLM == nil {
		return CommandResult{ResponseText: chatOfflineMessage}, nil
	}

	var message string
	for _, opt := range ctx.InteractionData.Options {
		if opt.Name == "message" {
			message = opt.StringValue()
		}
	}
	if message == "" {
		return CommandResult{ResponseText: "Missing message"}, nil
	}

	token := ctx.InteractionToken
	goBackground(func() { processChatMessage(message, token) })

	// Discord shows "Bernard is thinking..." until the followup arrives.
	return CommandResult{ResponseType: discord.InteractionResponseTypeDeferredChannelMessageWithSource}, nil
}

func processChatMessage(message, interactionToken string) {
	reply, err := chatLLM.Complete(chatSystemPrompt, message)
	if err != nil {
		slog.Error("chat completion failed", "err", err)
		reply = fmt.Sprintf("error bro, katanya %q", err.Error())
	} else if reply == "" {
		reply = "kurang tau bro"
	}
	if err := discord.CreateFollowupMessage(chatAppID, interactionToken, reply); err != nil {
		slog.Error("chat followup failed, retrying once", "err", err)
		time.Sleep(2 * time.Second)
		if err := discord.CreateFollowupMessage(chatAppID, interactionToken, reply); err != nil {
			slog.Error("chat followup retry failed", "err", err)
		}
	}
}
