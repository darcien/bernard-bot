package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

type fakeTool struct {
	name    string
	gotArgs string
	result  string
	err     error
}

func (f *fakeTool) Name() string            { return f.name }
func (f *fakeTool) Description() string     { return "fake" }
func (f *fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	f.gotArgs = string(args)
	return f.result, f.err
}

func TestRegistry_SchemasSortedAndStable(t *testing.T) {
	// Registered in reverse alphabetical order on purpose.
	r := NewRegistry(100, &fakeTool{name: "zebra"}, &fakeTool{name: "apple"})

	s1, s2 := r.Schemas(), r.Schemas()
	if &s1[0] != &s2[0] {
		t.Error("want same underlying bytes on every call, got fresh marshal")
	}
	if strings.Index(string(s1), "apple") > strings.Index(string(s1), "zebra") {
		t.Errorf("want names sorted alphabetically, got %s", s1)
	}
}

func TestRegistry_DuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want panic on duplicate tool name")
		}
	}()
	NewRegistry(100, &fakeTool{name: "dup"}, &fakeTool{name: "dup"})
}

func TestRegistry_Execute(t *testing.T) {
	boom := &fakeTool{name: "boom", err: errors.New("kaput")}
	noargs := &fakeTool{name: "noargs", result: "ok"}
	r := NewRegistry(100, boom, noargs)

	t.Run("unknown tool becomes error text", func(t *testing.T) {
		if got, _ := r.Execute(context.Background(), "nope", "{}"); got != "error: unknown tool nope" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("tool error becomes error text", func(t *testing.T) {
		if got, _ := r.Execute(context.Background(), "boom", "{}"); got != "error: kaput" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("empty args normalized to {} so tools can unmarshal", func(t *testing.T) {
		if got, _ := r.Execute(context.Background(), "noargs", ""); got != "ok" {
			t.Errorf("got %q", got)
		}
		if noargs.gotArgs != "{}" {
			t.Errorf("want tool to receive {}, got %q", noargs.gotArgs)
		}
	})
}

// A tool that implements Sourced reports where its output came from, but
// only when the call actually succeeded — a failed fetch cites nothing.
type sourcedTool struct{ fakeTool }

func (s *sourcedTool) Source(json.RawMessage) string { return "https://example.com/page" }

func TestRegistry_ExecuteReportsSource(t *testing.T) {
	ok := &sourcedTool{fakeTool{name: "fetch", result: "page text"}}
	broken := &sourcedTool{fakeTool{name: "broken", err: errors.New("404")}}
	plain := &fakeTool{name: "plain", result: "no source here"}
	r := NewRegistry(100, ok, broken, plain)

	if _, source := r.Execute(context.Background(), "fetch", "{}"); source != "https://example.com/page" {
		t.Errorf("want the tool's source, got %q", source)
	}
	if _, source := r.Execute(context.Background(), "broken", "{}"); source != "" {
		t.Errorf("want no source from a failed call, got %q", source)
	}
	if _, source := r.Execute(context.Background(), "plain", "{}"); source != "" {
		t.Errorf("want no source from a tool that isn't Sourced, got %q", source)
	}
}

func TestRegistry_ExecuteCapsResult(t *testing.T) {
	long := &fakeTool{name: "long", result: strings.Repeat("a", 50)}
	r := NewRegistry(10, long)

	got, _ := r.Execute(context.Background(), "long", "{}")
	if !strings.HasSuffix(got, "[truncated]") || !strings.HasPrefix(got, "aaaaaaaaaa") {
		t.Errorf("want capped result with marker, got %q", got)
	}
}

func TestTruncateRunes_MultibyteSafe(t *testing.T) {
	s := strings.Repeat("🐑", 20)
	got := TruncateRunes(s, 5)
	if !utf8.ValidString(got) {
		t.Error("truncation split a rune")
	}
	if !strings.HasPrefix(got, strings.Repeat("🐑", 5)) || !strings.Contains(got, "[truncated]") {
		t.Errorf("want 5 sheep + marker, got %q", got)
	}
	if got := TruncateRunes("short", 100); got != "short" {
		t.Errorf("want untouched short string, got %q", got)
	}
}

func TestTruncateHeadTail(t *testing.T) {
	s := "HEAD" + strings.Repeat("x", 1000) + "TAIL"

	t.Run("keeps both ends", func(t *testing.T) {
		got := TruncateHeadTail(s, 200)
		if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") {
			t.Errorf("want head and tail kept, got %q", got)
		}
		if !strings.Contains(got, "truncated") {
			t.Errorf("want marker, got %q", got)
		}
	})

	// The marker counts against the budget, so a second truncation
	// downstream is a no-op instead of chopping the tail again.
	t.Run("result fits the budget", func(t *testing.T) {
		for _, max := range []int{40, 100, 200, 999} {
			if n := utf8.RuneCountInString(TruncateHeadTail(s, max)); n > max {
				t.Errorf("max %d: got %d runes", max, n)
			}
		}
	})

	// Documents front-load: an aggregator's stories, an article's argument.
	t.Run("head gets most of the budget", func(t *testing.T) {
		got := TruncateHeadTail(s, 200)
		head, tail, ok := strings.Cut(got, truncationMarker)
		if !ok {
			t.Fatalf("want a marker separating head and tail, got %q", got)
		}
		if len(head) <= len(tail)*2 {
			t.Errorf("want a head-weighted split, got head=%d tail=%d", len(head), len(tail))
		}
	})

	t.Run("short input is untouched", func(t *testing.T) {
		if got := TruncateHeadTail("short", 100); got != "short" {
			t.Errorf("got %q", got)
		}
	})
}
