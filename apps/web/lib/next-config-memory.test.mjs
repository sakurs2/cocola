import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("../next.config.mjs", import.meta.url), "utf8");

test("HeroUI barrel imports are optimized to keep incremental compilation bounded", () => {
  assert.match(source, /experimental:\s*\{\s*optimizePackageImports:\s*\["@heroui\/react"\]/);
});
