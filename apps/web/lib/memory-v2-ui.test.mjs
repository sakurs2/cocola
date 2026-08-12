import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const adminSource = readFileSync(
  new URL("../app/admin/toolbox/memory-tool.tsx", import.meta.url),
  "utf8",
);
const profileSource = readFileSync(
  new URL("../components/profile/memory-panel.tsx", import.meta.url),
  "utf8",
);

test("Admin Memory uses a compact modal and central reset confirmation", () => {
  assert.match(adminSource, /<Modal[\s\S]*placement="center"/);
  assert.match(adminSource, /<ActionConfirmDialog[\s\S]*title=\{t\("resetTitle"\)\}/);
  assert.match(adminSource, /label=\{t\("extractionModel"\)\}/);
  assert.match(adminSource, /label=\{t\("embeddingModel"\)\}/);
  assert.match(adminSource, /patchMemoryConfig/);
  assert.match(adminSource, /disabled=\{saving \|\| resetting \|\| config\.resetting\}/);
  assert.doesNotMatch(adminSource, /AdminDrawer|<Sheet|window\.confirm/);
});

test("Profile Memory exposes user controls and all supported categories", () => {
  assert.match(profileSource, /label=\{t\("use"\)\}/);
  assert.match(profileSource, /label=\{t\("learn"\)\}/);
  for (const category of ["profile", "preferences", "entities", "events"]) {
    assert.match(profileSource, new RegExp(`id: "${category}"`));
  }
  assert.match(profileSource, /title=\{t\("clearTitle"\)\}/);
  assert.match(profileSource, /title=\{t\("deleteTitle"\)\}/);
  assert.match(profileSource, /AbortController/);
  assert.match(profileSource, /next_cursor/);
  assert.match(profileSource, /t\("loadMore"\)/);
  assert.doesNotMatch(profileSource, /Coming soon|window\.confirm|<Sheet/);
});
