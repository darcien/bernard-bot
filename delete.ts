import { loadSync } from "./deps.ts";
import { makeDiscordApiUrl } from "./discord_api.ts";

const config = loadSync();

function makeDeleteCommandUrl(commandId: string) {
  // https://discord.com/developers/docs/interactions/application-commands#delete-global-application-command
  return makeDiscordApiUrl(
    `/applications/${config.DISCORD_APPLICATION_ID}/commands/${commandId}`,
  );
}

async function deleteCommand() {
  const idArgName = "--commandId=";
  const commandId = Deno.args
    .find((arg) => arg.startsWith(idArgName))
    ?.replace(idArgName, "");

  if (!commandId) {
    throw new Error(
      `Missing command id to delete. Please use ${idArgName} argument.`,
    );
  }
  console.log(`Deleting command id=${commandId}...`);

  const url = makeDeleteCommandUrl(commandId);
  const result = await fetch(url, {
    method: "DELETE",
    headers: {
      Authorization: `Bot ${config.DISCORD_BOT_TOKEN}`,
    },
  });

  switch (result.status) {
    case 204:
      console.log(`Success deleting command id=${commandId}`);
      break;
    default:
      console.log(await result.json());
      break;
  }
}

await deleteCommand();
