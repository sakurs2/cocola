import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [
  actionDialogSource,
  deleteDialogSource,
  adminUISource,
  taskDrawerSource,
  modelsSource,
  foldersSource,
  wikiSource,
  skillsSource,
  adminSkillsSource,
] = await Promise.all([
  read("../components/ui/action-dialog.tsx"),
  read("../components/assistant-ui/delete-confirm-dialog.tsx"),
  read("../components/admin/admin-ui.tsx"),
  read("../components/scheduled-tasks/task-drawer.tsx"),
  read("../app/admin/models/page.tsx"),
  read("../app/folders/[id]/page.tsx"),
  read("../components/wiki/wiki-workspace.tsx"),
  read("../app/skills/page.tsx"),
  read("../app/admin/skills/page.tsx"),
]);

test("destructive confirmations use a centered HeroUI alert dialog", () => {
  const actionConfirm = actionDialogSource.slice(
    actionDialogSource.indexOf("export function ActionConfirmDialog"),
    actionDialogSource.indexOf("export function TextInputDialog"),
  );
  const adminConfirm = adminUISource.slice(
    adminUISource.indexOf("export function AdminConfirmDialog"),
    adminUISource.indexOf("export function AdminRefreshButton"),
  );
  const taskConfirm = taskDrawerSource.slice(
    taskDrawerSource.indexOf("export function TaskConfirmDialog"),
  );

  assert.match(actionConfirm, /<AlertDialog/);
  assert.match(actionConfirm, /<AlertDialog\.Container[^>]*placement="center"[^>]*size="sm"/);
  assert.doesNotMatch(actionConfirm, /<Sheet|placement="right"/);
  assert.match(deleteDialogSource, /<ActionConfirmDialog/);
  assert.doesNotMatch(deleteDialogSource, /<Sheet|placement="right"/);
  assert.match(adminConfirm, /<ActionConfirmDialog/);
  assert.doesNotMatch(adminConfirm, /<Sheet|placement="right"/);
  assert.match(taskConfirm, /<ActionConfirmDialog/);
  assert.doesNotMatch(taskConfirm, /<Sheet|placement="right"/);
});

test("delete entry points open the shared centered confirmation", () => {
  assert.match(modelsSource, /<AdminConfirmDialog/);
  assert.match(foldersSource, /<DeleteConfirmDialog/);
  assert.match(wikiSource, /<DeleteConfirmDialog/);
  assert.match(skillsSource, /setDeleteTarget\(skill\)/);
  assert.match(skillsSource, /<DeleteConfirmDialog/);
  assert.match(adminSkillsSource, /setDeleteTarget\(skill\)/);
  assert.match(adminSkillsSource, /<AdminConfirmDialog/);
  assert.doesNotMatch(foldersSource, /Close delete confirmation/);
  assert.doesNotMatch(wikiSource, /Close delete confirmation/);
});

test("model kind cards can shrink without overlapping", () => {
  assert.match(modelsSource, /sm:grid-cols-\[repeat\(2,minmax\(0,1fr\)\)\]/);
  assert.equal(modelsSource.match(/h-auto min-w-0 w-full flex-col items-stretch/g)?.length, 2);
  assert.match(modelsSource, /For Agent conversations\./);
  assert.match(modelsSource, /For Memory and knowledge\./);
});
