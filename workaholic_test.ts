import { assertEquals } from "@std/assert";
import { assertSnapshot } from "@std/testing";

import {
  formatWorkaholicAddCommand,
  isMessagePartialMatch,
  makeSummaryTable,
  parseMessageForSummary,
  WorkaholicType,
} from "./workaholic.ts";
import { APIMessage } from "$discord-api-types";

Deno.test("isMessagePartialMatch", () => {
  assertEquals(
    isMessagePartialMatch("31 aug OT"),
    true,
  );
  assertEquals(
    isMessagePartialMatch("1 sep ot"),
    true,
  );
  assertEquals(
    isMessagePartialMatch("01 sep OT"),
    true,
  );
  assertEquals(
    isMessagePartialMatch("ot 2 jam"),
    true,
  );
  assertEquals(
    isMessagePartialMatch("OT 1hours"),
    true,
  );
});

Deno.test("isMessagePartialMatch", () => {
  assertEquals(
    isMessagePartialMatch("ot"),
    false,
  );
  assertEquals(
    isMessagePartialMatch("2 jam"),
    false,
  );
  assertEquals(
    isMessagePartialMatch("23 aug"),
    false,
  );
  assertEquals(
    isMessagePartialMatch("```markdown```"),
    false,
  );
});

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
    "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01 ⁃ 7h ⁃ my lorem ipsum work",
  );

  const phFormatted = formatWorkaholicAddCommand({
    duration: 2,
    type: WorkaholicType.PriorityHours,
    userId: "456",
    what: "my lorem ipsum work",
    when: "today",
  });
  assertEquals(
    phFormatted,
    "🐴 ⁃ <@456> ⁃ PH ⁃ today ⁃ 2h ⁃ my lorem ipsum work",
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
  assertEquals(
    parseMessageForSummary(
      { content: "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01 ⁃ 7h" } as APIMessage,
    ),
    null,
  );
  assertEquals(
    parseMessageForSummary(
      { content: "🐴 ⁃ <@123> ⁃ OT ⁃ 2021-01-01" } as APIMessage,
    ),
    null,
  );
  assertEquals(
    parseMessageForSummary(
      { content: "<@123> ⁃ OT ⁃ 2021-01-01" } as APIMessage,
    ),
    null,
  );
});

Deno.test("makeSummaryTable", async (t) => {
  const nicknameByUserId = new Map([
    ["123", "Capybara"],
    ["456", "Yagi"],
    ["789", "Kaffu"],
  ]);
  const getNicknameByUserId = (userId: string) => {
    return nicknameByUserId.get(userId);
  };

  const summary = makeSummaryTable({
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
    getNicknameByUserId,
  });

  await assertSnapshot(t, summary);
});
