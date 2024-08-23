import {
  CommandContext,
  CommandHandlerResult,
  makeCommand,
} from "./command_utils.ts";
import {
  ApplicationCommandOptionType,
  InteractionResponseType,
} from "$discord-api-types";
import { ReminderMessage } from "./queue.ts";

enum RemindCommandOption {
  Message = "message",
  Who = "who",
  Days = "days",
  Hours = "hours",
  Minutes = "minutes",
}

export const remindCommand = makeCommand({
  name: "remind",
  description: "Schedule a reminder",
  options: [
    {
      name: RemindCommandOption.Message,
      description: "Reminder message",
      type: ApplicationCommandOptionType.String,
      required: true,
      min_length: 1,
    },
    {
      name: RemindCommandOption.Who,
      description: "Who to remind. Default to you",
      type: ApplicationCommandOptionType.User,
      required: false,
    },
    {
      name: RemindCommandOption.Days,
      description: "Remind in how many days",
      type: ApplicationCommandOptionType.Integer,
      required: false,
      min_value: 0,
      max_value: 366,
    },
    {
      name: RemindCommandOption.Hours,
      description: "Remind in how many hours",
      type: ApplicationCommandOptionType.Integer,
      required: false,
      min_value: 0,
      max_value: 24 * 31,
    },
    {
      name: RemindCommandOption.Minutes,
      description: "Remind in how many minutes",
      type: ApplicationCommandOptionType.Integer,
      required: false,
      min_value: 0,
      max_value: 60 * 24,
    },
  ],
});

export async function handleRemindCommand(
  {
    interactionData,
    db,
    user,
    channelId,
  }: CommandContext,
): Promise<CommandHandlerResult> {
  const options = interactionData.options || [];

  const messageOption = options.find(
    (option) => option.name === RemindCommandOption.Message,
  );

  const message = messageOption?.type === ApplicationCommandOptionType.String
    ? messageOption.value
    : "no messsage";

  const whoOption = options.find(
    (option) => option.name === RemindCommandOption.Who,
  );

  const who = whoOption?.type === ApplicationCommandOptionType.User
    ? whoOption.value
    : user.id;

  const daysOption = options.find(
    (option) => option.name === RemindCommandOption.Days,
  );
  const hoursOption = options.find(
    (option) => option.name === RemindCommandOption.Hours,
  );
  const minutesOption = options.find(
    (option) => option.name === RemindCommandOption.Minutes,
  );

  const [days, hours, minutes] = [
    daysOption,
    hoursOption,
    minutesOption,
  ].map((o) =>
    o?.type === ApplicationCommandOptionType.Integer ? o.value : 0
  ) as [number, number, number];

  const delayInSeconds = (days * 24 * 60 * 60) + (hours * 60 * 60) +
    (minutes * 60);

  if (delayInSeconds <= 0) {
    return {
      responseType: InteractionResponseType.ChannelMessageWithSource,
      responseText: "Please specify at least one of: days, hours, or minutes.",
    };
  }

  const reminderMessage = `<@${who}> ${message}`;

  await db.enqueue(
    {
      type: "reminder",
      message: reminderMessage,
      userId: who,
      channelId,
    } satisfies ReminderMessage,
    {
      delay: delayInSeconds * 1000,
    },
  );

  return {
    responseType: InteractionResponseType.ChannelMessageWithSource,
    responseText: "OK 👌",
  };
}
