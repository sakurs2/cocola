import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const tasksPageSource = readFileSync(new URL("../app/tasks/page.tsx", import.meta.url), "utf8");

test("isolates copy feedback state from the semantic result badge", () => {
  const resultCell = tasksPageSource.slice(
    tasksPageSource.indexOf("function TaskLastResult"),
    tasksPageSource.indexOf("function TaskResultCopyButton"),
  );

  assert.doesNotMatch(resultCell, /useState/);
  assert.match(resultCell, /<TaskResultCopyButton detail=\{result\.detail\} \/>/);
  assert.match(resultCell, /<Badge variant=\{taskResultBadgeVariant\[result\.tone\]\}>/);
  assert.doesNotMatch(resultCell, /<Chip/);
});
