import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(
  new URL("../components/wiki/wiki-workspace.tsx", import.meta.url),
  "utf8",
);

test("Wiki folders use directory navigation instead of an expandable tree", () => {
  assert.match(source, /currentFolderID/);
  assert.match(source, /navigateToFolder/);
  assert.match(source, /WikiNavigationRow/);
  assert.doesNotMatch(source, /setExpanded|WikiTreeRow|Collapse folder|Expand folder/);
});

test("Wiki sidebar exposes a bounded resize separator", () => {
  assert.match(source, /role="separator"/);
  assert.match(source, /onPointerMove=\{resizeSidebar\}/);
  assert.match(source, /MIN_SIDEBAR_WIDTH/);
  assert.match(source, /MAX_SIDEBAR_WIDTH/);
  assert.match(source, /onKeyDown=\{resizeSidebarWithKeyboard\}/);
});

test("Markdown pages wait for content before enabling the editor", () => {
  assert.match(source, /const \[contentLoaded, setContentLoaded\] = useState\(!markdown\)/);
  assert.match(source, /state === "loading" && !contentLoaded/);
  assert.match(source, /state === "load-error"/);
  assert.match(source, /Try again/);
  assert.doesNotMatch(source, /const loaded = useRef/);
});
