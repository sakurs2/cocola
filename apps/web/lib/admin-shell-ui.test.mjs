import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const shellSource = readFileSync(
  new URL("../components/admin/admin-shell.tsx", import.meta.url),
  "utf8",
);
const sidebarSource = readFileSync(
  new URL("../components/admin/admin-sidebar.tsx", import.meta.url),
  "utf8",
);
const navigationSource = readFileSync(
  new URL("../components/admin/admin-navigation.ts", import.meta.url),
  "utf8",
);
const overviewSource = readFileSync(new URL("../app/admin/page.tsx", import.meta.url), "utf8");
const architectureSource = readFileSync(
  new URL("../app/admin/architecture/page.tsx", import.meta.url),
  "utf8",
);
const componentLogsSource = readFileSync(
  new URL("../app/admin/component-logs/page.tsx", import.meta.url),
  "utf8",
);
const settingsSource = readFileSync(
  new URL("../app/admin/settings/page.tsx", import.meta.url),
  "utf8",
);
const selectControlSource = readFileSync(
  new URL("../components/ui/select-control.tsx", import.meta.url),
  "utf8",
);
const layoutSource = readFileSync(new URL("../app/admin/layout.tsx", import.meta.url), "utf8");
const globalStyles = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");

test("formal Admin uses the Cocola AppLayout compatibility layer and compound Sidebar", () => {
  assert.match(shellSource, /import \{ AppLayout \} from "@cocola\/ui-compat\/app-layout"/);
  assert.match(sidebarSource, /import \{ Sidebar \} from "@cocola\/ui-compat\/sidebar"/);
  assert.match(shellSource, /sidebar=\{<AdminSidebar activeSectionId=\{section\.id\} \/>\}/);
  assert.match(shellSource, /style=\{getAdminThemeStyle\(section\.theme\)\}/);
  assert.doesNotMatch(shellSource, /admin-glass-sidebar|admin-glass-shell|<aside/);
  assert.match(sidebarSource, /<Sidebar>[\s\S]*?<Sidebar\.Header>[\s\S]*?<Sidebar\.Content/);
  assert.match(sidebarSource, /<Sidebar\.Footer/);
  assert.match(sidebarSource, /Sidebar\.Content className="overscroll-contain pb-3 pt-1"/);
  assert.match(
    sidebarSource,
    /Sidebar\.Footer className="relative z-10 border-t border-separator bg-background"/,
  );
  assert.match(sidebarSource, /<Icon className=\{`size-4 \$\{section\.iconClassName\}`\} \/>/);
  assert.match(
    sidebarSource,
    /grid-cols-\[1\.25rem_minmax\(0,1fr\)\][\s\S]*?<CocolaCoreLogo className="size-10 max-w-none shrink-0" \/>/,
  );
  assert.match(layoutSource, /return <AdminShell>\{children\}<\/AdminShell>/);
  assert.doesNotMatch(layoutSource, /AdminHeroUIProvider/);
});

test("Admin route registry preserves every real Cocola Admin entry point", () => {
  for (const path of [
    "users",
    "models",
    "skills",
    "mcps",
    "toolbox",
    "scheduled-tasks",
    "audit",
    "token-usage",
    "sandboxes",
    "sandbox-nodes",
    "storage",
    "architecture",
    "component-logs",
    "settings",
  ]) {
    assert.match(navigationSource, new RegExp(`path: "${path}"`));
  }
  assert.match(navigationSource, /pathname\.startsWith\("\/admin\/traces"\)/);
});

test("Admin Overview is copied from the approved grouped HeroUI card layout", () => {
  assert.match(overviewSource, /admin-overview-group bg-surface-secondary rounded-3xl/);
  assert.match(overviewSource, /admin-overview-card h-full min-h-40 p-5/);
  assert.match(overviewSource, /admin-overview-icon bg-accent-soft text-accent/);
  assert.match(overviewSource, /admin-overview-cta[\s\S]*?text-white/);
  assert.match(overviewSource, /style=\{getAdminThemeStyle\(section\.theme\)\}/);
  assert.doesNotMatch(overviewSource, /cocola-admin-module/);
  assert.match(globalStyles, /\.cocola-admin-ui a:hover > \.admin-overview-card/);
  assert.match(globalStyles, /\.cocola-admin-ui \.cocola-sidebar-tab:hover/);
});

test("Admin utility pages keep stable HeroUI controls and visible architecture cards", () => {
  assert.doesNotMatch(shellSource, /AdminTopbar label=/);
  assert.doesNotMatch(shellSource, /function AdminTopbar\(\{ label/);
  assert.doesNotMatch(shellSource, /Self-hosted|userLabel|useSession|<Chip/);
  assert.match(shellSource, /navbar=\{<AdminTopbar \/>\}/);
  assert.match(shellSource, /<WorkspaceHeaderActions \/>/);
  assert.match(selectControlSource, /style=\{\{ transform: "none" \}\}/);
  assert.match(componentLogsSource, /className="h-10 w-full rounded-xl"/);
  assert.match(componentLogsSource, /<Input className="h-10"/);
  assert.match(architectureSource, /<Card[\s\S]*?admin-architecture-node-card group h-28 w-full/);
  assert.match(architectureSource, /<button[\s\S]*?aria-pressed=\{selected\}/);
  assert.match(architectureSource, /<ChevronRight/);
  assert.match(componentLogsSource, /<Label>Service<\/Label>/);
  assert.match(componentLogsSource, /lines\.length === 1 \? "line" : "lines"/);
  assert.match(componentLogsSource, /max-h-\[32rem\] min-h-32/);
  assert.match(globalStyles, /\.admin-architecture-node-card:hover/);
  assert.match(globalStyles, /transform: translateY\(-2px\)/);
});

test("Admin Settings aligns controls and actions to the same row start", () => {
  assert.match(
    settingsSource,
    /admin-setting-row[^\"]*lg:grid-cols-\[minmax\(0,1fr\)_minmax\(220px,320px\)_180px\][^\"]*lg:items-start/,
  );
  assert.match(settingsSource, /admin-setting-description min-w-0/);
  assert.match(settingsSource, /admin-setting-control min-w-0/);
  assert.match(settingsSource, /admin-setting-actions flex items-center justify-end gap-2/);
  assert.doesNotMatch(settingsSource, /admin-setting-row[^\"]*lg:items-center/);
});
