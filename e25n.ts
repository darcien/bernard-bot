import { CommandContext, makeCommand } from "./command_utils.ts";
import { e25n } from "./deps.ts";
import { ApplicationCommandOptionType } from "./deps.ts";

enum E25nCommandOption {
  Input = "input",
  Verbose = "verbose",
}

export const e25nCommand = makeCommand({
  name: "e25n",
  description:
    "Determine what the English Numerical Contraction is for an English word or phrase",
  options: [
    {
      name: E25nCommandOption.Input,
      description: `The input word or phrase. Min 3 characters.`,
      type: ApplicationCommandOptionType.String,
      required: true,
      min_length: 3,
      max_length: 1024,
    },
    {
      name: E25nCommandOption.Verbose,
      description: `Give verbose output including collisions. Default: false.`,
      type: ApplicationCommandOptionType.Boolean,
      required: false,
    },
  ],
});

const gradeByAnnoyingScale = {
  1: {
    emoji: "🏆",
    grade: "Not annoying",
    description:
      "Not annoying. No other English words match this abbreviation (collision) and its shorter to say.",
  },
  2: {
    emoji: "😒",
    grade: "Possibly annoying",
    description:
      "Possibly annoying. No collisions and its the same number of syllables to say.",
  },
  3: {
    emoji: "🙄",
    grade: "Mildly annoying",
    description:
      "Mildly annoying. No collisions and its longer to say OR Some collisions but its easier to say.",
  },
  4: {
    emoji: "😫",
    grade: "Annoying",
    description:
      "Annoying. Some collisions and its the same number (or less) of syallables to say.",
  },
  5: {
    emoji: "😤",
    grade: "Super annoying",
    description:
      "Super annoying. Don't do it. Some collisions and its longer to say.",
  },
};

// Want to use e25n package but import is not working.
// https://github.com/denoland/deno/issues/17784
// TODO: Fix e25n import or rewrite custom implementation
const IS_E25N_MODULE_RESOLUTION_FIXED_YET = false;

export function handleE25nCommand(
  { interactionData }: CommandContext,
) {
  const options = interactionData.options || [];

  const inputOption = options.find(
    (option) => option.name === E25nCommandOption.Input,
  );
  const input = inputOption?.type === ApplicationCommandOptionType.String
    ? inputOption.value
    : null;

  if (!input) {
    return {
      responseText: `Missing input word/phrase`,
    };
  }

  if (!IS_E25N_MODULE_RESOLUTION_FIXED_YET) {
    const inputChars = input.trim().toLowerCase().replaceAll(" ", "").split("");
    const first = inputChars[0];
    const last = inputChars[inputChars.length - 1];
    const middle = inputChars.slice(1, inputChars.length - 1).join("");
    const newWord = `${first}${middle.length}${last}`;
    return {
      responseText: `${input} -- **${newWord}**`,
    };
  }

  const verboseOption = options.find(
    (option) => option.name === E25nCommandOption.Verbose,
  );
  const verbose = verboseOption?.type === ApplicationCommandOptionType.Boolean
    ? verboseOption.value
    : false;

  try {
    const {
      newWord,
      annoyingScale,
      collisions,
    } = e25n(input);

    const { emoji, grade, description } = gradeByAnnoyingScale[annoyingScale];

    if (verbose) {
      return {
        responseText: `${input} -- **${newWord}**
${emoji} -- ${description}
Conflicts with: ${collisions.join(", ")}`,
      };
    }

    return {
      responseText:
        `${input} -- **${newWord}** (${grade} ${emoji}, collisions: ${collisions.length})`,
    };
  } catch (error: unknown) {
    return {
      responseText: `e25n() failed, error: ${
        error instanceof Error ? error.message : String(error)
      }`,
    };
  }
}
