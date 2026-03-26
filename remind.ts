import { CommandHandlerResult, makeCommand } from "./command_utils.ts";
import {
  ApplicationCommandOptionType,
  InteractionResponseType,
} from "$discord-api-types";

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

export function handleRemindCommand(): CommandHandlerResult {
  return {
    responseType: InteractionResponseType.ChannelMessageWithSource,
    responseText:
      "Remind is temporarily offline due to the current global economic climate.",
  };
}
