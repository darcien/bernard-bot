import {
  RESTGetAPIChannelMessagesResult,
  RESTGetAPIGuildMembersQuery,
  RESTGetAPIGuildMembersResult,
  RESTPatchAPIInteractionOriginalResponseJSONBody,
  RESTPatchAPIInteractionOriginalResponseResult,
  RESTPostAPIChannelMessageJSONBody,
  RESTPostAPIChannelMessageResult,
  RESTPostAPIInteractionFollowupJSONBody,
  RESTPostAPIInteractionFollowupResult,
  RouteBases,
  Routes,
} from "$discord-api-types";
import { loadSync } from "@std/dotenv";

const config = loadSync();

// https://discord.com/developers/docs/reference
export function makeDiscordApiUrl(path: string) {
  return `${RouteBases.api}${path}`;
}

const defaultAuthenticatedHeaders = {
  "Content-Type": "application/json",
  Authorization: `Bot ${config.DISCORD_BOT_TOKEN}`,
};

const repoUrl = "https://github.com/darcien/bernard-bot";
export async function fetchAsBot(input: string, init?: RequestInit) {
  console.log("🤖 Fetching as bot...", {
    input,
    init,
  });

  console.time(`⏱️ Fetching ${input}`);
  const result = await fetch(input, {
    ...init,
    headers: {
      // https://discord.com/developers/docs/reference#user-agent
      "User-Agent": `DiscordBot (${repoUrl})`,
      ...defaultAuthenticatedHeaders,
      ...init?.headers,
    },
  });
  console.timeEnd(`⏱️ Fetching ${input}`);

  return result;
}

export async function getMessagesFromChannel(
  { channelId, limit = 100 }: { channelId: string; limit?: number },
) {
  const url = addParamsToUrl(
    makeDiscordApiUrl(
      `/channels/${channelId}/messages`,
    ),
    { limit },
  );

  const res = await fetchAsBot(url);

  if (!res.ok) {
    throw new Error(`Failed to fetch messages from channel ${channelId}`);
  }

  const messages = Array.from(
    await res.json(),
  ) as RESTGetAPIChannelMessagesResult;

  return messages;
}

export async function sendMessageToChannel(
  { channelId, message }: { channelId: string; message: string },
) {
  const url = makeDiscordApiUrl(
    Routes.channelMessages(channelId),
  );

  const body: RESTPostAPIChannelMessageJSONBody = {
    content: message.slice(0, 2000),
  };

  const res = await fetchAsBot(url, {
    method: "POST",
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    throw new Error(`Failed to send message to channel ${channelId}`);
  }

  const sentMessage = (
    await res.json()
  ) as RESTPostAPIChannelMessageResult;

  return sentMessage;
}

export function addParamsToUrl(
  url: string,
  params: Record<string, string | number | boolean | undefined>,
) {
  const stringParams = new URLSearchParams(
    Object.entries(params).filter(([_, val]) => val != null).map((
      [key, val],
    ) => [key, String(val)]),
  ).toString();
  return `${url}?${stringParams}`;
}

export async function getGuildMembers(
  {
    guildId,
    after,
    limit,
  }: RESTGetAPIGuildMembersQuery & { guildId: string },
) {
  const rUrl = makeDiscordApiUrl(
    `/guilds/${guildId}/members`,
  );
  const urlWithParams = addParamsToUrl(
    rUrl,
    { after, limit },
  );

  const res = await fetchAsBot(urlWithParams);

  if (!res.ok) {
    throw new Error(`Bad response for getGuildMembers: ${res.status}`);
  }

  return Array.from(
    await res.json(),
  ) as RESTGetAPIGuildMembersResult;
}

export async function createFollowupMessage({
  message,
  interactionToken,
}: { message: string; interactionToken: string }) {
  const url = makeDiscordApiUrl(
    `/webhooks/${config.DISCORD_APPLICATION_ID}/${interactionToken}`,
  );

  const body: RESTPostAPIInteractionFollowupJSONBody = {
    content: message.slice(0, 2000),
  };

  const res = await fetchAsBot(url, {
    method: "POST",
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    throw new Error(`Bad response for createFollowupMessage: ${res.status}`);
  }

  const json = await res.json();

  return json as RESTPostAPIInteractionFollowupResult;
}

export async function editOriginalInteractionResponse({
  message,
  interactionToken,
}: { message: string; interactionToken: string }) {
  const url = makeDiscordApiUrl(
    `/webhooks/${config.DISCORD_APPLICATION_ID}/${interactionToken}/messages/@original`,
  );

  const body: RESTPatchAPIInteractionOriginalResponseJSONBody = {
    content: message.slice(0, 2000),
  };

  const res = await fetchAsBot(url, {
    method: "PATCH",
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    throw new Error(`Bad response for createFollowupMessage: ${res.status}`);
  }

  const json = await res.json();

  return json as RESTPatchAPIInteractionOriginalResponseResult;
}
