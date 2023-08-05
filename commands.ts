import { CommandContext, CommandHandler } from "./command_utils.ts";
import { handleRollCommand, rollCommand } from "./roll.ts";
import { handleWorkaholicCommand, workaholicCommand } from "./workaholic.ts";

export const commands = [rollCommand, workaholicCommand];

const commandHandlerMap: Record<string, CommandHandler> = {
  [rollCommand.name]: handleRollCommand,
  [workaholicCommand.name]: handleWorkaholicCommand,
};

export function handleCommands(
  context: CommandContext,
) {
  const handler = commandHandlerMap[context.interactionData.name];
  return handler ? handler(context) : null;
}
