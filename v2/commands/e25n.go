package commands

import (
	"fmt"
	"strings"
)

func handleE25n(ctx CommandContext) (CommandResult, error) {
	var input string
	for _, opt := range ctx.InteractionData.Options {
		if opt.Name == "input" {
			input = opt.StringValue()
		}
	}

	if input == "" {
		return CommandResult{ResponseText: "Missing input word/phrase"}, nil
	}

	// Custom contraction implementation — matches the TS fallback used while
	// the e25n module import is broken (https://github.com/denoland/deno/issues/17784).
	chars := []rune(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input), " ", "")))
	if len(chars) == 0 {
		return CommandResult{ResponseText: "Missing input word/phrase"}, nil
	}
	first := string(chars[0])
	last := string(chars[len(chars)-1])
	middleLen := 0
	if len(chars) > 2 {
		middleLen = len(chars) - 2
	}
	newWord := fmt.Sprintf("%s%d%s", first, middleLen, last)

	return CommandResult{ResponseText: fmt.Sprintf("%s -- **%s**", input, newWord)}, nil
}
