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
  switch (result.status) {
    case 200:
      console.log(`Success updating command /${body.name}`);
      break;
    case 201:
      console.log(`Success creating new command /${body.name}`);
      break;
    default:
      console.log(`Fail upserting command /${body.name}`, await result.json());
      break;
  }
}

console.log(`Registering ${commands.length} commands...`);
for (const command of commands) {
  await upsertCommand(command);
}
console.log(`Finished registering commands`);
