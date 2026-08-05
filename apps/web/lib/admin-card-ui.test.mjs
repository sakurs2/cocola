import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const skillsSource = readFileSync(new URL("../app/admin/skills/page.tsx", import.meta.url), "utf8");
const mcpsSource = readFileSync(new URL("../app/admin/mcps/page.tsx", import.meta.url), "utf8");
const toolboxCardSource = readFileSync(
  new URL("../app/admin/toolbox/toolbox-card.tsx", import.meta.url),
  "utf8",
);
const toolboxClientSource = readFileSync(
  new URL("../app/admin/toolbox/toolbox-client.tsx", import.meta.url),
  "utf8",
);
const tokenUsageSource = readFileSync(
  new URL("../app/admin/token-usage/page.tsx", import.meta.url),
  "utf8",
);
const globalStyles = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");

test("Admin Skill and MCP cards use HeroUI Switch controls", () => {
  assert.match(
    skillsSource,
    /<Switch[\s\S]*?isSelected=\{skill\.enabled\}[\s\S]*?<Switch\.Content>[\s\S]*?<Switch\.Control><Switch\.Thumb className="admin-switch-thumb shadow-sm" \/><\/Switch\.Control>/,
  );
  assert.match(
    mcpsSource,
    /<Switch[\s\S]*?isSelected=\{mcp\.enabled\}[\s\S]*?<Switch\.Content>[\s\S]*?<Switch\.Control><Switch\.Thumb className="admin-switch-thumb shadow-sm" \/><\/Switch\.Control>/,
  );
  assert.match(globalStyles, /\.switch\[data-selected="true"\] \.admin-switch-thumb/);
  assert.doesNotMatch(skillsSource, /variant="outline" isDisabled=\{working\} onPress=\{onToggle\}/);
  assert.doesNotMatch(mcpsSource, /variant="outline" onPress=\{onToggle\}/);
});

test("Admin catalog cards have the approved HeroUI Demo hover contract", () => {
  assert.match(skillsSource, /className="admin-skill-card h-full/);
  assert.match(mcpsSource, /className="admin-mcp-card h-full/);
  assert.match(globalStyles, /\.cocola-admin-ui \.admin-skill-card:hover/);
  assert.match(globalStyles, /\.cocola-admin-ui \.admin-mcp-card:hover/);
  assert.match(globalStyles, /\.cocola-admin-ui \.admin-toolbox-card:hover/);
});

test("Toolbox uses the compact business-card layout from the HeroUI Demo", () => {
  assert.match(toolboxCardSource, /admin-toolbox-card h-auto min-h-44 w-full/);
  assert.match(toolboxCardSource, /admin-toolbox-card-icon/);
  assert.match(toolboxCardSource, /admin-toolbox-card-arrow/);
  assert.doesNotMatch(toolboxClientSource, /title="More tools"/);
  assert.match(toolboxClientSource, /xl:grid-cols-3/);
});

test("Token Usage keeps range, trend, and user rows compact", () => {
  assert.match(tokenUsageSource, /className="admin-token-usage-range flex flex-wrap items-center gap-2"/);
  assert.doesNotMatch(tokenUsageSource, /<Card className="p-3">/);
  assert.match(tokenUsageSource, /className="h-\[300px\] px-4 pb-4 pt-1"/);
  assert.match(tokenUsageSource, /className="grid items-start gap-5/);
  assert.match(tokenUsageSource, /contentClassName="admin-token-usage-grid min-w-\[700px\]"/);
  assert.match(tokenUsageSource, /onRowAction=\{\(key\) =>/);
  assert.doesNotMatch(tokenUsageSource, /View usage for/);
  assert.doesNotMatch(tokenUsageSource, /text-muted block truncate font-mono text-xs">\{user\.user_id\}/);
  assert.match(globalStyles, /\.admin-token-usage-grid \.table__cell/);
});
