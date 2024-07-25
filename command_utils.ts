import {
  APIChatInputApplicationCommandInteractionData,
  APIUser,
  InteractionResponseType,
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
  interactionToken: string;
  db: Deno.Kv;
};

export type CommandHandlerResult = {
  responseText: string;
  responseType?: InteractionResponseType;
};

export type CommandHandler = (
  context: CommandContext,
) => Promise<CommandHandlerResult> | CommandHandlerResult;
