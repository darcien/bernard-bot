import { CommandHandlerResult } from "./command_utils.ts";
import {
  APIInteractionResponseChannelMessageWithSource,
  InteractionResponseType,
  json,
  Status,
  STATUS_TEXT,
} from "./deps.ts";

export function multipart(
  formData: FormData,
  init?: ResponseInit,
): Response {
  const headers = init?.headers instanceof Headers
    ? init.headers
    : new Headers(init?.headers);

  // For multipart, do not set the content-type manually
  // so the runtime can set it and the boundry automatically.
  // if (!headers.has("Content-Type")) {
  //   headers.set("Content-Type", "multipart/form-data");
  // }
  const statusText = init?.statusText ??
    STATUS_TEXT[(init?.status as Status) ?? Status.OK];

  return new Response(formData, {
    statusText,
    status: init?.status ?? Status.OK,
    headers,
  });
}

// https://discord.com/developers/docs/reference#uploading-files
// ------2231178723606858477463687254
// Content-Disposition: form-data; name="payload_json"

// {"type":4,"data":{"content":"hello","attachments":[{"id":0,"filename":"response.md"}]}}
// ------2231178723606858477463687254
// Content-Disposition: form-data; name="files[0]"; filename="response.md"
// Content-Type: text/markdown

// Hello world!
// ------2231178723606858477463687254--
export function makeReplyAsMarkdownAttachment(
  { content }: {
    content: string;
  },
) {
  const formData = new FormData();

  const attachmentName = "response.md";
  const attachmentId = 0;
  const type = "text/markdown";

  let fileContent = content;
  if (fileContent.includes("```markdown")) {
    // This is a temporary hack to remove useless
    // backtick formatting since we're sending the content
    // as a markdown file anyway.
    fileContent = fileContent.replace(/^```markdown$/gm, "");
    fileContent = fileContent.replace(/^```$/gm, "\n");
  }

  // While using Blob works type-wise and is logged
  // exactly the same as using File when using res.text(),
  // it does not work when responding to Discord API.
  const file = new File(
    [new Blob([fileContent], { type })],
    attachmentName,
    { type },
  );

  formData.set(
    "payload_json",
    JSON.stringify(
      {
        type: InteractionResponseType.ChannelMessageWithSource,
        data: {
          // Can add other JSON fields here
          // content: "hello mars",
          attachments: [{
            id: attachmentId,
            filename: attachmentName,
          }],
        },
      },
    ),
  );

  formData.set(`files[${attachmentId}]`, file, attachmentName);

  return formData;
}

const ENABLE_MULTIPART = true;

const DISCORD_MESSAGE_MAX_LENGTH = 2000;
export function makeWebhookResponseFromHandlerResult(
  { responseText }: CommandHandlerResult,
) {
  // TODO: Abstractize HTTP response command handler logic
  let content = responseText;

  // This length check probably will fail on weird unicode chars
  // because it's not doing graphemes counting.
  if (ENABLE_MULTIPART && content.length > DISCORD_MESSAGE_MAX_LENGTH) {
    return multipart(makeReplyAsMarkdownAttachment({
      content,
    }));
  }

  if (content.length > DISCORD_MESSAGE_MAX_LENGTH) {
    content = content.slice(0, 1900);
    content += "\n\n💸 not enough money to send the full responses.";
  }

  return json({
    // Type 4 responds with the below message retaining the user's
    // input at the top.
    type: InteractionResponseType.ChannelMessageWithSource,
    data: {
      content,
    },
  } as APIInteractionResponseChannelMessageWithSource);
}
