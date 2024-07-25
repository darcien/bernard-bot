import {
  APIChatInputApplicationCommandInteractionData,
  APIUser,
  RESTPostAPIChatInputApplicationCommandsJSONBody,
} from "./deps.ts";

export function makeCommand(
  c: RESTPostAPIChatInputApplicationCommandsJSONBody,
): RESTPostAPIChatInputApplicationCommandsJSONBody {
  return c;
}

export type CommandContext = {
  interactionData: APIChatInputApplicationCommandInteractionData;
  user: APIUser;
  channelId: string;
  guildId: string;
  interactionId: string;
  continuationToken: string;
  db: Deno.Kv;
};

export type CommandHandlerResult = {
  responseText: string;
};

export type CommandHandler = (
  context: CommandContext,
) => Promise<CommandHandlerResult> | CommandHandlerResult;
