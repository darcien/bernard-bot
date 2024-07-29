import { assertEquals } from "@std/assert";
import { addParamsToUrl, makeDiscordApiUrl } from "./discord_api.ts";

Deno.test("makeDiscordApiUrl", () => {
  const url = makeDiscordApiUrl("/guilds/321181582620884992/members");
  assertEquals(
    url,
    "https://discord.com/api/v10/guilds/321181582620884992/members",
  );
});

Deno.test("addParamsToUrl", () => {
  const url = addParamsToUrl("https://example.com/foo", {
    bar: "baz",
    qux: 7,
    absent: undefined,
    bool: true,
  });
  assertEquals(
    url,
    "https://example.com/foo?bar=baz&qux=7&bool=true",
  );
});
