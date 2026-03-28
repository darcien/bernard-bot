package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"bernard/commands"
	"bernard/discord"
)

var discordPublicKey ed25519.PublicKey

func main() {
	publicKeyHex := os.Getenv("DISCORD_PUBLIC_KEY")
	if publicKeyHex == "" {
		log.Fatal("Missing DISCORD_PUBLIC_KEY")
	}
	keyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		log.Fatalf("Invalid DISCORD_PUBLIC_KEY: %v", err)
	}
	discordPublicKey = ed25519.PublicKey(keyBytes)

	port := os.Getenv("PORT")
	if port == "" {
		port = "4650"
	}

	http.HandleFunc("/ping", handlePing)
	http.HandleFunc("/", handleInteraction)

	srv := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second, // workaholic check calls Discord API, needs headroom
	}

	slog.Info("listening", "port", port)
	log.Fatal(srv.ListenAndServe())
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	discord.WriteJSON(w, http.StatusOK, discord.PingResponse{Message: "Pong!"})
}

func handleInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		discord.WriteJSON(w, http.StatusMethodNotAllowed, discord.ErrorResponse{Error: "method not allowed"})
		return
	}

	signature := r.Header.Get("X-Signature-Ed25519")
	timestamp := r.Header.Get("X-Signature-Timestamp")
	if signature == "" || timestamp == "" {
		discord.WriteJSON(w, http.StatusUnauthorized, discord.ErrorResponse{Error: "missing signature headers"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB — Discord payloads are <100KB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "failed to read body"})
		return
	}

	if !verifySignature(timestamp, body, signature) {
		slog.Warn("invalid signature", "remote", r.RemoteAddr)
		discord.WriteJSON(w, http.StatusUnauthorized, discord.ErrorResponse{Error: "invalid signature"})
		return
	}

	var interaction discord.Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "invalid JSON"})
		return
	}

	if interaction.Type == discord.InteractionTypePing {
		discord.WriteJSON(w, http.StatusOK, discord.PongResponse{Type: discord.InteractionResponseTypePong})
		return
	}

	if interaction.Type == discord.InteractionTypeApplicationCommand {
		handleApplicationCommand(w, &interaction)
		return
	}

	discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "unknown interaction type"})
}

func handleApplicationCommand(w http.ResponseWriter, interaction *discord.Interaction) {
	if interaction.Channel == nil {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "missing channel"})
		return
	}
	if interaction.GuildID == "" {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "missing guild"})
		return
	}
	if interaction.Member == nil || interaction.Member.User == nil {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "missing member"})
		return
	}
	if interaction.Data == nil {
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "missing data"})
		return
	}

	ctx := commands.CommandContext{
		InteractionData:  *interaction.Data,
		User:             *interaction.Member.User,
		ChannelID:        interaction.Channel.ID,
		GuildID:          interaction.GuildID,
		InteractionID:    interaction.ID,
		InteractionToken: interaction.Token,
	}

	cmdName := ctx.InteractionData.Name
	username := ctx.User.Username
	start := time.Now()

	result, found, err := commands.Dispatch(ctx)
	dur := time.Since(start)
	if err != nil {
		slog.Error("command error", "command", cmdName, "user", username, "dur", dur, "err", err)
		errMsg := fmt.Sprintf("💣💥 Oops, debug time!\nError: %v\nStack: No stack trace", err)
		discord.RespondFromResult(w, discord.InteractionResponseTypeChannelMessageWithSource, errMsg)
		return
	}
	if !found {
		slog.Warn("unknown command", "command", cmdName, "user", username)
		discord.WriteJSON(w, http.StatusBadRequest, discord.ErrorResponse{Error: "unknown command"})
		return
	}

	slog.Info("command", "command", cmdName, "user", username, "dur", dur)
	discord.RespondFromResult(w, result.ResponseType, result.ResponseText)
}

// verifySignature checks the ED25519 signature from Discord.
// Matches verifySignature in mod.ts.
func verifySignature(timestamp string, body []byte, signature string) bool {
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	msg := make([]byte, len(timestamp)+len(body))
	copy(msg, timestamp)
	copy(msg[len(timestamp):], body)
	return ed25519.Verify(discordPublicKey, msg, sigBytes)
}
