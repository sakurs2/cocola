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

test("Wiki paths use a compact filesystem breadcrumb", () => {
  assert.match(source, /function displayWikiPath/);
  assert.match(source, /aria-label=\{t\("allFiles"\)\}[\s\S]*?>\s*\/\s*<\/Button>/);
  assert.match(source, /max-w-24 truncate[\s\S]*?<Tooltip\.Content>\{folder\.name\}/);
  assert.match(source, /displayWikiPath\(folder\.logical_path\)/);
  assert.doesNotMatch(source, />\{t\("allFiles"\)\}<\/Button>/);
});

test("Wiki folder contents use compact fixed-width tiles and a fluid canvas", () => {
  assert.match(source, /className="flex w-full flex-col px-5 py-5 lg:px-6 lg:py-6"/);
  assert.match(source, /cocola-web-wiki-node-card[\s\S]*?min-h-16 w-64/);
  assert.doesNotMatch(source, /mx-auto flex w-full max-w-5xl/);
  assert.doesNotMatch(source, /<Card className="cocola-web-wiki-node-card h-full p-4">/);
});

test("Wiki sidebar exposes a bounded resize separator", () => {
  assert.match(source, /role="separator"/);
  assert.match(source, /onPointerMove=\{resizeSidebar\}/);
  assert.match(source, /MIN_SIDEBAR_WIDTH/);
  assert.match(source, /MAX_SIDEBAR_WIDTH/);
  assert.match(source, /onKeyDown=\{resizeSidebarWithKeyboard\}/);
  assert.match(source, /w-full[\s\S]*?lg:w-\[var\(--cocola-wiki-sidebar-width\)\]/);
  assert.doesNotMatch(source, /style=\{\{ width: sidebarWidth \}\}/);
});

test("Wiki directory actions use one contextual create menu", () => {
  assert.match(source, /<Dropdown\.Trigger[\s\S]*?t\("new"\)/);
  assert.match(source, /<Dropdown\.Item id="folder"/);
  assert.match(source, /<Dropdown\.Item id="page"/);
  assert.match(source, /<Dropdown\.Item id="upload"/);
  assert.doesNotMatch(source, /function QuickAction|t\("fileLimit"\)/);
});

test("Wiki refresh exposes its loading state on every request", () => {
  assert.match(source, /const loadTree = useCallback[\s\S]*?setLoading\(true\)/);
  assert.match(source, /aria-busy=\{loading\}/);
  assert.match(source, /isDisabled=\{loading\}/);
  assert.match(source, /loading && "animate-spin"/);
  assert.match(source, /const succeeded = await loadTree\(\)/);
  assert.match(source, /refreshConfirmed \? t\("refreshed"\) : t\("refresh"\)/);
  assert.match(source, /REFRESH_FEEDBACK_MS/);
});

test("Markdown pages wait for content before enabling the editor", () => {
  assert.match(source, /const \[contentLoaded, setContentLoaded\] = useState\(!markdown\)/);
  assert.match(source, /state === "loading" && !contentLoaded/);
  assert.match(source, /state === "load-error"/);
  assert.match(source, /t\("tryAgain"\)/);
  assert.doesNotMatch(source, /const loaded = useRef/);
});
