import {
  APIChatInputApplicationCommandInteractionData,
  ApplicationCommandOptionType,
  RESTPostAPIChatInputApplicationCommandsJSONBody,
} from "https://deno.land/x/discord_api_types@0.37.19/v10.ts";

function makeCommand(
  c: RESTPostAPIChatInputApplicationCommandsJSONBody
): RESTPostAPIChatInputApplicationCommandsJSONBody {
  return c;
}

export enum CommandName {
  Roll = "roll",
}

export enum RollCommandOption {
  Sides = "sides",
  Quantity = "quantity",
}

export const rollCommandDefaultValue = {
  [RollCommandOption.Sides]: 6,
  [RollCommandOption.Quantity]: 1,
};

const rollCommand = makeCommand({
  name: CommandName.Roll,
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
  data: APIChatInputApplicationCommandInteractionData
) {
  const options = data.options || [];

  const sidesOption = options.find(
    (option) => option.name === RollCommandOption.Sides
  );
  const sides =
    sidesOption?.type === ApplicationCommandOptionType.Integer
      ? sidesOption.value
      : rollCommandDefaultValue[RollCommandOption.Sides];

  const quantityOption = options.find(
    (option) => option.name === RollCommandOption.Sides
  );
  const quantity =
    quantityOption?.type === ApplicationCommandOptionType.Integer
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

export const commands = [rollCommand];
export const knownCommands = new Set(commands.map((c) => c.name));
