package commands

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"bernard/discord"
)

func TestHandleRoll(t *testing.T) {
	tests := []struct {
		name      string
		options   []discord.InteractionDataOption
		wantCount int
		wantMin   int
		wantMax   int
	}{
		{
			name:      "defaults (1d6)",
			options:   nil,
			wantCount: 1,
			wantMin:   1,
			wantMax:   6,
		},
		{
			name: "d1 always produces 1",
			options: []discord.InteractionDataOption{
				intOpt("sides", 1),
				intOpt("quantity", 1),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   1,
		},
		{
			name: "3d6",
			options: []discord.InteractionDataOption{
				intOpt("sides", 6),
				intOpt("quantity", 3),
			},
			wantCount: 3,
			wantMin:   1,
			wantMax:   6,
		},
		{
			name: "1d100",
			options: []discord.InteractionDataOption{
				intOpt("sides", 100),
				intOpt("quantity", 1),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   100,
		},
		{
			name: "sides=0 falls back to default d6",
			options: []discord.InteractionDataOption{
				intOpt("sides", 0),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   6,
		},
		{
			name: "quantity=0 falls back to default 1",
			options: []discord.InteractionDataOption{
				intOpt("quantity", 0),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   6,
		},
		{
			name: "negative sides falls back to default d6",
			options: []discord.InteractionDataOption{
				intOpt("sides", -5),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   6,
		},
		{
			name: "negative quantity falls back to default 1",
			options: []discord.InteractionDataOption{
				intOpt("quantity", -1),
			},
			wantCount: 1,
			wantMin:   1,
			wantMax:   6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := CommandContext{
				InteractionData: discord.ApplicationCommandData{
					Name:    "roll",
					Options: tc.options,
				},
			}

			result, err := handleRoll(ctx)
			if err != nil {
				t.Fatal(err)
			}

			parts := strings.Fields(result.ResponseText)
			if len(parts) != tc.wantCount {
				t.Fatalf("want %d result(s), got %d: %q", tc.wantCount, len(parts), result.ResponseText)
			}
			for _, p := range parts {
				n, err := strconv.Atoi(p)
				if err != nil {
					t.Fatalf("non-numeric result %q", p)
				}
				if n < tc.wantMin || n > tc.wantMax {
					t.Errorf("result %d out of range [%d, %d]", n, tc.wantMin, tc.wantMax)
				}
			}
		})
	}
}

func intOpt(name string, val int) discord.InteractionDataOption {
	v, _ := json.Marshal(val)
	return discord.InteractionDataOption{
		Name:  name,
		Type:  discord.OptionTypeInteger,
		Value: v,
	}
}
