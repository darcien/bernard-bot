package commands

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

const (
	rollDefaultSides    = 6
	rollDefaultQuantity = 1
)

func handleRoll(ctx CommandContext) (CommandResult, error) {
	sides := rollDefaultSides
	quantity := rollDefaultQuantity

	for _, opt := range ctx.InteractionData.Options {
		switch opt.Name {
		case "sides":
			if v := opt.IntValue(); v >= 1 {
				sides = v
			}
		case "quantity":
			if v := opt.IntValue(); v >= 1 {
				quantity = v
			}
		}
	}

	results := make([]string, quantity)
	for i := range results {
		results[i] = fmt.Sprintf("%d", rand.IntN(sides)+1)
	}

	return CommandResult{ResponseText: strings.Join(results, " ")}, nil
}
