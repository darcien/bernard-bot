/// <reference lib="deno.unstable" />
import {
  APIChatInputApplicationCommandInteractionData,
  APIInteraction,
  APIInteractionResponseChannelMessageWithSource,
  InteractionResponseType,
  InteractionType,
} from "$discord-api-types";
import { json, serve, validateRequest } from "$sift";
import { loadSync } from "@std/dotenv";
import { sign } from "$tweetnacl";
import { handleCommands } from "./commands.ts";
import { makeWebhookResponseFromHandlerResult } from "./webhook_response.ts";
import { handleQueueMessage } from "./queue.ts";

// Local uses .env file
const config = loadSync();

if (config.DISCORD_PUBLIC_KEY == null) {
  throw new Error("Missing DISCORD_PUBLIC_KEY");
}

// For all requests to "/" endpoint, we want to invoke home() handler.
serve({
  "/": home,
  "/ping": () => json({ message: "Pong!" }),
});

const db = await Deno.openKv();

db.listenQueue(handleQueueMessage);

// The main logic of the Discord Slash Command is defined in this function.
async function home(request: Request) {
  console.log({ headers: request.headers, body: request.body });

  // validateRequest() ensures that a request is of POST method and
  // has the following headers.
  const { error } = await validateRequest(request, {
    POST: {
      headers: ["X-Signature-Ed25519", "X-Signature-Timestamp"],
    },
  });
  if (error) {
    return json({ error: error.message }, { status: error.status });
  }

  // verifySignature() verifies if the request is coming from Discord.
  // When the request's signature is not valid, we return a 401 and this is
  // important as Discord sends invalid requests to test our verification.
  const { valid, body } = await verifySignature(request);
  if (!valid) {
    return json(
      { error: "Invalid request" },
      {
        status: 401,
      },
    );
  }

  const {
    type = 0,
    data = { options: [] },
    member,
    channel,
    guild_id,
    id,
    token,
  } = JSON
    .parse(
      body,
    ) as APIInteraction;

  // Discord performs Ping interactions to test our application.
  // Type 1 in a request implies a Ping interaction.
  if (type === InteractionType.Ping) {
    return json({
      type: InteractionResponseType.Pong,
    });
  }

  // Type 2 in a request is an ApplicationCommand interaction.
  // It implies that a user has issued a command.
  if (type === InteractionType.ApplicationCommand) {
    const interactionData =
      data as APIChatInputApplicationCommandInteractionData;

    if (channel?.id == null) {
      return json({ error: "Channel missing from interaction" }, {
        status: 400,
      });
    }
    if (guild_id == null) {
      return json({ error: "Guild id missing from interaction" }, {
        status: 400,
      });
    }
    const user = member?.user;
    if (user == null) {
      return json({ error: "User missing from interaction" }, { status: 400 });
    }

    try {
      const handlerOutput = await handleCommands({
        interactionId: id,
        interactionToken: token,
        interactionData,
        user,
        channelId: channel.id,
        guildId: guild_id,
        db,
      });

      if (handlerOutput == null) {
        return json({ error: "Unknown command" }, { status: 400 });
      }

      return makeWebhookResponseFromHandlerResult(handlerOutput);
    } catch (rError: unknown) {
      const error = rError instanceof Error
        ? rError
        : new Error(String(rError));

      return json({
        type: 4,
        data: {
          content: `💣💥 Oops, debug time!
Error: ${error.message}
Stack: ${error.stack || "No stack trace"}`,
        },
      } as APIInteractionResponseChannelMessageWithSource);
    }
  }

  // We will return a bad request error as a valid Discord request
  // shouldn't reach here.
  return json({ error: "Bad request" }, { status: 400 });
}

/** Verify whether the request is coming from Discord. */
async function verifySignature(
  request: Request,
): Promise<{ valid: boolean; body: string }> {
  // Discord sends these headers with every request.
  const signature = request.headers.get("X-Signature-Ed25519")!;
  const timestamp = request.headers.get("X-Signature-Timestamp")!;
  const body = await request.text();
  const valid = sign.detached.verify(
    new TextEncoder().encode(timestamp + body),
    hexToUint8Array(signature),
    hexToUint8Array(config.DISCORD_PUBLIC_KEY!),
  );

  return { valid, body };
}

/** Converts a hexadecimal string to Uint8Array. */
function hexToUint8Array(hex: string) {
  return new Uint8Array(hex.match(/.{1,2}/g)!.map((val) => parseInt(val, 16)));
}
