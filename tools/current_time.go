package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CurrentTime tells the model what time it is. It exists so the system
// prompt never needs a timestamp (which would break prefix-cache stability).
type CurrentTime struct{}

func (CurrentTime) Name() string { return "current_time" }

func (CurrentTime) ReadOnly() bool { return true }

func (CurrentTime) Description() string {
	return "Get the current date and time. Use when you need to know what time or day it is now."
}

func (CurrentTime) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"timezone": {
				"type": "string",
				"description": "IANA timezone like Asia/Jakarta. Defaults to UTC."
			}
		}
	}`)
}

func (CurrentTime) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	loc := time.UTC
	if params.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(params.Timezone)
		if err != nil {
			return "", fmt.Errorf("unknown timezone %q", params.Timezone)
		}
	}
	return time.Now().In(loc).Format("Monday, 2 January 2006 15:04 MST"), nil
}
