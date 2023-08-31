import {
  loadSync,
  RESTGetAPIChannelMessagesResult,
  RESTGetAPIGuildMembersQuery,
  RESTGetAPIGuildMembersResult,
} from "./deps.ts";

const config = loadSync();

const DISCORD_BASE_URL = "https://discord.com";
const DISCORD_API_VERSION = 10;

// https://discord.com/developers/docs/reference
export function makeDiscordApiUrl(path: string) {
  return new URL(`/api/v${DISCORD_API_VERSION}${path}`, DISCORD_BASE_URL)
    .toString();
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
  { channelId }: { channelId: string },
) {
  const url = makeDiscordApiUrl(
    `/channels/${channelId}/messages`,
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
