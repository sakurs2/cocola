import assert from "node:assert/strict";
import test from "node:test";
import { inferModelIconSlug } from "./model-icons.ts";

test("infers provider icons from model aliases when icon metadata is missing", () => {
  assert.equal(inferModelIconSlug("deepseek-v4-pro"), "deepseek");
  assert.equal(inferModelIconSlug("qwen3.7-max"), "qwen");
  assert.equal(inferModelIconSlug("GPT-5 Codex"), "codex");
});
