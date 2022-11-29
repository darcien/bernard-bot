import { configSync as loadConfig } from "https://deno.land/std@0.166.0/dotenv/mod.ts";

const config = loadConfig({ safe: true });

function makeDeleteCommandUrl(commandId: string) {
  // https://discord.com/developers/docs/interactions/application-commands#delete-global-application-command
  return `https://discord.com/api/v10/applications/${config.DISCORD_APPLICATION_ID}/commands/${commandId}`;
}

async function deleteCommand() {
  const idArgName = "--commandId=";
  const commandId = Deno.args
    .find((arg) => arg.startsWith(idArgName))
    ?.replace(idArgName, "");

  if (!commandId) {
    throw new Error(
      `Missing command id to delete. Please use ${idArgName} argument.`
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
