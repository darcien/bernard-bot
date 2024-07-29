import { loadSync } from "@std/dotenv";
import { OpenAI } from "$openai";

const config = loadSync();

let client: OpenAI | null = null;

export function getOpenAiClient(): OpenAI {
  if (client) {
    return client;
  }

  client = new OpenAI({
    apiKey: config.GLHF_API_KEY,
    baseURL: "https://glhf.chat/api/openai/v1",
    maxRetries: 1,
    timeout: 60 * 1000, // 60 seconds (default is 10 minutes)
  });

  return client;
}
