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
const demoStyles = await readFile(new URL("../app/cocola-web-demo.css", import.meta.url), "utf8");
const profileSource = await readFile(
  new URL("../components/profile/profile-page-content.tsx", import.meta.url),
  "utf8",
);
const newProjectSource = await readFile(
  new URL("../app/projects/new/page.tsx", import.meta.url),
  "utf8",
);
test("workspace header aligns its title with page content while keeping global actions at the edge", () => {
  assert.match(
    source,
    /cocola-workspace-topbar-content[\s\S]*?mx-auto w-full max-w-7xl px-4 sm:px-6 lg:px-8/,
  );
  assert.match(
    source,
    /cocola-workspace-topbar-actions absolute right-4 top-1\/2 -translate-y-1\/2/,
  );
  assert.match(source, /const fullWidthTopbar = immersive \|\| pathname === "\/wiki"/);
  assert.match(source, /fullWidthTopbar \? "w-full px-4"/);
  assert.match(
    demoStyles,
    /\.cocola-user-ui\.cocola-web-shell \.app-layout__main\s*\{\s*scrollbar-gutter: stable;/,
  );
});

test("workspace pages keep their core content centered at a stable maximum width", () => {
  assert.match(pageFrameSource, /mx-auto flex w-full max-w-7xl/);
  assert.doesNotMatch(pageFrameSource, /layout = "fluid"|layout\?: "fluid" \| "content"/);
  assert.match(profileSource, /<WorkspacePageFrame>/);
  assert.match(newProjectSource, /<WorkspacePageFrame>/);
});

test("shared workspace catalogs use responsive page-specific columns", () => {
  assert.match(
    pageFrameSource,
    /cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-3/,
  );
});
