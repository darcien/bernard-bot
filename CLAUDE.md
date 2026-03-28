# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is this?

Bernard is a Discord slash command handler built with Go.
It responds to Discord interactions via HTTP webhooks.
Commands live in the `commands/` package, and the server validates Discord's ED25519 request signatures before dispatching to handlers.

## Commands

```sh
just check            # Format, vet, and build (run before committing)
just test             # Run all tests
just commands-register         # Register slash commands to Discord
just commands-list             # List registered commands
just commands-delete <id>      # Delete a command
```

## Architecture

Request flow: Discord POST → `main.go` (signature verification) → `commands/registry.go` (dispatch) → command handler → `discord/response.go` (format) → HTTP response

Adding a command:
1. Create `commands/<name>.go` with a handler matching the `Handler` type from `commands/registry.go`
2. Add the command definition in `commands/definitions.go`
3. Register the handler in `commands/registry.go`

Key types (`commands/registry.go`):
- `CommandContext` — what every handler receives (interaction data, user, IDs, token)
- `CommandResult` — `{ ResponseText, ResponseType }` that every handler returns
- `Handler` — the handler function signature

Response formatting (`discord/response.go`): Responses under 2000 chars (UTF-16 length, matching Discord's limit) are sent as JSON; larger responses become markdown file attachments via multipart FormData.

Deferred responses: Long-running commands (e.g. `/workaholic check`) return `DeferredChannelMessageWithSource` immediately, then fetch data and respond. See `discord/api.go` for the Discord API helpers.

## Environment variables

See `.env.example`. Required: `DISCORD_APPLICATION_ID`, `DISCORD_PUBLIC_KEY`, `DISCORD_BOT_TOKEN`.
The `justfile` loads `.env` automatically via `set dotenv-load`.

## Notes

- `/chat` and `/remind` are currently disabled and not implemented.
