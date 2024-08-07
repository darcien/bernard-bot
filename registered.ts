import { loadSync } from "@std/dotenv";
import { makeDiscordApiUrl } from "./discord_api.ts";

const config = loadSync();

// https://discord.com/developers/docs/interactions/application-commands#get-global-application-commands
const getAllCommandsUrl = makeDiscordApiUrl(
  `/applications/${config.DISCORD_APPLICATION_ID}/commands`,
);

async function getAllCommands() {
  console.log(`Getting all commands...`);

  const result = await fetch(getAllCommandsUrl, {
    method: "GET",
    headers: {
      Authorization: `Bot ${config.DISCORD_BOT_TOKEN}`,
    },
  });

  const res = await result.json();
  console.dir(res, {
    depth: Infinity,
  });
  const commandCount = "length" in res && typeof res.length === "number"
    ? res.length
    : null;

  if (commandCount != null) {
    console.log(`Total registered commands=${commandCount}`);
  }
}

await getAllCommands();
