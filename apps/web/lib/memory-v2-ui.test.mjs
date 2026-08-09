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
  assert.match(adminSource, /<ActionConfirmDialog[\s\S]*title="Reset all Memory\?"/);
  assert.match(adminSource, /Extraction model/);
  assert.match(adminSource, /Embedding model/);
  assert.match(adminSource, /patchMemoryConfig/);
  assert.match(adminSource, /disabled=\{saving \|\| resetting \|\| config\.resetting\}/);
  assert.doesNotMatch(adminSource, /AdminDrawer|<Sheet|window\.confirm/);
});

test("Profile Memory exposes user controls and all supported categories", () => {
  assert.match(profileSource, /label="Use memory"/);
  assert.match(profileSource, /label="Learn from conversations"/);
  for (const category of ["Profile", "Preferences", "Entities", "Events"]) {
    assert.match(profileSource, new RegExp(`label: "${category}"`));
  }
  assert.match(profileSource, /title="Clear all personal memory\?"/);
  assert.match(profileSource, /title="Delete this memory\?"/);
  assert.match(profileSource, /AbortController/);
  assert.match(profileSource, /next_cursor/);
  assert.match(profileSource, /Load more/);
  assert.doesNotMatch(profileSource, /Coming soon|window\.confirm|<Sheet/);
});
