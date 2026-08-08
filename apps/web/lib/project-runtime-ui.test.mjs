import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [newProjectSource, projectSource] = await Promise.all([
  read("../app/projects/new/page.tsx"),
  read("../app/projects/[id]/page.tsx"),
]);

test("Project creation keeps Agent Runtime selection internal", () => {
  assert.doesNotMatch(
    newProjectSource,
    /runtimePickerEnabled|defaultAgentRuntimeID|runtimeConfigError|runtime_id/,
  );
  assert.doesNotMatch(newProjectSource, /Default Agent Runtime|Choose the runtime used/);
  assert.match(newProjectSource, /Name and description can be changed later\./);
  assert.match(newProjectSource, /Create project/);
});

test("Project details do not expose runtime configuration", () => {
  assert.doesNotMatch(projectSource, /runtimePickerEnabled|draftRuntime|Default runtime/);
  assert.doesNotMatch(projectSource, /label="Runtime"|Project runtimes|name, runtime/);
  assert.match(
    projectSource,
    /newProjectTask\(project\.id, project\.runtime_id, selectedBaseRef\)/,
  );
  assert.match(projectSource, /Update this Project’s name and description\./);
});
