package commands

import "bernard/discord"

// CommandDefinition is the Discord slash command schema used for registration.
type CommandDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     []CommandOption `json:"options,omitempty"`
}

// CommandOption represents a command option or subcommand.
type CommandOption struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Type        discord.OptionType `json:"type"`
	Required    bool               `json:"required,omitempty"`
	MinValue    *int               `json:"min_value,omitempty"`
	MaxValue    *int               `json:"max_value,omitempty"`
	MinLength   *int               `json:"min_length,omitempty"`
	MaxLength   *int               `json:"max_length,omitempty"`
	Choices     []CommandChoice    `json:"choices,omitempty"`
	Options     []CommandOption    `json:"options,omitempty"`
}

// CommandChoice is a predefined value for a string option.
type CommandChoice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CommandDefinitions returns the schemas for all active commands.
// Used by the manage tool to register commands with Discord.
func CommandDefinitions() []CommandDefinition {
	return []CommandDefinition{
		rollDefinition(),
		e25nDefinition(),
		httpDefinition(),
		workaholicDefinition(),
		chatDefinition(),
	}
}

func chatDefinition() CommandDefinition {
	return CommandDefinition{
		Name:        "chat",
		Description: "Chat with me",
		Options: []CommandOption{
			{
				Name:        "message",
				Description: "Your message for me",
				Type:        discord.OptionTypeString,
				Required:    true,
				MinLength:   intPtr(1),
				MaxLength:   intPtr(2000),
			},
		},
	}
}

func intPtr(v int) *int { return &v }

func rollDefinition() CommandDefinition {
	return CommandDefinition{
		Name:        "roll",
		Description: "Roll a n-sided dice m amount of time(s)",
		Options: []CommandOption{
			{
				Name:        "sides",
				Description: "The side count of the dice. Default to 6",
				Type:        discord.OptionTypeInteger,
				MinValue:    intPtr(1),
				MaxValue:    intPtr(100),
			},
			{
				Name:        "quantity",
				Description: "The amount of roll. Default to 1",
				Type:        discord.OptionTypeInteger,
				MinValue:    intPtr(1),
				MaxValue:    intPtr(200),
			},
		},
	}
}

func e25nDefinition() CommandDefinition {
	return CommandDefinition{
		Name:        "e25n",
		Description: "Determine what the English Numerical Contraction is for an English word or phrase",
		Options: []CommandOption{
			{
				Name:        "input",
				Description: "The input word or phrase. Min 3 characters.",
				Type:        discord.OptionTypeString,
				Required:    true,
				MinLength:   intPtr(3),
				MaxLength:   intPtr(1024),
			},
			{
				Name:        "verbose",
				Description: "Give verbose output including collisions. Default: false.",
				Type:        discord.OptionTypeBoolean,
			},
		},
	}
}

func httpDefinition() CommandDefinition {
	// Choices mirror httpVariants in http.go (sorted alphabetically).
	choices := make([]CommandChoice, len(httpVariants))
	for i, v := range httpVariants {
		choices[i] = CommandChoice{Name: v.label, Value: v.label}
	}

	return CommandDefinition{
		Name:        "http",
		Description: "Explain a HTTP status code with a thousand words",
		Options: []CommandOption{
			{
				Name:        "status_code",
				Description: "Your HTTP response status codes",
				Type:        discord.OptionTypeInteger,
				Required:    true,
				MinValue:    intPtr(100),
				MaxValue:    intPtr(599),
			},
			{
				Name:        "variant",
				Description: "Determine the variant of the explanation. Default to cat",
				Type:        discord.OptionTypeString,
				Choices:     choices,
			},
		},
	}
}

func workaholicDefinition() CommandDefinition {
	return CommandDefinition{
		Name:        "workaholic",
		Description: "Add or check workaholic points",
		Options: []CommandOption{
			{
				Name:        "add",
				Description: "Add workaholic point for yourself",
				Type:        discord.OptionTypeSubCommand,
				Options: []CommandOption{
					{
						Name:        "what",
						Description: "What did you do?",
						Type:        discord.OptionTypeString,
						Required:    true,
					},
					{
						Name:        "when",
						Description: "When did you do it?",
						Type:        discord.OptionTypeString,
						Required:    true,
					},
					{
						Name:        "duration",
						Description: "How long did you do it (in hours)?",
						Type:        discord.OptionTypeInteger,
						Required:    true,
					},
					{
						Name:        "type",
						Description: "What's the workaholic type? Default to OT",
						Type:        discord.OptionTypeString,
						Choices: []CommandChoice{
							{Name: "OT", Value: "OT"},
							{Name: "PH", Value: "PH"},
						},
					},
				},
			},
			{
				Name:        "check",
				Description: "Check scoreboard",
				Type:        discord.OptionTypeSubCommand,
				Options: []CommandOption{
					{
						Name:        "who",
						Description: "For who? Default to everyone.",
						Type:        discord.OptionTypeUser,
					},
					{
						Name:        "when",
						Description: "(NOT IMPLEMENTED) For when? Default to current month.",
						Type:        discord.OptionTypeString,
					},
				},
			},
		},
	}
}
