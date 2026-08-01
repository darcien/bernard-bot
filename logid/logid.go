// Package logid tags a context with the turn that owns it, so the debug
// lines emitted deep in llm and tools can be attributed back to it.
//
// Turns from different channels run concurrently and their rounds interleave
// in the log. Without a tag, "llm call" and "tool call" lines can only be
// read when exactly one turn is in flight — which is not the traffic the
// measurements are for.
package logid

import "context"

type key struct{}

// With tags ctx. The id is the triggering Discord message, so a log line
// leads back to the message that caused it.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// Attrs is the slog pair for ctx's tag, empty when untagged — nothing outside
// a turn (startup, tests) gains a field it can't fill.
func Attrs(ctx context.Context) []any {
	id, ok := ctx.Value(key{}).(string)
	if !ok || id == "" {
		return nil
	}
	return []any{"turn", id}
}
