package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"bernard/chat"
	"bernard/commands"
	"bernard/discord"
	"bernard/gateway"
	"bernard/llm"
)

type server struct {
	publicKey     ed25519.PublicKey
	botToken      string
	applicationID string
	port          string
	llmBaseURL    string
	llmAPIKey     string
	llmModel      string
	logLevel      slog.Level
}

func main() {
	s, err := loadServer()
	if err != nil {
		log.Fatal(err)
	}
	slog.SetLogLoggerLevel(s.logLevel)
	discord.SetBotToken(s.botToken)

	var llmClient *llm.Client
	if s.llmBaseURL != "" && s.llmAPIKey != "" {
		llmClient = llm.NewClient(s.llmBaseURL, s.llmAPIKey, s.llmModel)
	} else {
		slog.Warn("LLM config missing, chat is offline")
	}
	bernard := chat.New(llmClient, s.applicationID)

	// Static facts once, so per-turn lines don't repeat them.
	slog.Info("chat config",
		"model", s.llmModel,
		"llm_configured", llmClient != nil,
		"tools", bernard.ToolNames(),
		"log_level", s.logLevel)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", s.handlePing)
	mux.HandleFunc("POST /", s.handleInteraction)

	srv := &http.Server{
		Addr:         ":" + s.port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second, // workaholic check calls Discord API, needs headroom
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("listening", "port", s.port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	// Gateway connection for @mention chat. A fatal error (bad token or
	// intents) stops only this entry point; slash commands keep working.
	go func() {
		err := gateway.Run(ctx, gateway.Config{
			Token:   s.botToken,
			Intents: gateway.IntentGuilds | gateway.IntentGuildMessages,
			OnEvent: func(eventType string, data json.RawMessage) {
				if eventType != "MESSAGE_CREATE" {
					return
				}
				var msg discord.Message
				if err := json.Unmarshal(data, &msg); err != nil {
					slog.Warn("bad MESSAGE_CREATE payload", "err", err)
					return
				}
				bernard.HandleMention(msg)
			},
		})
		if err != nil {
			slog.Error("gateway stopped for good", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	// Give in-flight requests time to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal(err)
	}

	// Chat replies run in goroutines the HTTP server doesn't track. A tool
	// loop can outlast any reasonable wait, so after ShutdownWait we cancel
	// in-flight turns (users get a "restarting" reply) and give those
	// replies a moment to post.
	if !bernard.Wait(chat.ShutdownWait) {
		slog.Warn("chat replies did not finish before timeout, cancelling")
		bernard.Cancel()
		if !bernard.Wait(10 * time.Second) {
			slog.Warn("chat replies still running after cancel")
		}
	}
	slog.Info("stopped")
}

func (s *server) handlePing(w http.ResponseWriter, r *http.Request) {
	discord.WriteJSON(w, http.StatusOK, discord.PingResponse{Message: "Pong!"})
}

func (s *server) handleInteraction(w http.ResponseWriter, r *http.Request) {
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

	if !s.verifySignature(timestamp, body, signature) {
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
		errMsg := fmt.Sprintf("💣💥 Oops, debug time!\nError: %v", err)
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
func (s *server) verifySignature(timestamp string, body []byte, signature string) bool {
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	msg := make([]byte, len(timestamp)+len(body))
	copy(msg, timestamp)
	copy(msg[len(timestamp):], body)
	return ed25519.Verify(s.publicKey, msg, sigBytes)
}
