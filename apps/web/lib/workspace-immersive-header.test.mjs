import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(
  new URL("../components/assistant-ui/workspace-shell.tsx", import.meta.url),
  "utf8",
);

test("immersive workspace header uses the full viewport width", () => {
  assert.match(source, /immersive \? "px-4" : "mx-auto max-w-7xl px-4 sm:px-6 lg:px-8"/);
});
