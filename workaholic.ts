import { CommandContext, makeCommand } from "./command_utils.ts";
import {
  APIApplicationCommandInteractionDataSubcommandOption,
  APIMessage,
  APIUser,
  ApplicationCommandOptionType,
  mapNotNullish,
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
  Overtime = "Overtime",
  PriorityHours = "Priority Hours",
}

const workaholicAddCommandDefaultValue = {
  [WorkaholicAddCommandOption.Type]: WorkaholicType.Overtime,
};

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
          description: `What did you do?`,
          type: ApplicationCommandOptionType.String,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.When,
          description: `When did you do it?`,
          type: ApplicationCommandOptionType.String,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.Duration,
          description: `How long did you do it?`,
          type: ApplicationCommandOptionType.Integer,
          required: true,
        },
        {
          name: WorkaholicAddCommandOption.Type,
          description: `What's the workaholic type?`,
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
          name: WorkaholicAddCommandOption.When,
          description: `For when?`,
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
    type: (sType in WorkaholicType ? sType : workaholicAddCommandDefaultValue[
      WorkaholicAddCommandOption.Type
    ]) as WorkaholicType,
  };
}

const prefix = "🐴";
// https://www.ascii-code.com/character/%E2%90%9F
const separator = "\u001f";
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
    `<@${raw.userId}>`,
    type,
    when,
    `${duration}h`,
    what,
  ].join(separator);
}

export function parseMessageForSummary(message: APIMessage) {
  const [_prefix, userMention, type, when, hDuration, what] = message
    .content
    .split(
      separator,
    );

  if (
    userMention == null || type == null || when == null || hDuration == null ||
    what == null
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

export function formatSummary(
  {
    workaholicMessages,
    nicknameByUserId,
  }: {
    workaholicMessages: Array<
      NonNullable<ReturnType<typeof parseMessageForSummary>>
    >;
    nicknameByUserId: ReturnType<typeof getNicknameByUserId>;
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
        nicknameByUserId.get(m.userId) || `<@${m.userId}>`,
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

function getNicknameByUserId(
  members: Awaited<ReturnType<typeof getGuildMembers>>,
) {
  const nicknameByUserId = new Map<string, string>();

  for (const member of members) {
    if (member.user) {
      nicknameByUserId.set(member.user.id, member.nick || member.user.username);
    }
  }

  return nicknameByUserId;
}

export async function handleWorkaholicCommand(
  { interactionData, user, channelId, guildId }: CommandContext,
) {
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
    const [messages, guildMembers] = await Promise.all([
      getMessagesFromChannel({ channelId }),
      // Hardcoded to 50 members for now
      getGuildMembers({ guildId, limit: 50 }),
    ]);
    const rWorkaholicMessages = messages.filter((message) =>
      message.content.startsWith(`${prefix}${separator}`)
    );

    const responseText = formatSummary(
      {
        workaholicMessages: mapNotNullish(
          rWorkaholicMessages,
          parseMessageForSummary,
        ),
        nicknameByUserId: getNicknameByUserId(guildMembers),
      },
    );

    return { responseText };
  }

  return {
    responseText: `Unknown subcommand!
${JSON.stringify(interactionData, null, 2)}`,
  };
}
