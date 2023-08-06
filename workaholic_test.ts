import { assertEquals } from "https://deno.land/std@0.197.0/assert/mod.ts";
import { assertSnapshot } from "https://deno.land/std@0.197.0/testing/snapshot.ts";

import {
  formatSummary,
  formatWorkaholicAddCommand,
  parseMessageForSummary,
  WorkaholicType,
} from "./workaholic.ts";
import { APIMessage } from "./deps.ts";

Deno.test("formatWorkaholicAddCommand", () => {
  const formatted = formatWorkaholicAddCommand({
    duration: 7,
    type: WorkaholicType.Overtime,
    userId: "123",
    what: "my lorem ipsum work",
    when: "2021-01-01",
  });
  assertEquals(
    formatted,
    "🐴<@123>Overtime2021-01-017hmy lorem ipsum work",
  );
});

Deno.test("parseMessageForSummary", () => {
  const userInput = {
    duration: 7,
    type: WorkaholicType.Overtime,
    userId: "123",
    what: "my lorem ipsum work",
    when: "2021-01-01",
  };
  const formatted = formatWorkaholicAddCommand(userInput);
  const parsed = parseMessageForSummary({ content: formatted } as APIMessage);

  assertEquals(
    parsed,
    {
      ...userInput,
      duration: String(userInput.duration),
    },
  );

  const bad = parseMessageForSummary({ content: "🐴" } as APIMessage);
  assertEquals(
    bad,
    null,
  );
});

Deno.test("formatSummary", async (t) => {
  const summary = formatSummary({
    workaholicMessages: [
      {
        when: "2 aug",
        what: "do something cool",
        duration: "2h",
        type: WorkaholicType.Overtime,
        userId: "123",
      },
      {
        when: "3 aug",
        what: "fix something awesome",
        duration: "1h",
        type: WorkaholicType.Overtime,
        userId: "123",
      },
      {
        when: "7 aug",
        what: "prod incident something something",
        duration: "3h",
        type: WorkaholicType.Overtime,
        userId: "456",
      },
      {
        when: "17 aug",
        what: "its holiday and im working",
        duration: "8h",
        type: WorkaholicType.PriorityHours,
        userId: "456",
      },
      {
        when: "30 aug",
        what: "need to finish this thing for showcase",
        duration: "2h",
        type: WorkaholicType.Overtime,
        userId: "789",
      },
    ],
    nicknameByUserId: new Map([
      ["123", "Capybara"],
      ["456", "Yagi"],
      ["789", "Kaffu"],
    ]),
  });

  await assertSnapshot(t, summary);
});
