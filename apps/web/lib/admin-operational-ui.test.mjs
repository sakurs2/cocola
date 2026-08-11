import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [
  adminUISource,
  actionDialogSource,
  globalStyles,
  auditSource,
  nodesSource,
  sandboxesSource,
  settingsSource,
  storageSource,
  mcpsSource,
  modelsSource,
  scheduledTasksSource,
  tokenUsageSource,
  usersSource,
] = await Promise.all([
  read("../components/admin/admin-ui.tsx"),
  read("../components/ui/action-dialog.tsx"),
  read("../app/globals.css"),
  read("../app/admin/audit/page.tsx"),
  read("../app/admin/sandbox-nodes/page.tsx"),
  read("../app/admin/sandboxes/page.tsx"),
  read("../app/admin/settings/page.tsx"),
  read("../app/admin/storage/page.tsx"),
  read("../app/admin/mcps/page.tsx"),
  read("../app/admin/models/page.tsx"),
  read("../app/admin/scheduled-tasks/page.tsx"),
  read("../app/admin/token-usage/page.tsx"),
  read("../app/admin/users/page.tsx"),
]);

test("Admin operational tables expose a viewport-bottom synchronized scrollbar", () => {
  assert.match(adminUISource, /export function AdminDataGrid/);
  assert.match(adminUISource, /admin-data-grid-scroll/);
  assert.match(adminUISource, /admin-data-grid-floating-scrollbar fixed bottom-0/);
  assert.match(adminUISource, /floating\.scrollLeft = scroller\.scrollLeft/);
  assert.match(adminUISource, /scroller\.scrollLeft = floating\.scrollLeft/);
  assert.match(adminUISource, /rect\.bottom > viewportBottom \+ 1/);
  assert.match(globalStyles, /\.admin-data-grid-scroll[\s\S]*?overflow-x: scroll/);
  assert.match(globalStyles, /\.admin-data-grid-floating-scrollbar\[data-visible="true"\]/);
  assert.match(auditSource, /<AdminDataGrid/);
});

test("Admin row actions move with the table and use a compact HeroUI menu", () => {
  assert.match(adminUISource, /export function AdminRowActions[\s\S]*?<Dropdown>/);
  assert.match(
    adminUISource,
    /<Dropdown\.Popover className="w-max min-w-0 md:min-w-0" placement="bottom end">/,
  );
  for (const source of [nodesSource, sandboxesSource, storageSource]) {
    assert.match(source, /<AdminRowActions/);
    assert.doesNotMatch(source, /pinned:\s*"end"/);
  }
  assert.match(sandboxesSource, /<AdminTruncatedValue/);
});

test("Admin errors use a centered HeroUI error dialog", () => {
  assert.match(adminUISource, /export function AdminErrorDialog[\s\S]*?<AlertDialog/);
  assert.match(adminUISource, /<AlertDialog\.Icon status="danger">/);
  for (const source of [auditSource, nodesSource, sandboxesSource, settingsSource, storageSource]) {
    assert.match(source, /<AdminErrorDialog/);
  }
});

test("Quantity settings separate the numeric value from MB and GB", () => {
  assert.match(settingsSource, /grid-cols-\[minmax\(0,1fr\)_96px\]/);
  assert.match(settingsSource, /value: "MB", label: "MB"/);
  assert.match(settingsSource, /value: "GB", label: "GB"/);
  assert.match(settingsSource, /quantity\.unit === "GB" \? "Gi" : "Mi"/);
});

test("Missing storage uses the stale cleanup flow without offering a rebuild action", () => {
  assert.match(storageSource, /Stale binding can be cleaned/);
  assert.match(storageSource, /A fresh volume is created on the next run/);
  assert.match(storageSource, /Clean stale binding/);
  assert.match(storageSource, /Delete orphan volume/);
  assert.doesNotMatch(storageSource, /Clean up all|No cleanup needed/);
  assert.doesNotMatch(storageSource, /Rebuild empty Volume|Recreate Volume/);
});

test("Storage measurement reports progress and results in a centered HeroUI card", () => {
  assert.match(adminUISource, /export function AdminToast[\s\S]*?<Card/);
  assert.match(
    storageSource,
    /setToast\(\{ message: "Measuring volume usage…", tone: "loading" \}\)/,
  );
  assert.match(storageSource, /method: "POST"/);
  assert.match(storageSource, /Measured \$\{formatBytes\(result\.allocated_bytes\)\}/);
  assert.match(storageSource, /measurement: result/);
  assert.match(storageSource, /const measurement = volume\.measurement/);
  assert.match(storageSource, /groupVolumesByNode\(targets\)/);
  assert.match(storageSource, /for \(const volume of group\)/);
  assert.match(storageSource, /index \+= 4/);
  assert.match(storageSource, /Measure page/);
  assert.match(storageSource, /<AdminToast/);
  assert.doesNotMatch(storageSource, /Session requests|No cleanup needed/);
});

test("Session storage exposes safe orphan deletion without ambiguous cleanup copy", () => {
  assert.match(storageSource, /orphan_count\?: number/);
  assert.match(storageSource, /Delete orphans \(\{orphanCount\}\)/);
  assert.match(storageSource, /fetch\("\/api\/admin\/session-storage\/orphans"/);
  assert.match(storageSource, /Active Session Volumes are not affected/);
  assert.doesNotMatch(storageSource, /Clean up all|No cleanup needed/);
});

test("Session storage keeps only user-facing operational columns", () => {
  assert.doesNotMatch(storageSource, /header: "Generation"/);
  assert.doesNotMatch(storageSource, /header: "Last reset"/);
  assert.match(storageSource, /contentClassName="min-w-\[940px\]"/);
  for (const header of [
    "Session / User",
    "Node",
    "Volume",
    "Requested",
    "Actual usage",
    "Actions",
  ]) {
    assert.match(storageSource, new RegExp(`header: "${header}"`));
  }
});

test("Admin runtime surfaces keep feedback compact and omit inapplicable Compose controls", () => {
  assert.match(sandboxesSource, /<AdminToast/);
  assert.doesNotMatch(sandboxesSource, /Sandbox runtime state refreshed[\s\S]*?rounded-xl border/);
  assert.match(nodesSource, /const composeOnly =/);
  assert.match(nodesSource, /column\.id !== "capacity" && column\.id !== "actions"/);
  assert.match(nodesSource, /formatMemoryQuantity\(node\.memory_allocatable\)/);
  assert.match(storageSource, /\{formatPercent\(availableRatio\)\} available/);
  assert.doesNotMatch(storageSource, /formatPercent\(occupiedRatio\)\} unavailable/);
});

test("Sandbox rows keep one-line summaries and move diagnostics into a centered details modal", () => {
  for (const header of ["Sandbox", "Status", "Owner", "Node", "Created", "Actions"]) {
    assert.match(sandboxesSource, new RegExp('header: "' + header + '"'));
  }
  assert.doesNotMatch(
    sandboxesSource,
    /header: "Session \/ User"|header: "Runtime"|header: "Node \/ Pod"/,
  );
  assert.match(sandboxesSource, /label: "View details"/);
  assert.match(sandboxesSource, /<Modal\.Container placement="center"/);
  assert.match(sandboxesSource, /label="Runtime image"/);
  assert.match(sandboxesSource, /label="Session ID"/);
});

test("Empty MCP state has one create action and no redundant search control", () => {
  assert.match(mcpsSource, /loading \|\| mcps\.length > 0/);
  assert.match(mcpsSource, /<AdminToast message=\{notice\}/);
  assert.match(mcpsSource, /No MCP servers configured/);
});

test("Truncated Admin table values share tooltip, navigation, and visible copy affordances", () => {
  assert.match(adminUISource, /href\?: string/);
  assert.match(adminUISource, /onPress\?: \(\) => void/);
  assert.match(adminUISource, /group inline-flex min-w-0 max-w-full items-center align-middle/);
  assert.match(adminUISource, /className="size-6 min-w-6 shrink-0 opacity-70/);
  assert.doesNotMatch(adminUISource, /group flex min-w-0 flex-1 items-center gap-1/);
  assert.match(adminUISource, /opacity-70 transition-opacity/);
  for (const source of [
    auditSource,
    modelsSource,
    nodesSource,
    sandboxesSource,
    scheduledTasksSource,
    storageSource,
    tokenUsageSource,
    usersSource,
    settingsSource,
    mcpsSource,
  ]) {
    assert.match(source, /<AdminTruncatedValue/);
  }
  assert.doesNotMatch(modelsSource, /max-w-64 truncate font-mono text-xs/);
  assert.doesNotMatch(tokenUsageSource, /block min-w-0 truncate text-sm font-semibold/);
  assert.doesNotMatch(scheduledTasksSource, /block truncate font-semibold/);
  assert.match(usersSource, /function CredentialRow[\s\S]*?<AdminTruncatedValue/);
});

test("Admin confirmation loading icons stay centered in every shared confirmation dialog", () => {
  assert.match(actionDialogSource, /className="inline-grid place-items-center"/);
  assert.match(actionDialogSource, /className="col-start-1 row-start-1 size-4 animate-spin"/);
  assert.doesNotMatch(actionDialogSource, /absolute left-1\/2 top-1\/2/);
});
