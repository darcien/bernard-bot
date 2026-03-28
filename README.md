# Bernard

## What is this?

A collection of [Discord Slash command][discord-slash] handler built with Go.

[discord-slash]: https://support.discord.com/hc/en-us/articles/1500000368501-Slash-Commands-FAQ

## Does this provides any useful commands?

It depends, but generally no.

## Why?

For fun.

## Who is this Bernard?

It is my friend from my office. I never saw him, but nobody has ever proved he
doesn't exist either.

I believe he will show himself when we're in a real pinch, one day.

## How does this works?

Discord Slash commands handler works by responding to the HTTP request
send by Discord or sending webhook to Discord.

That applies here too.
The [main entry file][main-entry-file] registers an HTTP server to respond to Discord interaction requests.

[main-entry-file]: main.go

## Okay, I'm interested, how do I run this?

Prerequisites:
- [Go][go] 1.26+
- [just][just] for command runner.

[go]: https://go.dev/
[just]: https://github.com/casey/just

Steps:
- [Create a Discord app](https://discord.com/developers/docs/quick-start/getting-started)
- Prepare the environment variables.
  - See .env.example for example.
- [Register][register] the available commands to Discord by running `just commands-register`.
- Deploy the binary somewhere reachable by Discord.
- Update the application interaction URL to point to the deployed server.
- Add the application to a Discord server.
- You should be able to use the slash command now.

[register]: https://discord.com/developers/docs/interactions/application-commands#registering-a-command

## Deploy

```sh
# Build for linux, this creates `bernard-linux` binary
just build-linux

# Example command if you're doing traditional server
rsync --progress bernard-linux <server>:/home/darcien/bernard-bot/bernard

# Restart the systemd service so it picks up the new binary
sudo systemctl restart bernard
```


## Discord application configuration

To add the bot to a server, generate an invite URL under OAuth2 → URL Generator with these scopes:
- `bot` — makes the bot a guild member, required for REST API calls that read messages and members
- `applications.commands` — registers slash commands in the server

In your application → Bot → Privileged Gateway Intents, enable Server Members Intent. This is required because `/workaholic check` calls `GET /guilds/{id}/members` to resolve usernames, and Discord restricts this endpoint without the intent enabled.

## Why Golang?

Initially this was written in TS and using Deno runtime.
That was back in 2022, where writing TS and having 1 binary that does everything
fast is a breath of fresh air.
Coincidentally, the first commit happened on the day OpenAI announced ChatGPT (30 Nov 2022).

Now, fast forward to 2026, Deno Deploy Classic is shutting down soon.
New Deno Deploy doesn't support Deno Queue.
That means I need to find alternative and kill some commands that requires it.

These days I'm running everything inside a 1 GB RAM VPS.
So that's my primary target for hosting this.
And having Bernard idle with Deno runtime costs ~150MB, that's a lot.

There are alternative JS runtime with lower memory use.
But considering the actual code complexity we have, switching to another
language is very doable.
Especially compiled language to minimize the memory usage.

The top contender is either Golang or Rust as usual.
With TS investing into golang for TS 7, I think it makes more sense to
port to Golang rather than Rust.
(There are other considerations, but not going to write the long version here).

Hence, here we are, suddenly our TS codebase got converted to Golang overnight.
