package chat

import (
	"testing"

	"bernard/llm"
)

func TestAssemblePrompt(t *testing.T) {
	history := []llm.Message{{Role: "user", Content: "alice: earlier"}}
	current := llm.Message{Role: "user", Content: "bob: now"}

	t.Run("empty memory leaves system prompt byte-identical", func(t *testing.T) {
		msgs := assemblePrompt("prompt", "", history, current)
		if len(msgs) != 3 {
			t.Fatalf("want [system, history, current], got %d messages", len(msgs))
		}
		if msgs[0].Role != "system" || msgs[0].Content != "prompt" {
			t.Errorf("want bare prompt as system, got %+v", msgs[0])
		}
		if msgs[2].Content != "bob: now" {
			t.Errorf("want current message last, got %+v", msgs[2])
		}
	})

	t.Run("memory concatenates onto system prompt", func(t *testing.T) {
		msgs := assemblePrompt("prompt", "# Memory\n- call darcien batman", history, current)
		if msgs[0].Content != "prompt\n\n# Memory\n- call darcien batman" {
			t.Errorf("want memory folded into system message, got %q", msgs[0].Content)
		}
	})
}
