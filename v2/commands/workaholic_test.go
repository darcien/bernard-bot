package commands

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"bernard/discord"
)

// --- isMessagePartialMatch ---

func TestIsMessagePartialMatch_True(t *testing.T) {
	cases := []string{
		"31 aug OT",
		"1 sep ot",
		"01 sep OT",
		"ot 2 jam",
		"OT 1hours",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if !isMessagePartialMatch(c) {
				t.Errorf("expected true for %q", c)
			}
		})
	}
}

func TestIsMessagePartialMatch_False(t *testing.T) {
	cases := []string{
		"ot",
		"2 jam",
		"23 aug",
		"```markdown```",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if isMessagePartialMatch(c) {
				t.Errorf("expected false for %q", c)
			}
		})
	}
}

// --- formatWorkaholicAdd ---

func TestFormatWorkaholicAdd(t *testing.T) {
	got := formatWorkaholicAdd("123", "my lorem ipsum work", "2021-01-01", 7, workaholicTypeOT)
	want := "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01 ⁃ 7h ⁃ my lorem ipsum work"
	if got != want {
		t.Errorf("want %q\ngot  %q", want, got)
	}

	got = formatWorkaholicAdd("456", "my lorem ipsum work", "today", 2, workaholicTypePH)
	want = "🐴 ⁃ <@456> ⁃ PH ⁃ today ⁃ 2h ⁃ my lorem ipsum work"
	if got != want {
		t.Errorf("want %q\ngot  %q", want, got)
	}
}

// --- parseMessage ---

func TestParseMessage(t *testing.T) {
	// Round-trip: format then parse gives back original fields.
	content := formatWorkaholicAdd("123", "my lorem ipsum work", "2021-01-01", 7, workaholicTypeOT)
	got := parseMessage(content)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	want := &workaholicEntry{
		userID:   "123",
		what:     "my lorem ipsum work",
		when:     "2021-01-01",
		duration: "7", // 'h' is stripped from "7h"
		wType:    workaholicTypeOT,
	}
	if *got != *want {
		t.Errorf("want %+v\ngot  %+v", want, got)
	}
}

func TestParseMessage_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"only prefix", "🐴"},
		{"missing what", "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01 ⁃ 7h"},
		{"missing duration and what", "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01"},
		{"no prefix", "<@123> ⁃ OT ⁃ 2021-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if parseMessage(tc.content) != nil {
				t.Errorf("expected nil for %q", tc.content)
			}
		})
	}
}

// --- makeSummaryTable ---

func TestMakeSummaryTable(t *testing.T) {
	nicknames := map[string]string{
		"123": "Capybara",
		"456": "Yagi",
		"789": "Kaffu",
	}

	// Duration values come in as-is from the caller (test mirrors TS test data).
	entries := []workaholicEntry{
		{userID: "123", when: "2 aug", duration: "2h", what: "do something cool", wType: workaholicTypeOT},
		{userID: "123", when: "3 aug", duration: "1h", what: "fix something awesome", wType: workaholicTypeOT},
		{userID: "456", when: "7 aug", duration: "3h", what: "prod incident something something", wType: workaholicTypeOT},
		{userID: "456", when: "17 aug", duration: "8h", what: "its holiday and im working", wType: workaholicTypePH},
		{userID: "789", when: "30 aug", duration: "2h", what: "need to finish this thing for showcase", wType: workaholicTypeOT},
	}

	got := makeSummaryTable(entries, nicknames)

	// Expected output matches the TS snapshot in __snapshots__/workaholic_test.ts.snap.
	want := "```markdown\n" +
		"| Who      | When   | How Long | What                                   | Type |\n" +
		"| -------- | ------ | -------- | -------------------------------------- | ---- |\n" +
		"| Capybara | 2 aug  | 2h       | do something cool                      | OT   |\n" +
		"| Capybara | 3 aug  | 1h       | fix something awesome                  | OT   |\n" +
		"| Yagi     | 7 aug  | 3h       | prod incident something something      | OT   |\n" +
		"| Yagi     | 17 aug | 8h       | its holiday and im working             | PH   |\n" +
		"| Kaffu    | 30 aug | 2h       | need to finish this thing for showcase | OT   |\n" +
		"```"

	if got != want {
		t.Errorf("table mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

// --- handleWorkaholicCheck (with mock API) ---

func TestHandleWorkaholicCheck_NoMessages(t *testing.T) {
	api := workaholicAPIClient{
		getMessages: func(channelID string, limit int) ([]discord.Message, error) {
			return []discord.Message{}, nil
		},
		getMembers: func(guildID string, limit int) ([]discord.Member, error) {
			return []discord.Member{}, nil
		},
	}

	handler := makeWorkaholicHandler(api)
	ctx := checkCtx("")
	result, err := handler(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseText != noWorkaholicMessage {
		t.Errorf("want %q, got %q", noWorkaholicMessage, result.ResponseText)
	}
}

func TestHandleWorkaholicCheck_APIError(t *testing.T) {
	api := workaholicAPIClient{
		getMessages: func(channelID string, limit int) ([]discord.Message, error) {
			return nil, errors.New("network error")
		},
		getMembers: func(guildID string, limit int) ([]discord.Member, error) {
			return []discord.Member{}, nil
		},
	}

	handler := makeWorkaholicHandler(api)
	_, err := handler(checkCtx(""))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHandleWorkaholicCheck_ExactMatches(t *testing.T) {
	// Use no-month-matching so the test doesn't depend on current date.
	msg1 := formatWorkaholicAdd("123", "wrote tests", "1 jan", 2, workaholicTypeOT)
	msg2 := formatWorkaholicAdd("456", "fixed bugs", "2 jan", 3, workaholicTypeOT)

	api := workaholicAPIClient{
		getMessages: func(channelID string, limit int) ([]discord.Message, error) {
			return []discord.Message{
				{Content: msg1, Author: discord.User{ID: "123"}, Timestamp: "2024-01-01T00:00:00Z"},
				{Content: msg2, Author: discord.User{ID: "456"}, Timestamp: "2024-01-02T00:00:00Z"},
			}, nil
		},
		getMembers: func(guildID string, limit int) ([]discord.Member, error) {
			u1 := discord.User{ID: "123", Username: "Alice"}
			u2 := discord.User{ID: "456", Username: "Bob"}
			return []discord.Member{{User: &u1}, {User: &u2}}, nil
		},
	}

	handler := makeWorkaholicHandler(api)
	result, err := handler(checkCtxNoMonth())
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseText == "" || result.ResponseText == noWorkaholicMessage {
		t.Errorf("expected table output, got %q", result.ResponseText)
	}
}

func TestHandleWorkaholicCheck_FilterByWho(t *testing.T) {
	msg1 := formatWorkaholicAdd("123", "wrote tests", "1 jan", 2, workaholicTypeOT)
	msg2 := formatWorkaholicAdd("456", "fixed bugs", "2 jan", 3, workaholicTypeOT)

	api := workaholicAPIClient{
		getMessages: func(channelID string, limit int) ([]discord.Message, error) {
			return []discord.Message{
				{
					Content:   msg1,
					Author:    discord.User{ID: "123"},
					Timestamp: "2024-01-01T00:00:00Z",
					Mentions:  []discord.User{{ID: "123"}},
				},
				{
					Content:   msg2,
					Author:    discord.User{ID: "456"},
					Timestamp: "2024-01-02T00:00:00Z",
					Mentions:  []discord.User{{ID: "456"}},
				},
			}, nil
		},
		getMembers: func(guildID string, limit int) ([]discord.Member, error) {
			u1 := discord.User{ID: "123", Username: "Alice"}
			u2 := discord.User{ID: "456", Username: "Bob"}
			return []discord.Member{{User: &u1}, {User: &u2}}, nil
		},
	}

	handler := makeWorkaholicHandler(api)
	// Filter to user 456 only, with no-month-matching.
	ctx := checkCtxWhoNoMonth("456")
	result, err := handler(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Should contain Bob's entry but not Alice's.
	if result.ResponseText == noWorkaholicMessage {
		t.Fatal("expected table output, got no-workaholic message")
	}
	if !strings.Contains(result.ResponseText, "Bob") {
		t.Error("expected Bob in output")
	}
	if strings.Contains(result.ResponseText, "Alice") {
		t.Error("expected Alice to be filtered out")
	}
}

// --- helpers ---

func checkCtx(who string) CommandContext {
	checkOpt := discord.InteractionDataOption{Name: "check", Type: discord.OptionTypeSubCommand}
	if who != "" {
		v, _ := json.Marshal(who)
		checkOpt.Options = []discord.InteractionDataOption{
			{Name: "who", Type: discord.OptionTypeUser, Value: v},
		}
	}
	return CommandContext{
		InteractionData: discord.ApplicationCommandData{
			Name:    "workaholic",
			Options: []discord.InteractionDataOption{checkOpt},
		},
		ChannelID: "ch1",
		GuildID:   "g1",
	}
}

func checkCtxWhoNoMonth(who string) CommandContext {
	whoVal, _ := json.Marshal(who)
	whenVal, _ := json.Marshal("no-month-matching")
	return CommandContext{
		InteractionData: discord.ApplicationCommandData{
			Name: "workaholic",
			Options: []discord.InteractionDataOption{
				{
					Name: "check",
					Type: discord.OptionTypeSubCommand,
					Options: []discord.InteractionDataOption{
						{Name: "who", Type: discord.OptionTypeUser, Value: whoVal},
						{Name: "when", Type: discord.OptionTypeString, Value: whenVal},
					},
				},
			},
		},
		ChannelID: "ch1",
		GuildID:   "g1",
	}
}

func checkCtxNoMonth() CommandContext {
	v, _ := json.Marshal("no-month-matching")
	return CommandContext{
		InteractionData: discord.ApplicationCommandData{
			Name: "workaholic",
			Options: []discord.InteractionDataOption{
				{
					Name: "check",
					Type: discord.OptionTypeSubCommand,
					Options: []discord.InteractionDataOption{
						{Name: "when", Type: discord.OptionTypeString, Value: v},
					},
				},
			},
		},
		ChannelID: "ch1",
		GuildID:   "g1",
	}
}
