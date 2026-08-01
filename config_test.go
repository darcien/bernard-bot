package main

import (
	"log/slog"
	"testing"
)

func TestLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		// Unset or nonsense falls back to debug: at this volume a hidden
		// line costs more than a noisy one.
		"":        slog.LevelDebug,
		"verbose": slog.LevelDebug,
	}
	for in, want := range cases {
		if got := logLevel(in); got != want {
			t.Errorf("logLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadServer_RequiresDiscordConfig(t *testing.T) {
	t.Setenv("DISCORD_PUBLIC_KEY", "")
	if _, err := loadServer(); err == nil {
		t.Error("want an error when DISCORD_PUBLIC_KEY is unset")
	}

	// Valid hex, but the other required vars are still missing.
	t.Setenv("DISCORD_PUBLIC_KEY", "abcd")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	if _, err := loadServer(); err == nil {
		t.Error("want an error when DISCORD_BOT_TOKEN is unset")
	}

	t.Setenv("DISCORD_BOT_TOKEN", "tok")
	t.Setenv("DISCORD_APPLICATION_ID", "")
	if _, err := loadServer(); err == nil {
		t.Error("want an error when DISCORD_APPLICATION_ID is unset")
	}

	t.Setenv("DISCORD_APPLICATION_ID", "app")
	t.Setenv("LLM_MODEL", "")
	s, err := loadServer()
	if err != nil {
		t.Fatal(err)
	}
	if s.llmModel != "deepseek-v4-flash" {
		t.Errorf("want the default model, got %q", s.llmModel)
	}
	if s.port != "4650" {
		t.Errorf("want the default port, got %q", s.port)
	}
}
