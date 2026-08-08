import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(path, import.meta.url), "utf8");

const [
  adminUISource,
  globalStyles,
  auditSource,
  nodesSource,
  sandboxesSource,
  settingsSource,
  storageSource,
] = await Promise.all([
  read("../components/admin/admin-ui.tsx"),
  read("../app/globals.css"),
  read("../app/admin/audit/page.tsx"),
  read("../app/admin/sandbox-nodes/page.tsx"),
  read("../app/admin/sandboxes/page.tsx"),
  read("../app/admin/settings/page.tsx"),
  read("../app/admin/storage/page.tsx"),
]);

test("Admin operational tables expose a persistent horizontal scrollbar", () => {
  assert.match(adminUISource, /export function AdminDataGrid/);
  assert.match(adminUISource, /admin-data-grid-scroll/);
  assert.match(globalStyles, /\.admin-data-grid-scroll[\s\S]*?overflow-x: scroll/);
  assert.match(globalStyles, /\.admin-data-grid-scroll::-webkit-scrollbar/);
  assert.match(auditSource, /<AdminDataGrid/);
});

test("Admin row actions move with the table and use a compact HeroUI menu", () => {
  assert.match(adminUISource, /export function AdminRowActions[\s\S]*?<Dropdown>/);
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
  assert.match(storageSource, /Clean up all/);
  assert.doesNotMatch(storageSource, /Rebuild empty Volume|Recreate Volume/);
});
