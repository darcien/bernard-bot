package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func loadServer() (*server, error) {
	publicKeyHex := os.Getenv("DISCORD_PUBLIC_KEY")
	if publicKeyHex == "" {
		return nil, fmt.Errorf("missing DISCORD_PUBLIC_KEY")
	}
	keyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid DISCORD_PUBLIC_KEY: %w", err)
	}

	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("missing DISCORD_BOT_TOKEN")
	}

	applicationID := os.Getenv("DISCORD_APPLICATION_ID")
	if applicationID == "" {
		return nil, fmt.Errorf("missing DISCORD_APPLICATION_ID")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "4650"
	}

	// LLM config is optional — /chat goes offline when base URL or key is unset.
	llmModel := os.Getenv("LLM_MODEL")
	if llmModel == "" {
		llmModel = "deepseek-v4-flash"
	}

	return &server{
		publicKey:     ed25519.PublicKey(keyBytes),
		botToken:      botToken,
		applicationID: applicationID,
		port:          port,
		llmBaseURL:    os.Getenv("LLM_BASE_URL"),
		llmAPIKey:     os.Getenv("LLM_API_KEY"),
		llmModel:      llmModel,
		logLevel:      logLevel(os.Getenv("LOG_LEVEL")),
	}, nil
}

// logLevel maps LOG_LEVEL to a slog level. Debug is the default: at this
// bot's volume the per-step detail costs nothing, and a missing line means
// debugging by restart-and-hope.
func logLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}
