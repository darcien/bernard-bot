package tools

// Snip geometry: the informed half of tool-output shaping. The cap in Execute
// is blind and symmetric; the snip pass runs later, knows which tool produced
// a stale result, and asks it. Reasonix's tool.SnipHint / SnipHinter and
// agent.snipStrategyFor, ported whole.

// SnipHint is how maintenance should shorten a stale result this tool
// produced: Head/Tail lines from each end, HeadChars/TailChars when the result
// is one giant line.
//
// On the tool rather than in a name-keyed table, so a rename carries the
// policy with it and a new tool cannot take a default nobody chose — the
// stance test in package chat holds that line.
//
// Zero value invalid; implementers return positive counts. A hint keeping no
// lines asks for a split that keeps nothing, so SnipHintFor discards it.
type SnipHint struct {
	Head      int
	Tail      int
	HeadChars int
	TailChars int
}

// SnipHinter is implemented by tools whose output a generic head/tail split
// would garble. Discovered by type assertion; omitting it takes the
// ReadOnly-tiered default.
type SnipHinter interface {
	SnipHint() SnipHint
}

// Tiered by side effect, Reasonix's numbers. An observer front-loads its
// answer, so long head and short tail; a writer can fail at either end (what
// was written at the head, the error at the tail), so both ends evenly.
//
// Deliberately the only two: a tool fitting neither implements SnipHinter, and
// the stance test fails until it does.
var (
	DefaultReadOnlySnip      = SnipHint{Head: 80, Tail: 12, HeadChars: 10000, TailChars: 2000}
	DefaultSideEffectingSnip = SnipHint{Head: 40, Tail: 40, HeadChars: 8000, TailChars: 8000}
)

// Get returns the registered tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// SnipHintFor resolves geometry in Reasonix's snipStrategyFor order: the
// tool's own hint, then its ReadOnly tier. An unknown name — tool gone, or a
// message that never carried one — takes the read-only default, the safe guess
// for anything that reached a context window at all.
func (r *Registry) SnipHintFor(name string) SnipHint {
	t, ok := r.byName[name]
	if !ok {
		return DefaultReadOnlySnip
	}
	// A hint keeping no lines is a partly-filled literal, not a policy;
	// honouring it would throw the result away.
	if hinter, ok := t.(SnipHinter); ok {
		if h := hinter.SnipHint(); h.Head > 0 || h.Tail > 0 {
			return h
		}
	}
	if t.ReadOnly() {
		return DefaultReadOnlySnip
	}
	return DefaultSideEffectingSnip
}
