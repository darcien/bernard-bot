import {
  CommandContext,
  CommandHandlerResult,
  makeCommand,
} from "./command_utils.ts";
import {
  ApplicationCommandOptionType,
  InteractionResponseType,
} from "./deps.ts";
import { ChatMessage } from "./queue.ts";

enum ChatCommandOption {
  Message = "message",
}

export const chatCommand = makeCommand({
  name: "chat",
  description: "Chat with me",
  options: [
    {
      name: ChatCommandOption.Message,
      description: "Your message for me",
      type: ApplicationCommandOptionType.String,
      required: true,
      min_length: 1,
    },
  ],
});

export async function handleChatCommand(
  {
    interactionToken,
    interactionData,
    db,
  }: CommandContext,
): Promise<CommandHandlerResult> {
  const options = interactionData.options || [];

  const messageOption = options.find(
    (option) => option.name === ChatCommandOption.Message,
  );

  const message = messageOption?.type === ApplicationCommandOptionType.String
    ? messageOption.value
    : "no messsage";

  await db.enqueue(
    {
      type: "chat_message",
      message,
      interaction_token: interactionToken,
    } satisfies ChatMessage,
  );

  return {
    responseType: InteractionResponseType.DeferredChannelMessageWithSource,
    // With deferred response,
    // user see loading state like "Bernard is thinking...",
    // so the response text doesn't matter here.
    responseText: "Bernard is thinking...",
  };
}
