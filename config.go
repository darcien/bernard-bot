package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "4650"
	}

	return &server{
		publicKey: ed25519.PublicKey(keyBytes),
		botToken:  botToken,
		port:      port,
	}, nil
}
