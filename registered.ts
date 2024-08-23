import { loadSync } from "@std/dotenv";
import { fetchAsBot, makeDiscordApiUrl } from "./discord_api.ts";
import {
  RESTGetAPIApplicationCommandsResult,
  Routes,
} from "$discord-api-types";

const config = loadSync();

// https://discord.com/developers/docs/interactions/application-commands#get-global-application-commands
const getAllCommandsUrl = makeDiscordApiUrl(
  Routes.applicationCommands(config.DISCORD_APPLICATION_ID!),
);

async function getAllCommands() {
  console.log(`Getting all commands...`);

  const result = await fetchAsBot(getAllCommandsUrl, {
    method: "GET",
  });

  const res = await result.json() as RESTGetAPIApplicationCommandsResult;
  console.dir(res, {
    depth: Infinity,
  });
  const commandCount = Array.isArray(res) ? res.length : null;

  if (commandCount != null) {
    console.log(`Total registered commands=${commandCount}`);
  }
}

await getAllCommands();
