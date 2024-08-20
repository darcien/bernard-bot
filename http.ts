import { CommandContext, makeCommand } from "./command_utils.ts";
import { ApplicationCommandOptionType } from "$discord-api-types";
import { sortBy } from "@std/collections";

enum HttpCommandOption {
  StatusCode = "status_code",
  Variant = "variant",
}

const httpVariants = sortBy([
  {
    label: "cat",
    buildUrl: (statusCode: number) => `https://http.cat/${statusCode}`,
  },
  {
    label: "mdn",
    buildUrl: (statusCode: number) =>
      `https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/${statusCode}`,
  },
  {
    label: "cat2",
    buildUrl: (statusCode: number) => `https://httpcats.com/${statusCode}.webp`,
  },
  {
    label: "dog",
    buildUrl: (statusCode: number) => `https://http.dog/${statusCode}.webp`,
  },
  {
    label: "goat",
    buildUrl: (statusCode: number) =>
      `https://httpgoats.com/${statusCode}.webp`,
  },
  {
    label: "duck",
    buildUrl: (statusCode: number) =>
      `https://httpducks.com/${statusCode}.webp`,
  },
  {
    label: "garden",
    buildUrl: (statusCode: number) => `https://http.garden/${statusCode}.webp`,
  },
  {
    label: "fish",
    buildUrl: (statusCode: number) => `https://http.fish/${statusCode}.webp`,
  },
  {
    label: "pizza",
    buildUrl: (statusCode: number) => `https://http.pizza/${statusCode}.webp`,
  },
], (v) => v.label);

const defaultVariant = httpVariants[0]!;

const httpCommandDefaultValue = {
  [HttpCommandOption.Variant]: defaultVariant.label,
};

export const httpCommand = makeCommand({
  name: "http",
  description: "Explain a HTTP status code with a thousand words",
  options: [
    {
      name: HttpCommandOption.StatusCode,
      description: "Your HTTP response status codes",
      type: ApplicationCommandOptionType.Integer,
      required: true,
      min_value: 100,
      max_value: 599,
    },
    {
      name: HttpCommandOption.Variant,
      description: `Determine the variant of the explanation. Default to ${
        httpCommandDefaultValue[HttpCommandOption.Variant]
      }`,
      type: ApplicationCommandOptionType.String,
      required: false,
      choices: httpVariants.map((v) => ({ name: v.label, value: v.label })),
    },
  ],
});

export function handleHttpCommand(
  { interactionData }: CommandContext,
) {
  const options = interactionData.options || [];

  const variantOption = options.find(
    (option) => option.name === HttpCommandOption.Variant,
  );
  const variant = variantOption?.type === ApplicationCommandOptionType.String
    ? variantOption.value
    : httpCommandDefaultValue[HttpCommandOption.Variant];

  const variantInfo = httpVariants.find((v) => v.label === variant) ||
    defaultVariant;

  const statusCodeOption = options.find(
    (option) => option.name === HttpCommandOption.StatusCode,
  );
  const statusCode =
    statusCodeOption?.type === ApplicationCommandOptionType.Integer
      ? statusCodeOption.value
      : 404;

  return {
    responseText: variantInfo.buildUrl(statusCode),
  };
}
