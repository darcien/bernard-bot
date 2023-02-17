import { configSync as loadConfig } from "https://deno.land/std@0.166.0/dotenv/mod.ts";
import { RESTPostAPIChatInputApplicationCommandsJSONBody } from "https://deno.land/x/discord_api_types@0.37.19/v10.ts";
import { commands } from "./commands.ts";

const config = loadConfig({ safe: true });

// https://discord.com/developers/docs/interactions/application-commands#create-global-application-command
const createCommandUrl = `https://discord.com/api/v10/applications/${config.DISCORD_APPLICATION_ID}/commands`;

async function upsertCommand(
  body: RESTPostAPIChatInputApplicationCommandsJSONBody
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
