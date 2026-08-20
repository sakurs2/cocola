import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(
  new URL("../components/assistant-ui/workspace-shell.tsx", import.meta.url),
  "utf8",
);
const pageFrameSource = await readFile(
  new URL("../components/heroui-workspace/workspace-ui.tsx", import.meta.url),
  "utf8",
);
const globalStyles = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
const profileSource = await readFile(
  new URL("../components/profile/profile-page-content.tsx", import.meta.url),
  "utf8",
);
const newProjectSource = await readFile(
  new URL("../app/projects/new/page.tsx", import.meta.url),
  "utf8",
);
const resourceCatalogSources = await Promise.all(
  [
    "../app/agents/page.tsx",
    "../app/mcps/page.tsx",
    "../app/skills/page.tsx",
    "../app/projects/page.tsx",
    "../app/projects/[id]/page.tsx",
    "../app/admin/skills/page.tsx",
    "../app/admin/mcps/page.tsx",
    "../app/admin/toolbox/toolbox-client.tsx",
  ].map((path) => readFile(new URL(path, import.meta.url), "utf8")),
);

test("workspace header keeps global actions aligned to the viewport edge", () => {
  assert.match(source, /flex w-full items-center gap-3 px-4/);
  assert.doesNotMatch(source, /mx-auto max-w-7xl px-4 sm:px-6 lg:px-8/);
});

test("workspace pages separate fluid work canvases from readable content", () => {
  assert.match(pageFrameSource, /layout = "fluid"/);
  assert.match(pageFrameSource, /layout === "content" \? "mx-auto max-w-5xl" : ""/);
  assert.doesNotMatch(pageFrameSource, /max-w-\[100rem\]|max-w-7xl/);
  assert.match(profileSource, /<WorkspacePageFrame layout="content">/);
  assert.match(newProjectSource, /<WorkspacePageFrame layout="content">/);
});

test("resource catalog cards stay fixed-width, left-aligned, and responsive", () => {
  assert.match(
    globalStyles,
    /\.cocola-resource-card-grid\s*\{[\s\S]*?display: flex;[\s\S]*?flex-wrap: wrap;[\s\S]*?justify-content: start;[\s\S]*?gap: 1rem/,
  );
  assert.match(
    globalStyles,
    /\.cocola-resource-card-grid > \*\s*\{[\s\S]*?flex: 0 0 min\(100%, 20rem\);[\s\S]*?width: min\(100%, 20rem\)/,
  );
  assert.match(pageFrameSource, /cocola-web-catalog-grid cocola-resource-card-grid/);
  resourceCatalogSources.forEach((catalogSource) => {
    assert.match(catalogSource, /cocola-resource-card-grid/);
  });
});
