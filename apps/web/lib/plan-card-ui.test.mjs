import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const planCardSource = readFileSync(
  new URL("../components/assistant-ui/plan-card.tsx", import.meta.url),
  "utf8",
);

test("Plan Card uses the compact HeroUI composition", () => {
  assert.match(planCardSource, /from "@heroui\/react"/);
  assert.match(planCardSource, /<Card[\s\S]*?<Card\.Header/);
  assert.match(planCardSource, /<Chip color=\{statusView\.color\} size="sm" variant="soft">/);
  assert.match(planCardSource, /<Card\.Content className="px-4 py-3">/);
  assert.match(planCardSource, /<Card\.Footer className="flex-col items-stretch gap-3 px-4 py-3/);
  assert.match(planCardSource, /<Dropdown\.Menu aria-label=\{t\("moreActions"\)\}/);
  assert.match(planCardSource, /<Separator \/>/);
  assert.match(planCardSource, /className="my-4 w-full max-w-none/);
  assert.match(planCardSource, /<TextField[\s\S]*?<TextArea/);
  assert.match(planCardSource, /t\("generateRevision"\)/);
  assert.match(planCardSource, /revisePlan\(plan, feedback\)/);
});

test("Plan Card no longer carries the legacy hand-styled surface", () => {
  assert.doesNotMatch(planCardSource, /components\/ui\/dropdown-menu/);
  assert.doesNotMatch(planCardSource, /statusView\.(accent|badge|frame|header|iconFrame)/);
  assert.doesNotMatch(planCardSource, /<button/);
  assert.doesNotMatch(planCardSource, /absolute inset-y-0 left-0 w-1/);
  assert.doesNotMatch(planCardSource, /max-w-\[720px\]/);
});
