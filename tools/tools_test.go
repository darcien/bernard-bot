package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
func (f *fakeTool) ReadOnly() bool          { return true }
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
	long := &fakeTool{name: "long", result: strings.Repeat("a", 5000)}
	r := NewRegistry(500, long)

	got, _ := r.Execute(context.Background(), "long", "{}")
	ceiling := 500 + len(fmt.Sprintf(truncationMarker, 5000, 5000)) + 6
	if len(got) > ceiling {
		t.Errorf("want the cap respected, got %d bytes, want at most %d", len(got), ceiling)
	}
	if !strings.Contains(got, "truncated 4") || !strings.Contains(got, "of 5000 bytes") {
		t.Errorf("want a sized marker, got %q", got)
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

	// Marker appended after the cut, not reserved inside it, so the result
	// runs over by its width plus outward snapping. See TruncateHeadTail.
	t.Run("result fits the budget plus the marker", func(t *testing.T) {
		for _, max := range []int{40, 100, 200, 999} {
			ceiling := max + len(fmt.Sprintf(truncationMarker, len(s), len(s))) + 6
			if n := len(TruncateHeadTail(s, max)); n > ceiling {
				t.Errorf("max %d: got %d bytes, want at most %d", max, n, ceiling)
			}
		}
	})

	// The model needs the magnitude: losing 5% and losing 95% are different
	// answers, and identical without the numbers.
	t.Run("marker carries the sizes", func(t *testing.T) {
		got := TruncateHeadTail(s, 200)
		re := regexp.MustCompile(`truncated (\d+) of (\d+) bytes`)
		m := re.FindStringSubmatch(got)
		if m == nil {
			t.Fatalf("want elided and total bytes in the marker, got %q", got)
		}
		if m[2] != strconv.Itoa(len(s)) {
			t.Errorf("want the original size %d, got %s", len(s), m[2])
		}
		// What survived plus what the marker says was elided is the input.
		marker := fmt.Sprintf(truncationMarker, 0, 0)
		kept := len(got) - len(m[0]) - (len(marker) - len(fmt.Sprintf("truncated %d of %d bytes", 0, 0)))
		elided, _ := strconv.Atoi(m[1])
		if kept+elided != len(s) {
			t.Errorf("want kept %d + elided %d to equal the input %d", kept, elided, len(s))
		}
	})

	// Blind to what the output means, so it cannot pick a side.
	t.Run("splits the budget evenly", func(t *testing.T) {
		got := TruncateHeadTail(s, 200)
		head, _, ok := strings.Cut(got, "\n[... truncated")
		if !ok {
			t.Fatalf("want a marker separating head and tail, got %q", got)
		}
		_, tail, _ := strings.Cut(got, "...]\n")
		if len(head) != len(tail) {
			t.Errorf("want an even split, got head=%d tail=%d", len(head), len(tail))
		}
		if len(head) != 100 {
			t.Errorf("want half the budget each side, got %d", len(head))
		}
	})

	// A cut inside a multibyte character corrupts the JSON encode.
	t.Run("cuts on rune boundaries", func(t *testing.T) {
		wide := strings.Repeat("日", 500)
		got := TruncateHeadTail(wide, 200)
		if !utf8.ValidString(got) {
			t.Errorf("want valid UTF-8, got %q", got)
		}
		ceiling := 200 + len(fmt.Sprintf(truncationMarker, len(wide), len(wide))) + 6
		if len(got) > ceiling {
			t.Errorf("want the budget respected, got %d bytes", len(got))
		}
	})

	// Outward snapping used to let head and tail overlap here: the rune came
	// out twice and the marker read a negative count.
	t.Run("barely oversized input never overlaps or reports a negative", func(t *testing.T) {
		re := regexp.MustCompile(`truncated (-?\d+) of (\d+) bytes`)
		for _, max := range []int{6, 7, 8, 9, 10} {
			in := "abcd" + "一" + "efgh" // 11 bytes, the rune at 4..6
			got := TruncateHeadTail(in, max)
			m := re.FindStringSubmatch(got)
			if m == nil {
				t.Fatalf("max %d: want a marker, got %q", max, got)
			}
			elided, _ := strconv.Atoi(m[1])
			if elided < 0 {
				t.Errorf("max %d: want a non-negative elision, got %d (%q)", max, elided, got)
			}
			if !utf8.ValidString(got) {
				t.Errorf("max %d: want valid UTF-8, got %q", max, got)
			}
			head, _, _ := strings.Cut(got, "\n[... truncated")
			_, tail, _ := strings.Cut(got, "...]\n")
			if len(head)+len(tail)+elided != len(in) {
				t.Errorf("max %d: want head %d + tail %d + elided %d to equal the input %d (%q)",
					max, len(head), len(tail), elided, len(in), got)
			}
		}
	})

	// Exported, so a caller can pass a budget too small to hold anything.
	t.Run("a budget of zero or one keeps nothing", func(t *testing.T) {
		for _, max := range []int{0, 1} {
			got := TruncateHeadTail("hello", max)
			if !strings.Contains(got, "of 5 bytes") {
				t.Errorf("max %d: want the marker alone, got %q", max, got)
			}
		}
	})

	t.Run("short input is untouched", func(t *testing.T) {
		if got := TruncateHeadTail("short", 100); got != "short" {
			t.Errorf("got %q", got)
		}
	})
}
