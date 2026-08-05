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
const selectControlSource = readFileSync(
  new URL("../components/ui/select-control.tsx", import.meta.url),
  "utf8",
);
const layoutSource = readFileSync(new URL("../app/admin/layout.tsx", import.meta.url), "utf8");
const globalStyles = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");

test("formal Admin uses the approved HeroUI Pro AppLayout and compound Sidebar", () => {
  assert.match(shellSource, /import \{ AppLayout \} from "@heroui-pro\/react\/app-layout"/);
  assert.match(shellSource, /sidebar=\{<AdminSidebar activeSectionId=\{section\.id\} \/>\}/);
  assert.match(shellSource, /style=\{getAdminThemeStyle\(section\.theme\)\}/);
  assert.doesNotMatch(shellSource, /admin-glass-sidebar|admin-glass-shell|<aside/);
  assert.match(sidebarSource, /<Sidebar>[\s\S]*?<Sidebar\.Header>[\s\S]*?<Sidebar\.Content>/);
  assert.match(sidebarSource, /<Sidebar\.Footer>/);
  assert.match(sidebarSource, /<CocolaCoreLogo className="size-10 shrink-0" \/>/);
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
  assert.doesNotMatch(overviewSource, /cocola-admin-module/);
  assert.match(globalStyles, /\.cocola-admin-ui a:hover > \.admin-overview-card/);
  assert.match(globalStyles, /\.cocola-admin-ui \.cocola-sidebar-tab:hover/);
});

test("Admin utility pages keep stable HeroUI controls and visible architecture cards", () => {
  assert.doesNotMatch(shellSource, /AdminTopbar label=/);
  assert.doesNotMatch(shellSource, /function AdminTopbar\(\{ label/);
  assert.match(selectControlSource, /style=\{\{ transform: "none" \}\}/);
  assert.match(componentLogsSource, /className="h-10 w-full rounded-xl"/);
  assert.match(componentLogsSource, /<Input className="h-10"/);
  assert.match(architectureSource, /<Card[\s\S]*?group h-28 w-full/);
  assert.match(architectureSource, /<button[\s\S]*?aria-pressed=\{selected\}/);
});
