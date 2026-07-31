package commands

import "bernard/discord"

type CommandContext struct {
	InteractionData  discord.ApplicationCommandData
	User             discord.User
	ChannelID        string
	GuildID          string
	InteractionID    string
	InteractionToken string
}

type CommandResult struct {
	ResponseText string
	ResponseType discord.InteractionResponseType // 0 = default (ChannelMessageWithSource)
}

type Handler func(ctx CommandContext) (CommandResult, error)

// registry maps command names to handlers.
var registry = map[string]Handler{
	"roll":       handleRoll,
	"e25n":       handleE25n,
	"http":       handleHTTP,
	"workaholic": handleWorkaholic,
	"chat":       handleChat,
}

// Dispatch looks up and calls the handler for the given command.
// Returns (result, false, nil) if command is not found.
func Dispatch(ctx CommandContext) (CommandResult, bool, error) {
	h, ok := registry[ctx.InteractionData.Name]
	if !ok {
		return CommandResult{}, false, nil
	}
	result, err := h(ctx)
	return result, true, err
}
