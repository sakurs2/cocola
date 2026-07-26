import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";

const require = createRequire(import.meta.url);
const packageEntry = require.resolve("@assistant-ui/react");
const triggerModule = await import(
  pathToFileURL(join(dirname(packageEntry), "primitives/composer/trigger/detectTrigger.js")).href
);
const { detectTrigger } = triggerModule;

test("wiki mention trigger works after existing text", () => {
  assert.deepEqual(detectTrigger("总结@", "@", "总结@".length), {
    query: "",
    offset: 2,
  });
  assert.deepEqual(detectTrigger("compare@brief", "@", "compare@brief".length), {
    query: "brief",
    offset: 7,
  });
});

test("slash commands still require a whitespace boundary", () => {
  assert.equal(detectTrigger("compare/skill", "/", "compare/skill".length), null);
  assert.deepEqual(detectTrigger("compare /skill", "/", "compare /skill".length), {
    query: "skill",
    offset: 8,
  });
});
