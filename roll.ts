import { CommandContext, makeCommand } from "./command_utils.ts";
import { ApplicationCommandOptionType } from "./deps.ts";

enum RollCommandOption {
  Sides = "sides",
  Quantity = "quantity",
}

const rollCommandDefaultValue = {
  [RollCommandOption.Sides]: 6,
  [RollCommandOption.Quantity]: 1,
};

export const rollCommand = makeCommand({
  name: "roll",
  description: "Roll a n-sided dice m amount of time(s)",
  options: [
    {
      name: RollCommandOption.Sides,
      description: `The side count of the dice. Default to ${
        rollCommandDefaultValue[RollCommandOption.Sides]
      }`,
      type: ApplicationCommandOptionType.Integer,
      required: false,
      min_value: 1,
      max_value: 100,
    },
    {
      name: RollCommandOption.Quantity,
      description: `The amount of roll. Default to ${
        rollCommandDefaultValue[RollCommandOption.Quantity]
      }`,
      type: ApplicationCommandOptionType.Integer,
      required: false,
      min_value: 1,
      max_value: 200,
    },
  ],
});

export function handleRollCommand(
  { interactionData }: CommandContext,
) {
  const options = interactionData.options || [];

  const sidesOption = options.find(
    (option) => option.name === RollCommandOption.Sides,
  );
  const sides = sidesOption?.type === ApplicationCommandOptionType.Integer
    ? sidesOption.value
    : rollCommandDefaultValue[RollCommandOption.Sides];

  const quantityOption = options.find(
    (option) => option.name === RollCommandOption.Quantity,
  );
  const quantity = quantityOption?.type === ApplicationCommandOptionType.Integer
    ? quantityOption.value
    : rollCommandDefaultValue[RollCommandOption.Quantity];

  const nRoll = () => roll(sides);
  const rollResult = [...Array(quantity)].map(nRoll);

  return {
    responseText: `${rollResult.join(" ")}`,
  };
}

function roll(sides: number) {
  return Math.floor(Math.random() * sides + 1);
}
