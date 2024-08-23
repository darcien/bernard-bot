import { commands } from "./commands.ts";
import { makeDiscordApiUrl } from "./discord_api.ts";
import { loadSync } from "@std/dotenv";
import {
  RESTPostAPIChatInputApplicationCommandsJSONBody,
} from "$discord-api-types";

const config = loadSync();

// https://discord.com/developers/docs/interactions/application-commands#create-global-application-command
const createCommandUrl = makeDiscordApiUrl(
  `/applications/${config.DISCORD_APPLICATION_ID}/commands`,
);

function toInteger(input: string | null): number {
  if (input == null) {
    return 0;
  }

  const parsed = parseInt(input, 10);

  return isFinite(parsed) ? parsed : 0;
}

function toFloat(input: string | null): number {
  if (input == null) {
    return 0;
  }

  const parsed = parseFloat(input);

  return isFinite(parsed) ? parsed : 0;
}

async function upsertCommand(
  body: RESTPostAPIChatInputApplicationCommandsJSONBody,
) {
  const result = await fetch(createCommandUrl, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bot ${config.DISCORD_BOT_TOKEN}`,
    },
    body: JSON.stringify(body),
  });

  // https://discord.com/developers/docs/topics/rate-limits#header-format
  const rateLimit = {
    remainingRequests: toInteger(result.headers.get("X-RateLimit-Remaining")),
    // Total time (in seconds) of when the current rate limit bucket will reset.
    // Can have decimals to match previous millisecond ratelimit precision
    resetAfterSeconds: toFloat(result.headers.get("X-RateLimit-Reset-After")),
  };

  switch (result.status) {
    case 200:
      console.log(`Success updating command /${body.name}`);
      return { rateLimit };
    case 201:
      console.log(`Success creating new command /${body.name}`);
      return { rateLimit };
    default:
      console.log(`Fail upserting command /${body.name}`, await result.json());
      return { rateLimit };
  }
}

function sleep(ms: number) {
  return new Promise<void>((resolve) =>
    setTimeout(() => {
      resolve();
    }, ms)
  );
}

async function idle(seconds: number) {
  let remainingSeconds = seconds;
  while (remainingSeconds > 0) {
    console.log(`Idling for ${remainingSeconds} seconds...`);
    await sleep(1000);
    remainingSeconds -= 1;
  }
  console.log("Done idling.");
}

console.log(`Registering ${commands.length} commands...`);

for (let i = 0; i < commands.length; i++) {
  const command = commands[i];

  const { rateLimit } = await upsertCommand(command!);

  const isLast = (i + 1) === commands.length;

  if (rateLimit.remainingRequests === 0 && !isLast) {
    const idleSeconds = Math.ceil(rateLimit.resetAfterSeconds);
    console.log(
      `Hitting rate limit when registering commands. Will idle for ${idleSeconds} seconds.`,
    );
    await idle(idleSeconds);
  }
}

console.log(`Finished registering commands`);
