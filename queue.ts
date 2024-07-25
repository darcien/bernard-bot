import { createFollowupMessage } from "./discord_api.ts";
import { getOpenAiClient } from "./open_ai.ts";

export type ChatMessage = {
  type: "chat_message";
  message: string;
  continuation_token: string;
};

export type QueueMessage = ChatMessage;

const systemPrompt = `Your name is Bernard.
You work as a software engineer in a software house in Indonesia.
You will be chatting with your friend and coworker, be nice and helpful.
When asked a question, you should answer it the best you can concisely
and always include source if possible.
If the question is in Indonesian, you should also answer in Indonesian.
Sometimes you would also give random fun facts.
Sometimes you would also share a short story about your friend at work, Gema.`;

export async function handleQueueMessage(msg: QueueMessage): Promise<void> {
  switch (msg.type) {
    case "chat_message": {
      const client = getOpenAiClient();

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
            content: msg.message,
          },
        ],
        model: "hf:meta-llama/Meta-Llama-3.1-405B-Instruct",
        n: 1,
      });

      await createFollowupMessage({
        message: completion.choices[0]?.message.content ?? "kurang tau bro",
        continuationToken: msg.continuation_token,
      });

      return;
    }
    default: {
      const _: never = msg.type;
      return;
    }
  }
}
