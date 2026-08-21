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
  traceSource,
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
  read("../app/admin/traces/[traceId]/page.tsx"),
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
  assert.match(storageSource, /t\("states\.staleBinding"\)/);
  assert.match(storageSource, /t\("states\.freshNextRun"\)/);
  assert.match(storageSource, /t\("actions\.cleanBinding"\)/);
  assert.match(storageSource, /t\("actions\.deleteOrphanVolume"\)/);
  assert.doesNotMatch(storageSource, /Clean up all|No cleanup needed/);
  assert.doesNotMatch(storageSource, /Rebuild empty Volume|Recreate Volume/);
});

test("Storage measurement reports progress and results in a centered HeroUI card", () => {
  assert.match(adminUISource, /export function AdminToast[\s\S]*?<Card/);
  assert.match(
    storageSource,
    /setToast\(\{ message: t\("toast\.measuring"\), tone: "loading" \}\)/,
  );
  assert.match(storageSource, /method: "POST"/);
  assert.match(storageSource, /t\("toast\.measured"/);
  assert.match(storageSource, /measurement: result/);
  assert.match(storageSource, /const measurement = volume\.measurement/);
  assert.match(storageSource, /groupVolumesByNode\(targets\)/);
  assert.match(storageSource, /for \(const volume of group\)/);
  assert.match(storageSource, /index \+= 4/);
  assert.match(storageSource, /t\("actions\.measurePage"\)/);
  assert.match(storageSource, /<AdminToast/);
  assert.doesNotMatch(storageSource, /Session requests|No cleanup needed/);
});

test("Session storage exposes safe orphan deletion without ambiguous cleanup copy", () => {
  assert.match(storageSource, /orphan_count\?: number/);
  assert.match(storageSource, /t\("actions\.deleteOrphans", \{ count: orphanCount \}\)/);
  assert.match(storageSource, /fetch\("\/api\/admin\/session-storage\/orphans"/);
  assert.match(storageSource, /t\("confirm\.bulkDescription"\)/);
  assert.doesNotMatch(storageSource, /Clean up all|No cleanup needed/);
});

test("Session storage keeps only user-facing operational columns", () => {
  assert.doesNotMatch(storageSource, /header: "Generation"/);
  assert.doesNotMatch(storageSource, /header: "Last reset"/);
  assert.match(storageSource, /contentClassName="min-w-\[940px\]"/);
  for (const key of ["sessionUser", "node", "volume", "requested", "actualUsage", "actions"]) {
    assert.match(storageSource, new RegExp(`header: t\\("columns\\.${key}"\\)`));
  }
});

test("Admin runtime surfaces keep feedback compact and omit inapplicable Compose controls", () => {
  assert.match(sandboxesSource, /<AdminToast/);
  assert.doesNotMatch(sandboxesSource, /Sandbox runtime state refreshed[\s\S]*?rounded-xl border/);
  assert.match(nodesSource, /const composeOnly =/);
  assert.match(nodesSource, /column\.id !== "capacity" && column\.id !== "actions"/);
  assert.match(nodesSource, /formatMemoryQuantity\(node\.memory_allocatable\)/);
  assert.match(storageSource, /t\("availablePercent"/);
  assert.doesNotMatch(storageSource, /formatPercent\(occupiedRatio\)\} unavailable/);
});

test("Admin node and storage pages preserve independently available diagnostics", () => {
  assert.match(nodesSource, /usage_available\?: boolean/);
  assert.match(nodesSource, /<AdminAlert tone="warning"/);
  assert.match(nodesSource, /t\("usageUnavailable"\)/);
  assert.match(nodesSource, /t\("usageUnknown"\)/);
  assert.match(storageSource, /Promise\.allSettled/);
  assert.match(storageSource, /setNodeLoadError/);
  assert.match(storageSource, /setVolumeLoadError/);
  assert.match(storageSource, /t\("nodes\.unavailable"\)/);
  assert.match(storageSource, /t\("sessions\.unavailable"\)/);
});

test("Sandbox rows keep one-line summaries and move diagnostics into a centered details modal", () => {
  for (const key of ["sandbox", "status", "owner", "node", "created", "actions"]) {
    assert.match(sandboxesSource, new RegExp(`header: t\\("columns\\.${key}"\\)`));
  }
  assert.doesNotMatch(
    sandboxesSource,
    /header: "Session \/ User"|header: "Runtime"|header: "Node \/ Pod"/,
  );
  assert.match(sandboxesSource, /label: t\("actions\.details"\)/);
  assert.match(sandboxesSource, /<Modal\.Container placement="center"/);
  assert.match(sandboxesSource, /label=\{t\("details\.image"\)\}/);
  assert.match(sandboxesSource, /label=\{t\("details\.sessionId"\)\}/);
});

test("Empty MCP state has one create action and no redundant search control", () => {
  assert.match(mcpsSource, /loading \|\| mcps\.length > 0/);
  assert.match(mcpsSource, /<AdminToast message=\{notice\}/);
  assert.match(mcpsSource, /t\("empty"\)/);
});

test("Truncated Admin table values share tooltip, navigation, and visible copy affordances", () => {
  assert.match(adminUISource, /href\?: string/);
  assert.match(adminUISource, /onPress\?: \(\) => void/);
  assert.match(adminUISource, /group inline-flex min-w-0 max-w-full items-center align-middle/);
  assert.match(adminUISource, /className="size-6 min-w-6 shrink-0 opacity-70/);
  assert.match(
    adminUISource,
    /const \[tooltipOpen, setTooltipOpen\] = useState\(false\)[\s\S]*?containerRef\.current\?\.firstElementChild[\s\S]*?trigger\.scrollWidth > trigger\.clientWidth \+ 1[\s\S]*?<TooltipPrimitive\.Root open=\{tooltipOpen\} onOpenChange=\{updateTooltipOpen\}>[\s\S]*?<TooltipPrimitive\.Trigger asChild>\{text\}<\/TooltipPrimitive\.Trigger>[\s\S]*?<TooltipPrimitive\.Portal>[\s\S]*?className="z-50 max-w-80/,
  );
  assert.match(adminUISource, /<TooltipPrimitive\.Provider delayDuration=\{0\}>/);
  assert.doesNotMatch(adminUISource, /<span className="min-w-0">\{text\}<\/span>/);
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

test("Agent Runs keeps conversation metadata on two deterministic rows", () => {
  assert.match(
    auditSource,
    /id: "conversation"[\s\S]*?className="grid min-w-0 gap-0\.5"[\s\S]*?copy\.conversationTitle[\s\S]*?copy\.conversationId/,
  );
  assert.doesNotMatch(
    auditSource,
    /id: "conversation"[\s\S]*?className="block min-w-0"[\s\S]*?id: "source"/,
  );
  assert.match(auditSource, /AdminStatusBadge className="whitespace-nowrap"/);
  assert.match(auditSource, /className="whitespace-nowrap font-mono text-xs"/);
  assert.match(
    auditSource,
    /id: "conversation"[\s\S]*?minWidth: 360,[\s\S]*?headerClassName: "w-\[360px\] min-w-\[360px\]",[\s\S]*?cellClassName: "w-\[360px\] min-w-\[360px\]",[\s\S]*?id: "source"/,
  );
});

test("Agent Runs rebuilds cached table cells when the locale changes", () => {
  assert.match(auditSource, /const locale = useLocale\(\)/);
  assert.match(auditSource, /<AdminDataGrid[\s\S]*?key=\{locale\}/);
});

test("Trace timeline keeps its summary and continuous rail aligned with event icons", () => {
  assert.match(
    traceSource,
    /<Card\.Header className="flex-row items-start justify-between gap-4 p-0">/,
  );
  assert.match(traceSource, /<TimerReset className="size-3\.5" \/>/);
  assert.match(traceSource, /left-\[1\.625rem\] top-\[1\.9375rem\] -bottom-\[1\.9375rem\] w-px/);
  assert.doesNotMatch(traceSource, /<CheckCircle2 className="size-3\.5" \/>/);
});

test("Admin confirmation loading icons stay centered in every shared confirmation dialog", () => {
  assert.match(actionDialogSource, /className="inline-grid place-items-center"/);
  assert.match(actionDialogSource, /className="col-start-1 row-start-1 size-4 animate-spin"/);
  assert.doesNotMatch(actionDialogSource, /absolute left-1\/2 top-1\/2/);
});
