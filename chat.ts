import { CommandContext, makeCommand } from "./command_utils.ts";
import { ApplicationCommandOptionType } from "./deps.ts";
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
    continuationToken,
    interactionData,
    db,
  }: CommandContext,
) {
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
      continuation_token: continuationToken,
    } satisfies ChatMessage,
  );

  return {
    responseText: "bentar bro, mikir dulu",
  };
}
