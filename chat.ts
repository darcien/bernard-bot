import {
  CommandContext,
  CommandHandlerResult,
  makeCommand,
} from "./command_utils.ts";
import {
  ApplicationCommandOptionType,
  InteractionResponseType,
} from "$discord-api-types";
import { createFollowupMessage } from "./discord_api.ts";
import { getOpenAiClient } from "./open_ai.ts";

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

const systemPrompt = `Your name is Bernard.
Everyone already knows your name,
so you don't have to introduce yourself everytime.
You work as a software engineer in a software house in Indonesia.
You will be chatting with your friend and coworker, be nice and helpful.
When asked a question, you should answer it the best you can concisely
and always include source if possible.
If the question is in Indonesian, you should also answer in Indonesian.
Sometimes you would also give random fun facts.
Sometimes you would also share a short story about your friend at work, Gema.`;

async function processChatMessage(
  message: string,
  interactionToken: string,
): Promise<void> {
  const client = getOpenAiClient();

  try {
    const completion = await client.chat.completions.create({
      messages: [
        // This is still not ideal because
        // the session is restarted on every message.
        // Ideally we should create a session,
        // and pick up the chat from the session until it expired.
        {
          role: "system",
          content: systemPrompt,
        },
        {
          role: "user",
          content: message,
        },
      ],
      model: "hf:meta-llama/Meta-Llama-3.1-405B-Instruct",
      n: 1,
    });

    await createFollowupMessage({
      message: completion.choices[0]?.message.content ?? "kurang tau bro",
      interactionToken,
    });
  } catch (rawError) {
    console.error(rawError);
    const error = rawError instanceof Error
      ? rawError
      : new Error(String(rawError));
    await createFollowupMessage({
      message: `error bro, katanya "${error.message}"`,
      interactionToken,
    });
  }
}

export function handleChatCommand(
  {
    interactionToken,
    interactionData,
  }: CommandContext,
): CommandHandlerResult {
  const options = interactionData.options || [];

  const messageOption = options.find(
    (option) => option.name === ChatCommandOption.Message,
  );

  const message = messageOption?.type === ApplicationCommandOptionType.String
    ? messageOption.value
    : "no messsage";

  processChatMessage(message, interactionToken).catch((err) =>
    console.error("chat background task failed:", err)
  );

  return {
    responseType: InteractionResponseType.DeferredChannelMessageWithSource,
    // With deferred response,
    // user see loading state like "Bernard is thinking...",
    // so the response text doesn't matter here.
    responseText: "Bernard is thinking...",
  };
}
