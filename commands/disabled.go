package commands

// Disabled command definitions.
//
// These commands are not registered or handled. They are kept here to document
// the expected UX when they are reimplemented.
//
// To re-enable a command:
//  1. Add its definition to CommandDefinitions() in definitions.go
//  2. Implement the handler in its own file
//  3. Add it to the registry in registry.go

import "bernard/discord"

// remindDefinition is disabled because it depended on Deno.Kv queue which was
// removed during the Go migration. Needs a persistent job store to reimplement.
func remindDefinition() CommandDefinition {
	return CommandDefinition{
		Name:        "remind",
		Description: "Schedule a reminder",
		Options: []CommandOption{
			{
				Name:        "message",
				Description: "Reminder message",
				Type:        discord.OptionTypeString,
				Required:    true,
				MinLength:   intPtr(1),
			},
			{
				Name:        "who",
				Description: "Who to remind. Default to you",
				Type:        discord.OptionTypeUser,
			},
			{
				Name:        "days",
				Description: "Remind in how many days",
				Type:        discord.OptionTypeInteger,
				MinValue:    intPtr(0),
				MaxValue:    intPtr(366),
			},
			{
				Name:        "hours",
				Description: "Remind in how many hours",
				Type:        discord.OptionTypeInteger,
				MinValue:    intPtr(0),
				MaxValue:    intPtr(24 * 31),
			},
			{
				Name:        "minutes",
				Description: "Remind in how many minutes",
				Type:        discord.OptionTypeInteger,
				MinValue:    intPtr(0),
				MaxValue:    intPtr(60 * 24),
			},
		},
	}
}
