package tools

import "testing"

// hintingTool declares geometry; writingTool declines and reports a write.
type hintingTool struct{ fakeTool }

func (hintingTool) SnipHint() SnipHint {
	return SnipHint{Head: 3, Tail: 2, HeadChars: 100, TailChars: 50}
}

// emptyHintTool fills only the char budgets — the partial-literal case.
type emptyHintTool struct{ fakeTool }

func (emptyHintTool) SnipHint() SnipHint {
	return SnipHint{HeadChars: 8000, TailChars: 2000}
}

type writingTool struct{ fakeTool }

func (writingTool) ReadOnly() bool { return false }

func TestRegistry_SnipHintFor(t *testing.T) {
	reg := NewRegistry(100,
		&hintingTool{fakeTool{name: "hinter"}},
		&fakeTool{name: "reader"},
		&writingTool{fakeTool{name: "writer"}},
		&WebFetch{},
	)

	// The tool's hint beats any tier.
	t.Run("asks the tool first", func(t *testing.T) {
		want := SnipHint{Head: 3, Tail: 2, HeadChars: 100, TailChars: 50}
		if got := reg.SnipHintFor("hinter"); got != want {
			t.Errorf("want the tool's hint %+v, got %+v", want, got)
		}
	})

	t.Run("web_fetch declares Reasonix's geometry", func(t *testing.T) {
		want := SnipHint{Head: 120, Tail: 12, HeadChars: 12000, TailChars: 2000}
		if got := reg.SnipHintFor("web_fetch"); got != want {
			t.Errorf("want %+v, got %+v", want, got)
		}
	})

	// No hint: the tier falls out of ReadOnly.
	t.Run("falls back to the ReadOnly tier", func(t *testing.T) {
		if got := reg.SnipHintFor("reader"); got != DefaultReadOnlySnip {
			t.Errorf("want the read-only default, got %+v", got)
		}
		if got := reg.SnipHintFor("writer"); got != DefaultSideEffectingSnip {
			t.Errorf("want the side-effecting default, got %+v", got)
		}
	})

	// A partial literal asks for a split keeping no lines; honouring it would
	// throw the result away.
	t.Run("a hint that keeps no lines is discarded", func(t *testing.T) {
		reg := NewRegistry(100, &emptyHintTool{fakeTool{name: "empty"}})
		if got := reg.SnipHintFor("empty"); got != DefaultReadOnlySnip {
			t.Errorf("want the read-only default, got %+v", got)
		}
	})

	// Tool gone, or a message that never carried a name.
	t.Run("unknown name takes the read-only default", func(t *testing.T) {
		if got := reg.SnipHintFor("gone"); got != DefaultReadOnlySnip {
			t.Errorf("want the read-only default, got %+v", got)
		}
		if got := reg.SnipHintFor(""); got != DefaultReadOnlySnip {
			t.Errorf("want the read-only default, got %+v", got)
		}
	})
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry(100, &fakeTool{name: "reader"})
	if _, ok := reg.Get("reader"); !ok {
		t.Error("want the registered tool back")
	}
	if _, ok := reg.Get("gone"); ok {
		t.Error("want nothing for an unregistered name")
	}
}
