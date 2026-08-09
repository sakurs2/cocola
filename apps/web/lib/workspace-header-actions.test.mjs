import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const actionsSource = await readFile(
  new URL("../components/assistant-ui/workspace-header-actions.tsx", import.meta.url),
  "utf8",
);
const chatSource = await readFile(new URL("../app/page.tsx", import.meta.url), "utf8");
const workspaceShellSource = await readFile(
  new URL("../components/assistant-ui/workspace-shell.tsx", import.meta.url),
  "utf8",
);
const adminShellSource = await readFile(
  new URL("../components/admin/admin-shell.tsx", import.meta.url),
  "utf8",
);
const workspaceRoutesSource = await readFile(
  new URL("./workspace-routes.ts", import.meta.url),
  "utf8",
);

test("workspace headers expose one safe Cocola GitHub link beside the theme toggle", () => {
  assert.match(actionsSource, /https:\/\/github\.com\/sakurs2\/cocola/);
  assert.match(actionsSource, /const GITHUB_LINK_LABEL = "Go to GitHub page"/);
  assert.match(actionsSource, /aria-label=\{GITHUB_LINK_LABEL\}/);
  assert.match(
    actionsSource,
    /<Tooltip\.Content placement="bottom end">\{GITHUB_LINK_LABEL\}<\/Tooltip\.Content>/,
  );
  assert.match(actionsSource, /window\.open\(COCOLA_GITHUB_URL, "_blank", "noopener,noreferrer"\)/);
  assert.match(actionsSource, /<Button[\s\S]*?<GitHubIcon[\s\S]*?<\/Button>/);
  assert.match(actionsSource, /<GitHubIcon[\s\S]*?<WorkspaceThemeToggle \/>/);

  for (const source of [chatSource, workspaceShellSource, adminShellSource]) {
    assert.match(source, /<WorkspaceHeaderActions \/>/);
  }
});

test("Project task pages keep the global header actions in only the workspace topbar", async () => {
  const projectTaskSource = await readFile(
    new URL("../app/projects/[id]/tasks/[conversationId]/page.tsx", import.meta.url),
    "utf8",
  );

  assert.match(projectTaskSource, /<Home \/>/);
  assert.match(workspaceRoutesSource, /PROJECT_TASK_PATH/);
  assert.match(chatSource, /const showHeaderActions = !isProjectTaskPath\(pathname\)/);
  assert.match(chatSource, /showHeaderActions \? <WorkspaceHeaderActions \/> : null/);
  assert.match(workspaceShellSource, /const compactTopbar = isProjectTaskPath\(pathname\)/);
});
