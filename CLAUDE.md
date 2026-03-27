# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is this?

Bernard is a Discord slash command handler built with Deno.
It responds to Discord interactions via HTTP webhooks.
Commands live as individual files, and the server validates Discord's ED25519 request signatures before dispatching to handlers.

## Commands

```sh
just test             # Run all tests
just update-snapshot  # Update test snapshots
just register         # Register slash commands to Discord
just get-registered   # List registered commands
just delete-registered <commandId>  # Delete a command

deno task check       # Format, lint, and type-check (run before committing)
```

## Architecture

Request flow: Discord POST → `mod.ts` (signature verification) → `commands.ts` (dispatch) → command handler → `webhook_response.ts` (format) → HTTP response

Adding a command:
1. Create `<name>.ts` exporting a `makeCommand(...)` definition and a `handle<Name>Command` handler matching the `CommandHandler` type from `command_utils.ts`
2. Register both in `commands.ts` (`commands` array + `commandHandlerMap`)

Key types (`command_utils.ts`):
- `CommandContext` — what every handler receives (interaction data, user, IDs, token)
- `CommandHandlerResult` — `{ responseText, responseType? }` that every handler returns
- `CommandHandler` — the handler function signature

Response formatting (`webhook_response.ts`): Responses under 2000 chars are sent as JSON; larger responses become markdown file attachments via multipart FormData.

Deferred responses: Long-running commands return `InteractionResponseType.DeferredChannelMessageWithSource` immediately, then call Discord's webhook API to send a followup. See `discord_api.ts` for the followup helper.

## Environment variables

See `.env.example`. Required: `DISCORD_APPLICATION_ID`, `DISCORD_PUBLIC_KEY`, `DISCORD_BOT_TOKEN`. The `justfile` loads `.env` automatically via `set dotenv-load`.

## Notes

- `deps.ts` exists as a workaround for a Deno import resolution bug (issue #17784) with the e25n package.
- `/chat` and `/remind` are currently disabled and not supported.
