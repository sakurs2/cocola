import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const loginSource = await readFile(new URL("../app/login/page.tsx", import.meta.url), "utf8");

test("the password visibility control is centered within the input itself", () => {
  assert.match(
    loginSource,
    /<div className="relative">[\s\S]*?autoComplete="current-password"[\s\S]*?className="w-full pr-11"[\s\S]*?className="absolute inset-y-0 right-1 my-auto"/,
  );
  assert.doesNotMatch(loginSource, /className="absolute bottom-[^"]*right-/);
});
