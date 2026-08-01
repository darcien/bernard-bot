package chat

import (
	"strings"
	"testing"

	"bernard/llm"
)

func textUnit(size int) []llm.Message {
	return []llm.Message{{Role: "user", Content: strings.Repeat("a", size)}}
}

func TestTrimUnits(t *testing.T) {
	t.Run("under budget stays untouched", func(t *testing.T) {
		units := [][]llm.Message{textUnit(10), textUnit(10)}
		if got := trimUnits(units, 100, 50); len(got) != 2 {
			t.Errorf("want 2 units, got %d", len(got))
		}
	})

	t.Run("over budget drops oldest until under floor", func(t *testing.T) {
		units := [][]llm.Message{textUnit(40), textUnit(40), textUnit(40)}
		got := trimUnits(units, 100, 50) // total 120+roles > 100
		if len(got) != 1 {
			t.Fatalf("want 1 unit left, got %d", len(got))
		}
		if got[0][0].Content != units[2][0].Content {
			t.Error("want newest unit kept, oldest dropped")
		}
	})

	t.Run("newest unit survives even alone over the floor", func(t *testing.T) {
		units := [][]llm.Message{textUnit(10), textUnit(500)}
		got := trimUnits(units, 100, 50)
		if len(got) != 1 || len(got[0][0].Content) != 500 {
			t.Errorf("want only the oversized newest unit kept, got %d units", len(got))
		}
	})
}

func TestUnitSize_CountsToolCallArguments(t *testing.T) {
	unit := []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://example.com"}`},
		}},
	}}
	if got := unitSize(unit); got < len(`{"url":"https://example.com"}`) {
		t.Errorf("want arguments counted in size, got %d", got)
	}
}
