import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [projectSource, taskSource, runtimeSource, branchSource, sidebarSource, workspaceSource] =
  await Promise.all([
    read("../app/projects/[id]/page.tsx"),
    read("../app/projects/[id]/tasks/[conversationId]/page.tsx"),
    read("../app/runtime-provider.tsx"),
    read("../components/assistant-ui/project-branch-control.tsx"),
    read("../components/assistant-ui/heroui-workspace-sidebar.tsx"),
    read("../app/page.tsx"),
  ]);

test("new Project tasks expose an editable safe branch before the first message", () => {
  assert.match(projectSource, /<ProjectTaskBranchField/);
  assert.match(projectSource, /updatePendingProjectTaskBranch/);
  assert.match(projectSource, /disabled=\{Boolean\(projectTaskBranchError\(taskBranchName\)\)\}/);
  assert.match(branchSource, /PROJECT_TASK_BRANCH_PREFIX = "cocola\/task-"/);
  assert.match(branchSource, /Editable until the first message/);
  assert.match(runtimeSource, /project_task_branch: projectTaskBranch/);
  assert.match(runtimeSource, /taskBranch: branchName/);
});

test("Project task chrome is compact and workspace starts at its resize minimum", () => {
  assert.match(taskSource, /flex h-10 shrink-0/);
  assert.doesNotMatch(taskSource, /conversation\?\.title \|\| "Task"/);
  assert.doesNotMatch(taskSource, />\/<\/span>/);
  assert.match(workspaceSource, /useState\(480\)/);
  assert.match(workspaceSource, /beginDockResize\(event, workspaceWidth, 480/);
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
