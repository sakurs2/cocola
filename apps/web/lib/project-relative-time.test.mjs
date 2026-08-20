import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const projectListSource = await readFile(
  new URL("../app/projects/page.tsx", import.meta.url),
  "utf8",
);

test("Project relative timestamps use one explicit clock", () => {
  assert.match(projectListSource, /const now = useNow\(\{ updateInterval: 60_000 \}\)/);
  assert.match(
    projectListSource,
    /formatProjectTime\(project\.updated_at, format, now, t\("recently"\)\)/,
  );
  assert.match(projectListSource, /now\.getTime\(\) - then/);
  assert.equal(projectListSource.match(/format\.relativeTime\([^\n]+\{ now, unit:/g)?.length, 3);
  assert.doesNotMatch(projectListSource, /Date\.now\(\) - then/);
});
