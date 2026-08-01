// Package tools is the tool-calling surface exposed to the LLM: a minimal
// Tool interface, a registry with byte-stable schemas (tool list is part of
// the cached prompt prefix), and safe execution where failures become result
// text instead of aborting the loop.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"
	"unicode/utf8"
)

type Tool interface {
	Name() string        // e.g. "web_fetch"
	Description() string // shown to the model
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

type Registry struct {
	byName    map[string]Tool
	names     []string // sorted; schema order and the startup log line
	schemas   json.RawMessage
	resultCap int
}

// NewRegistry builds the registry and marshals the OpenAI "tools" array once,
// sorted by tool name so the bytes are stable regardless of registration
// order. Duplicate names are a wiring bug and panic.
func NewRegistry(resultCap int, ts ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool, len(ts)), resultCap: resultCap}
	for _, t := range ts {
		if _, dup := r.byName[t.Name()]; dup {
			panic("tools: duplicate tool name " + t.Name())
		}
		r.byName[t.Name()] = t
	}

	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	r.names = names

	type functionSchema struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type schemaEntry struct {
		Type     string         `json:"type"`
		Function functionSchema `json:"function"`
	}
	entries := make([]schemaEntry, 0, len(names))
	for _, name := range names {
		t := r.byName[name]
		entries = append(entries, schemaEntry{
			Type: "function",
			Function: functionSchema{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	schemas, err := json.Marshal(entries)
	if err != nil {
		panic(fmt.Sprintf("tools: marshal schemas: %v", err))
	}
	r.schemas = schemas
	return r
}

// Schemas returns the OpenAI "tools" array, marshaled once at construction.
func (r *Registry) Schemas() json.RawMessage { return r.schemas }

// Names returns the registered tool names, sorted.
func (r *Registry) Names() []string { return slices.Clone(r.names) }

// Execute runs a named tool. Failures come back as result text ("error: ...")
// so the model can react; the tool loop never aborts on a tool error.
func (r *Registry) Execute(ctx context.Context, name, args string) string {
	if args == "" {
		args = "{}" // some gateways send "" for no-arg calls
	}
	t, ok := r.byName[name]
	if !ok {
		slog.Debug("tool call", "name", name, "err", "unknown tool")
		return "error: unknown tool " + name
	}

	start := time.Now()
	out, err := t.Execute(ctx, json.RawMessage(args))
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		// The model's arguments are the thing worth seeing when a tool
		// misbehaves — that's its decision, not user content.
		slog.Debug("tool call", "name", name, "args", TruncateRunes(args, 200), "dur", dur, "err", err)
		return "error: " + err.Error()
	}

	result := TruncateRunes(out, r.resultCap)
	attrs := []any{"name", name, "args", TruncateRunes(args, 200), "dur", dur, "chars", len(result)}
	if len(result) != len(out) {
		attrs = append(attrs, "truncated_from", len(out))
	}
	slog.Debug("tool call", attrs...)
	return result
}

// TruncateRunes caps s at max runes, cutting on a rune boundary (a byte
// slice can split a multibyte character and corrupt the JSON encode).
func TruncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "\n[truncated]"
}

// TruncateHeadTail caps s at max runes keeping the head and tail halves —
// pages and logs often bury the useful part at the bottom.
func TruncateHeadTail(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	rs := []rune(s)
	half := max / 2
	return string(rs[:half]) + "\n[... truncated ...]\n" + string(rs[len(rs)-half:])
}
