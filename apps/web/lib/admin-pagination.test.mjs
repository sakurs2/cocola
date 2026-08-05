import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const adminUISource = readFileSync(
  new URL("../components/admin/admin-ui.tsx", import.meta.url),
  "utf8",
);
const skillsSource = readFileSync(new URL("../app/admin/skills/page.tsx", import.meta.url), "utf8");
const runsSource = readFileSync(new URL("../app/admin/audit/page.tsx", import.meta.url), "utf8");
const storageSource = readFileSync(
  new URL("../app/admin/storage/page.tsx", import.meta.url),
  "utf8",
);
const sandboxesSource = readFileSync(
  new URL("../app/admin/sandboxes/page.tsx", import.meta.url),
  "utf8",
);
const orphanRouteSource = readFileSync(
  new URL("../app/api/admin/session-storage/orphans/route.ts", import.meta.url),
  "utf8",
);

test("admin collections use the shared visible pagination control", () => {
  assert.match(adminUISource, /export function AdminPagination/);
  assert.match(adminUISource, /Previous page of \$\{label\}/);
  assert.match(adminUISource, /Next page of \$\{label\}/);
  assert.match(skillsSource, /<AdminPagination[\s\S]*label="skills"/);
  assert.match(runsSource, /<AdminPagination[\s\S]*label="runs"/);
  assert.match(storageSource, /<AdminPagination[\s\S]*label="volumes"/);
});

test("admin Skills and Session Storage request bounded server pages", () => {
  assert.match(skillsSource, /const SKILLS_PAGE_SIZE = 24/);
  assert.match(skillsSource, /offset: String\(page \* SKILLS_PAGE_SIZE\)/);
  assert.match(storageSource, /const SESSION_STORAGE_PAGE_SIZE = 25/);
  assert.match(storageSource, /offset: String\(volumePage \* SESSION_STORAGE_PAGE_SIZE\)/);
  assert.match(storageSource, /requested_bytes/);
  assert.match(storageSource, /<AdminPagination[\s\S]*total=\{volumeTotal\}[\s\S]*label="volumes"/);
});

test("Agent Runs fetch one lookahead row so exact full pages cannot open an empty next page", () => {
  assert.match(runsSource, /limit: String\(PAGE_SIZE \+ 1\)/);
  assert.match(runsSource, /setHasNext\(loadedRuns\.length > PAGE_SIZE\)/);
  assert.match(runsSource, /loadedRuns\.slice\(0, PAGE_SIZE\)/);
});

test("destructive admin actions use product dialogs instead of browser confirmation", () => {
  assert.doesNotMatch(storageSource, /window\.confirm|window\.alert|window\.prompt/);
  assert.doesNotMatch(sandboxesSource, /window\.confirm|window\.alert|window\.prompt/);
  assert.match(storageSource, /<AdminConfirmDialog/);
  assert.match(sandboxesSource, /<AdminConfirmDialog/);
});

test("Session Storage can delete every current orphan with one backend request", () => {
  assert.match(storageSource, /Delete all orphans \(\{orphanCount\}\)/);
  assert.match(storageSource, /fetch\(\"\/api\/admin\/session-storage\/orphans\"/);
  assert.match(orphanRouteSource, /proxyAdmin\(req, "\/admin\/session-storage\/orphans"\)/);
});
