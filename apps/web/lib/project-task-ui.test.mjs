import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [
  projectSource,
  taskSource,
  runtimeSource,
  branchSource,
  sidebarSource,
  workspaceSource,
  workspaceShellSource,
  gitPanelSource,
] = await Promise.all([
  read("../app/projects/[id]/page.tsx"),
  read("../app/projects/[id]/tasks/[conversationId]/page.tsx"),
  read("../app/runtime-provider.tsx"),
  read("../components/assistant-ui/project-branch-control.tsx"),
  read("../components/assistant-ui/heroui-workspace-sidebar.tsx"),
  read("../app/page.tsx"),
  read("../components/assistant-ui/workspace-shell.tsx"),
  read("../components/assistant-ui/workspace-panel.tsx"),
]);

test("new Project tasks expose an editable safe branch before the first message", () => {
  const newProjectTaskSource = runtimeSource.slice(
    runtimeSource.indexOf("const newProjectTask = useCallback"),
    runtimeSource.indexOf("const updatePendingProjectTaskBaseRef"),
  );

  assert.match(projectSource, /<ProjectTaskBranchField/);
  assert.match(projectSource, /updatePendingProjectTaskBranch/);
  assert.match(projectSource, /disabled=\{Boolean\(projectTaskBranchError\(taskBranchName\)\)\}/);
  assert.match(branchSource, /PROJECT_TASK_BRANCH_PREFIX = "cocola\/task-"/);
  assert.match(branchSource, /t\("editableHint"\)/);
  assert.match(runtimeSource, /project_task_branch: projectTaskBranch/);
  assert.match(runtimeSource, /taskBranch: branchName/);
  assert.doesNotMatch(runtimeSource, /pickerEnabled/);
  assert.match(
    newProjectTaskSource,
    /selectAgentRuntime\(\{[\s\S]*?runtimes,[\s\S]*?defaultRuntimeId: defaultAgentRuntimeID/,
  );
  assert.match(newProjectTaskSource, /setSelectedRuntimeIdState\(selected\?\.id \?\? ""\)/);
});

test("Project branch context is borderless and reveals the full truncated name on hover", () => {
  assert.match(branchSource, /<Tooltip delay=\{150\}>/);
  assert.match(branchSource, /<Tooltip\.Content className="max-w-\[22rem\]">/);
  assert.match(branchSource, /break-all font-mono text-xs/);
  assert.doesNotMatch(
    branchSource.slice(branchSource.indexOf("export function ProjectBranchBadge")),
    /border border-border/,
  );
  assert.match(taskSource, /title=\{branchName\}/);
});

test("Project task chrome is compact and workspace starts at its resize minimum", () => {
  assert.match(taskSource, /flex h-10 shrink-0/);
  assert.doesNotMatch(taskSource, /conversation\?\.title \|\| "Task"/);
  assert.doesNotMatch(taskSource, />\/<\/span>/);
  assert.match(workspaceSource, /useState\(480\)/);
  assert.match(workspaceSource, /beginDockResize\(event, workspaceWidth, 480/);
  assert.match(workspaceShellSource, /isProjectTaskPath\(pathname\)/);
  assert.match(workspaceShellSource, /compact=\{compactTopbar\}/);
  assert.match(workspaceShellSource, /compact \? "h-10" : "h-14"/);
  assert.match(workspaceSource, /const showHeaderActions = !isProjectTaskPath\(pathname\)/);
  assert.match(workspaceSource, /showHeaderActions \? <WorkspaceHeaderActions \/> : null/);
});

test("Project conversations appear in Chats with Project routing and identity", () => {
  assert.match(sidebarSource, /const recentConversations = conversations/);
  assert.doesNotMatch(
    sidebarSource,
    /conversations\.filter\(\(conversation\) => !conversation\.project_id\)/,
  );
  assert.match(
    sidebarSource,
    /`\/projects\/\$\{encodeURIComponent\(conversation\.project_id\)\}\/tasks\/\$\{encodeURIComponent\(conversation\.id\)\}`/,
  );
  assert.match(sidebarSource, /conversation\.project_id[\s\S]*?text-indigo-600/);
  assert.match(
    sidebarSource,
    /conversation\.chat_type !== "scheduled_task" && !conversation\.project_id/,
  );
});

test("Project Git review uses a full-width progress rail and centered merge confirmation", () => {
  assert.match(
    gitPanelSource,
    /grid-cols-\[auto_minmax\(1rem,1fr\)_auto_minmax\(1rem,1fr\)_auto\]/,
  );
  assert.match(gitPanelSource, /title=\{t\("mergeTitle"\)\}/);
  assert.match(
    gitPanelSource,
    /<ActionConfirmDialog[\s\S]*?confirmLabel=\{t\("actions\.merge"\)\}/,
  );
  assert.match(gitPanelSource, /groupGitCommitFiles\(files\)/);
  assert.match(gitPanelSource, /group\.directory\.replaceAll\("\/", " \/ "\)/);
  assert.match(gitPanelSource, /text-success">\+\{additions\}/);
  assert.match(gitPanelSource, /text-danger">−\{deletions\}/);
});
