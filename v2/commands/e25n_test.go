package commands

import (
	"encoding/json"
	"testing"

	"bernard/discord"
)

func TestHandleE25n(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Classic e25n examples
		{"internationalization", "internationalization -- **i18n**"},
		{"kubernetes", "kubernetes -- **k8s**"},
		{"accessibility", "accessibility -- **a11y**"},
		// Spaces are stripped before contracting
		{"hello world", "hello world -- **h8d**"},
		// Leading/trailing spaces trimmed
		{"  hello  ", "  hello   -- **h3o**"},
		// 1-char after stripping: middle is 0, first==last
		{"a", "a -- **a0a**"},
		// 1-char after space stripping
		{" a ", " a  -- **a0a**"},
		// 2-char input: middle is 0, first!=last
		{"ab", "ab -- **a0b**"},
		// 2-char after space stripping: "a b" → "ab"
		{"a b", "a b -- **a0b**"},
		// 3-char input: middle has 1 char
		{"abc", "abc -- **a1c**"},
		// Case insensitive: result is lowercase
		{"HELLO", "HELLO -- **h3o**"},
		// Unicode: rune-based counting
		{"ünïcödé", "ünïcödé -- **ü5é**"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			v, _ := json.Marshal(tc.input)
			ctx := CommandContext{
				InteractionData: discord.ApplicationCommandData{
					Name: "e25n",
					Options: []discord.InteractionDataOption{
						{Name: "input", Type: discord.OptionTypeString, Value: v},
					},
				},
			}

			result, err := handleE25n(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResponseText != tc.want {
				t.Errorf("want %q, got %q", tc.want, result.ResponseText)
			}
		})
	}
}

func TestHandleE25n_MissingInput(t *testing.T) {
	ctx := CommandContext{
		InteractionData: discord.ApplicationCommandData{
			Name:    "e25n",
			Options: nil,
		},
	}
	result, err := handleE25n(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseText != "Missing input word/phrase" {
		t.Errorf("unexpected response: %q", result.ResponseText)
	}
}

func TestHandleE25n_WhitespaceOnly(t *testing.T) {
	for _, input := range []string{"   ", "\t", "  \t  "} {
		t.Run(input, func(t *testing.T) {
			v, _ := json.Marshal(input)
			ctx := CommandContext{
				InteractionData: discord.ApplicationCommandData{
					Name: "e25n",
					Options: []discord.InteractionDataOption{
						{Name: "input", Type: discord.OptionTypeString, Value: v},
					},
				},
			}
			result, err := handleE25n(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResponseText != "Missing input word/phrase" {
				t.Errorf("want missing input message, got %q", result.ResponseText)
			}
		})
	}
}
