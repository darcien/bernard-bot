package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCurrentTime(t *testing.T) {
	tool := CurrentTime{}

	t.Run("no arguments defaults to UTC", func(t *testing.T) {
		got, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, "UTC") {
			t.Errorf("want a UTC timestamp, got %q", got)
		}
		if _, err := time.Parse("Monday, 2 January 2006 15:04 MST", got); err != nil {
			t.Errorf("want a parseable timestamp, got %q", got)
		}
	})

	t.Run("timezone is applied", func(t *testing.T) {
		got, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Asia/Jakarta"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, "WIB") {
			t.Errorf("want Jakarta time, got %q", got)
		}
	})

	// Errors reach the model as text, so they must be legible, not a Go
	// type name.
	t.Run("unknown timezone errors", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Mars/Olympus"}`))
		if err == nil || !strings.Contains(err.Error(), "Mars/Olympus") {
			t.Errorf("want an error naming the timezone, got %v", err)
		}
	})

	t.Run("malformed arguments error", func(t *testing.T) {
		if _, err := tool.Execute(context.Background(), json.RawMessage(`not json`)); err == nil {
			t.Error("want an error for malformed arguments")
		}
	})
}
