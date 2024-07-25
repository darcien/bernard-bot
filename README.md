# Bernard

## What is this?

A collection of [Discord Slash command][discord-slash] handler created using [Deno](https://deno.land/)

[discord-slash]: https://support.discord.com/hc/en-us/articles/1500000368501-Slash-Commands-FAQ
[deno]: https://deno.com/

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
The [main entry file][main-entry-file] does 2 things:
- register a HTTP server to respond to Discord interactions request,
- setup queue to process the incoming request asynchronously.

[main-entry-file]: mod.ts

## Okay, I'm interested, how do I run this?

Prerequisites:
- [Deno][deno], whatever is the latest 1.x version.
  - This bot needs unstable features like Deno KV.
- [just][just] for command runner.

[just]: https://github.com/casey/just

- [Create a Discord app](https://discord.com/developers/docs/quick-start/getting-started)
- Prepare the environment variables.
  - See .env.example for example.
  - `DENO_DEPLOY_*` envs are only needed if you want to run this via Deno Deploy.
  - `GLHF_API_KEY` is needed for the OpenAPI style chat.
- [Register][register] the available commands to Discord by running `just register`.
- Deploy the main entry file somewhere reachable by Discord.
- Update the application interaction URL to point to the deployed file.
- Add the application a Discord server.
- You should be able to use the slash command from the bot and get responses back.

[register]: https://discord.com/developers/docs/interactions/application-commands#registering-a-command


## Why use `just` instead of `deno task`?

This project was started before Deno introduced `deno task`.
I'm too lazy to migrate it over.
