import { chatCommand, handleChatCommand } from "./chat.ts";
import { CommandContext, CommandHandler } from "./command_utils.ts";
import { e25nCommand, handleE25nCommand } from "./e25n.ts";
import { handleHttpCommand, httpCommand } from "./http.ts";
import { handleRemindCommand, remindCommand } from "./remind.ts";
import { handleRollCommand, rollCommand } from "./roll.ts";
import { handleWorkaholicCommand, workaholicCommand } from "./workaholic.ts";

export const commands = [
  rollCommand,
  workaholicCommand,
  e25nCommand,
  chatCommand,
  httpCommand,
  remindCommand,
];

const commandHandlerMap: Record<string, CommandHandler> = {
  [rollCommand.name]: handleRollCommand,
  [workaholicCommand.name]: handleWorkaholicCommand,
  [e25nCommand.name]: handleE25nCommand,
  [chatCommand.name]: handleChatCommand,
  [httpCommand.name]: handleHttpCommand,
  [remindCommand.name]: handleRemindCommand,
};

export function handleCommands(
  context: CommandContext,
) {
  const handler = commandHandlerMap[context.interactionData.name];
  return handler ? handler(context) : null;
}
