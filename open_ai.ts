import { loadSync } from "@std/dotenv";
import { OpenAI } from "$openai";

const config = loadSync();

// TODO: Replace with a working OpenAI-compatible provider
const OPENAI_BASE_URL = "https://glhf.chat/api/openai/v1";

let client: OpenAI | null = null;

export function getOpenAiClient(): OpenAI {
  if (client) {
    return client;
  }

  client = new OpenAI({
    apiKey: config.OPENAI_API_KEY,
    baseURL: OPENAI_BASE_URL,
    maxRetries: 1,
    timeout: 3 * 60 * 1000, // 3 mins (default is 10 minutes)
  });

  return client;
}
