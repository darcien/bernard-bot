import {
  CommandContext,
  CommandHandlerResult,
  makeCommand,
} from "./command_utils.ts";
import {
  APIApplicationCommandInteractionDataSubcommandOption,
  APIMessage,
  APIUser,
  ApplicationCommandOptionType,
  markdownTable,
} from "./deps.ts";
import { getGuildMembers, getMessagesFromChannel } from "./discord_api.ts";

enum WorkaholicSubcommandOption {
  Add = "add",
  Check = "check",
}

enum WorkaholicAddCommandOption {
  What = "what",
  When = "when",
  Duration = "duration",
  Type = "type",
}

export enum WorkaholicType {
  Overtime = "OT",
  PriorityHours = "PH",
}

const workaholicAddCommandDefaultValue = {
  [WorkaholicAddCommandOption.Type]: WorkaholicType.Overtime,
};

enum WorkaholicCheckCommandOption {
  Who = "who",
  When = "when",
}

enum DebugCommandOption {
  NoMonthMatching = "no-month-matching",
}

export const workaholicCommand = makeCommand({
  name: "workaholic",
  description: "Add or check workaholic points",
  options: [
    {
      name: WorkaholicSubcommandOption.Add,
      description: "Add workaholic point for yourself",
      type: ApplicationCommandOptionType.Subcommand,
      options: [
        {
          name: WorkaholicAddCommandOption.What,
          description: "What did you do?",
          type: ApplicationCommandOptionType.String,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.When,
          description: "When did you do it?",
          type: ApplicationCommandOptionType.String,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.Duration,
          description: "How long did you do it (in hours)?",
          type: ApplicationCommandOptionType.Integer,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.Type,
          description: `What's the workaholic type? Default to ${
            workaholicAddCommandDefaultValue[WorkaholicAddCommandOption.Type]
          }`,
          type: ApplicationCommandOptionType.String,
          required: false,
          choices: [
            {
              name: WorkaholicType.Overtime,
              value: WorkaholicType.Overtime,
            },
            {
              name: WorkaholicType.PriorityHours,
              value: WorkaholicType.PriorityHours,
            },
          ],
        },
      ],
    },
    {
      name: WorkaholicSubcommandOption.Check,
      description: "Check scoreboard",
      type: ApplicationCommandOptionType.Subcommand,
      options: [
        {
          name: WorkaholicCheckCommandOption.Who,
          description: "For who? Default to everyone.",
          type: ApplicationCommandOptionType.User,
          required: false,
        },
        {
          name: WorkaholicCheckCommandOption.When,
          description: "(NOT IMPLEMENTED) For when? Default to current month.",
          type: ApplicationCommandOptionType.String,
          required: false,
        },
      ],
    },
  ],
});

type RawWorkaholicToAdd = {
  what: unknown;
  when: unknown;
  duration: unknown;
  type: unknown;
};

function sanitizeWorkaholicAdd(
  { what, when, duration, type: rType }: RawWorkaholicToAdd,
) {
  const sType = String(rType);
  return {
    what: String(what),
    when: String(when),
    duration: typeof duration === "number" ? duration : 0,
    type:
      (Object.values(WorkaholicType).includes(sType as WorkaholicType)
        ? sType
        : workaholicAddCommandDefaultValue[
          WorkaholicAddCommandOption.Type
        ]),
  };
}

function formatUserIdForDiscord(userId: string) {
  return `<@${userId}>`;
}

const prefix = "🐴";
// https://www.ascii-code.com/character/%E2%90%9F
const separator = " \u2043 ";
export function formatWorkaholicAddCommand(
  raw: RawWorkaholicToAdd & { userId: APIUser["id"] },
) {
  const {
    what,
    when,
    duration,
    type,
  } = sanitizeWorkaholicAdd(raw);
  return [
    prefix,
    // Mention the user sending the command
    formatUserIdForDiscord(raw.userId),
    type,
    when,
    `${duration}h`,
    what,
  ].join(separator);
}

export type ValidWorkaholicMessage = NonNullable<
  ReturnType<typeof parseMessageForSummary>
>;
export function parseMessageForSummary(message: APIMessage) {
  const [_prefix, userMention, type, when, hDuration, ...rWhat] = message
    .content
    .split(
      separator,
    );

  const what = rWhat.join(separator);

  if (
    userMention == null || type == null || when == null || hDuration == null ||
    what.trim() === ""
  ) {
    return null;
  }

  // userMention = <@1047114663546077236>
  const userId = userMention.slice(2, -1);
  const duration = hDuration.slice(0, -1);

  return {
    userId,
    what,
    when,
    duration,
    type,
  };
}

const rgxDateAndMonth = /[0-3]?\d \w{3}/;
const rgxOt = /ot|overtime/i;
const rgxDuration = /\d?\d\s?(jam|hours?)/i;
export function isMessagePartialMatch(messageContent: APIMessage["content"]) {
  // If message contains code block,
  // assume it's a big unrelated msg and unlikely to be
  // partial match.
  if (messageContent.includes("```")) {
    return false;
  }

  const containsOt = rgxOt.test(messageContent);
  if (rgxDuration.test(messageContent)) {
    // If msg contains duration,
    // no need to check date and month again
    // because the regex overlap,
    // date n month are looser version of duration.
    // e.g. 2 jam is a matching date n month
    // can be worked around using more specific month abbrev
    // but not worth it.
    return containsOt;
  }
  return containsOt && rgxDateAndMonth.test(messageContent);
}

export function makePartialMatchWarning({ messages, getNicknameByUserId }: {
  messages: Array<APIMessage>;
  getNicknameByUserId: ReturnType<typeof makeGetNicknameByUserIdFn>;
}) {
  if (messages.length === 0) {
    return null;
  }
  return [
    `I also found ${messages.length} message(s) that might worth some workaholic points:`,
    ...messages.map((m) =>
      `- ${
        getNicknameByUserId(m.author.id) || formatUserIdForDiscord(m.author.id)
      }, ${m.content}`
    ),
  ].join("\n");
}

enum WorkaholicResponseMessage {
  NoMessages = "No workaholic detected yet, keep working!",
}

export function makeSummaryTable(
  {
    workaholicMessages,
    getNicknameByUserId,
  }: {
    workaholicMessages: Array<
      NonNullable<ReturnType<typeof parseMessageForSummary>>
    >;
    getNicknameByUserId: ReturnType<typeof makeGetNicknameByUserIdFn>;
  },
) {
  const header = [
    "Who",
    "When",
    "How Long",
    "What",
    "Type",
  ];
  const table = markdownTable(
    [
      header,
      // TODO: add sorting/grouping
      ...workaholicMessages.map((m) => [
        getNicknameByUserId(m.userId) || formatUserIdForDiscord(m.userId),
        m.when,
        m.duration,
        m.what,
        m.type,
      ]),
    ],
  );
  return [
    "```markdown",
    table,
    "```",
  ].join("\n");
}

function makeGetNicknameByUserIdFn(
  members: Awaited<ReturnType<typeof getGuildMembers>>,
) {
  const nicknameByUserId = new Map<string, string>();

  for (const member of members) {
    if (member.user) {
      nicknameByUserId.set(member.user.id, member.nick || member.user.username);
    }
  }

  return function getNicknameByUserId(userId: string) {
    return nicknameByUserId.get(userId);
  };
}

export async function handleWorkaholicCommand(
  { interactionData, user, channelId, guildId }: CommandContext,
): Promise<CommandHandlerResult> {
  const options = interactionData.options || [];

  const addCommand = options.find((
    option,
  ): option is APIApplicationCommandInteractionDataSubcommandOption =>
    option.name === WorkaholicSubcommandOption.Add &&
    option.type === ApplicationCommandOptionType.Subcommand
  );

  if (addCommand) {
    const addOptions = addCommand.options || [];
    const whatOption = addOptions.find((option) =>
      option.name === WorkaholicAddCommandOption.What
    );
    const whenOption = addOptions.find((option) =>
      option.name === WorkaholicAddCommandOption.When
    );
    const durationOption = addOptions.find((option) =>
      option.name === WorkaholicAddCommandOption.Duration
    );
    const typeOption = addOptions.find((option) =>
      option.name === WorkaholicAddCommandOption.Type
    );
    return {
      responseText: formatWorkaholicAddCommand({
        what: whatOption?.value,
        when: whenOption?.value,
        duration: durationOption?.value,
        type: typeOption?.value,
        userId: user.id,
      }),
    };
  }

  const checkCommand = options.find((
    option,
  ): option is APIApplicationCommandInteractionDataSubcommandOption =>
    option.name === WorkaholicSubcommandOption.Check &&
    option.type === ApplicationCommandOptionType.Subcommand
  );

  if (checkCommand) {
    // It might be impossible to implement the when option efficiently
    // because the API does not provide:
    // - search API for messages
    // - before or after using timestamp instead of snowflake id
    // The bruteforce way to implement this would
    // be to iterate through the messages until we found message
    // with suitable timestamp, which might take multiple API calls
    // depending on the when options and channel activity.
    const checkOptions = checkCommand.options || [];
    const whenOption = checkOptions.find((option) =>
      option.name === WorkaholicCheckCommandOption.When
    );

    const doMonthMatching = !String(whenOption?.value ?? "").includes(
      DebugCommandOption.NoMonthMatching,
    );

    const whoOption = checkOptions.find((option) =>
      option.name === WorkaholicCheckCommandOption.Who
    );

    const who = whoOption?.type === ApplicationCommandOptionType.User
      ? whoOption.value
      : null;

    const [allMessages, guildMembers] = await Promise.all([
      // TODO: Retrieve all messages in the specified month
      // Right now this retrieves the last 100 only
      getMessagesFromChannel({ channelId }),
      // Hardcoded to 50 members for now
      getGuildMembers({ guildId, limit: 50 }),
    ]);

    // TODO: Consider making this aware of the user executor timezone
    // Right now this uses GMT+0 and might show messages from different months
    // at the start and the end of month depending on TZ.
    const getYearMonthPart = (isoTimestamp: string) => isoTimestamp.slice(0, 7);
    const thisMonthPart = getYearMonthPart(new Date().toISOString());

    const thisMonthMessages = allMessages.filter((m) =>
      doMonthMatching
        ? getYearMonthPart(m.timestamp) === thisMonthPart
        // Don't filter out messages by month
        // when month matching is disabled.
        : true
    );

    if (thisMonthMessages.length === 0) {
      return {
        responseText: WorkaholicResponseMessage.NoMessages,
      };
    }

    const matchingUserMessages = thisMonthMessages.filter((m) =>
      who == null ? true : m.mentions.find((user) => user.id === who) != null
    );

    // Only enable partial matching when checking for everyone.
    const enablePartialMatching = who == null;

    const partialMatchMessages: Array<APIMessage> = [];
    const matchingMessages: Array<ValidWorkaholicMessage> = [];

    // With the assumption messages from discord are sorted with timestamp desc:
    // Go through every messages in reverse order
    // so the top most message is the oldest message
    for (const channelMessage of matchingUserMessages.toReversed()) {
      if (channelMessage.content.startsWith(`${prefix}${separator}`)) {
        const validMessage = parseMessageForSummary(channelMessage);
        if (validMessage) {
          matchingMessages.push(validMessage);
        } else if (enablePartialMatching) {
          partialMatchMessages.push(channelMessage);
        }
      } else {
        if (
          enablePartialMatching &&
          channelMessage.webhook_id == null && !channelMessage.author.bot &&
          isMessagePartialMatch(channelMessage.content)
        ) {
          partialMatchMessages.push(channelMessage);
        }
      }
    }

    const getNicknameByUserId = makeGetNicknameByUserIdFn(guildMembers);

    const summaryTable = matchingMessages.length > 0
      ? makeSummaryTable(
        {
          workaholicMessages: matchingMessages,
          getNicknameByUserId,
        },
      )
      : null;
    const partialMatchWarning = makePartialMatchWarning({
      messages: enablePartialMatching ? partialMatchMessages : [],
      getNicknameByUserId,
    });

    if (summaryTable == null) {
      return {
        responseText: partialMatchWarning != null
          ? `No exact matches found but...`.concat("\n", partialMatchWarning)
          : WorkaholicResponseMessage.NoMessages,
      };
    }

    const responseText = [summaryTable, partialMatchWarning].filter(
      Boolean,
    )
      .join("\n");

    return { responseText };
  }

  return {
    responseText: `💣 Unhandled subcommand!
raw data=${JSON.stringify(interactionData, null, 2)}`,
  };
}
