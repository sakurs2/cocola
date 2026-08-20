import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const capabilitiesSource = readFileSync(
  new URL("../components/agents/agent-capabilities-editor.tsx", import.meta.url),
  "utf8",
);
const itemCardSource = readFileSync(
  new URL("../../../packages/ui-compat/src/item-card.tsx", import.meta.url),
  "utf8",
);
const skillsPageSource = readFileSync(new URL("../app/skills/page.tsx", import.meta.url), "utf8");
const skillDetailSource = readFileSync(
  new URL("../app/skills/[id]/page.tsx", import.meta.url),
  "utf8",
);
const adminSkillsPageSource = readFileSync(
  new URL("../app/admin/skills/page.tsx", import.meta.url),
  "utf8",
);
const mcpsPageSource = readFileSync(new URL("../app/mcps/page.tsx", import.meta.url), "utf8");
const demoStyles = readFileSync(new URL("../app/cocola-web-demo.css", import.meta.url), "utf8");
const globalStyles = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");

test("Agent capabilities use a compact Knowledge empty state and a two-column Skills grid", () => {
  const knowledgeSection = capabilitiesSource.slice(
    capabilitiesSource.indexOf('<Card.Title>{t("knowledge")}</Card.Title>'),
    capabilitiesSource.indexOf("<Sheet isOpen={wikiPickerOpen}"),
  );

  assert.match(capabilitiesSource, /columns=\{2\}/);
  assert.match(
    demoStyles,
    /\.cocola-user-ui \.cocola-web-agent-skill-grid \.item-card \{\s*align-items: flex-start;/,
  );
  assert.match(knowledgeSection, /t\("emptyKnowledge"\)/);
  assert.doesNotMatch(knowledgeSection, /min-h-24/);
  assert.match(itemCardSource, /const rootProps = \{[\s\S]*?children,/);
});

test("user workspace primary actions keep white text on a theme-colored background", () => {
  assert.match(
    demoStyles,
    /\.cocola-user-ui \.cocola-web-page-primary-action,[\s\S]*?color: white !important;/,
  );
  assert.match(
    capabilitiesSource,
    /className="cocola-web-page-primary-action"[^>]*onPress=\{addKnowledge\}/,
  );
});

test("Skills catalog exposes bounded pagination controls", () => {
  assert.match(skillsPageSource, /const SKILLS_PER_PAGE = 12/);
  assert.match(skillsPageSource, /paginateCatalog\(/);
  assert.match(skillsPageSource, /aria-label=\{t\("pagination"\)\}/);
  assert.match(skillsPageSource, /setSkillPage\(1\)/);
});

test("Skills cards keep isolated state actions and catalog hover motion", () => {
  const adminToggleAction = adminSkillsPageSource.slice(
    adminSkillsPageSource.indexOf("const setSkillEnabled"),
    adminSkillsPageSource.indexOf("const deleteSkill"),
  );

  assert.doesNotMatch(adminToggleAction, /setWorking\(/);
  assert.match(adminSkillsPageSource, /working=\{actionSkillId === skill\.id\}/);
  assert.match(skillsPageSource, /cocola-web-skill-card/);
  assert.match(skillsPageSource, /className="cocola-web-catalog-card-icon"/);
  assert.match(
    globalStyles,
    /\.cocola-user-ui \.cocola-web-skill-card:hover,[\s\S]*?translateY\(-3px\) scale\(1\.005\)/,
  );
  assert.match(
    globalStyles,
    /\.cocola-web-skill-card:hover \.cocola-web-catalog-card-icon,[\s\S]*?rotate\(-2deg\) scale\(1\.06\)/,
  );
  assert.match(
    skillsPageSource,
    /cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-4/,
  );
  assert.match(
    skillsPageSource,
    /<Switch[\s\S]*?isSelected=\{skill\.enabled\}[\s\S]*?<Switch\.Content>[\s\S]*?<Switch\.Control>[\s\S]*?<Switch\.Thumb \/>/,
  );
  assert.doesNotMatch(skillsPageSource, /cocola-web-skill-(?:enable|disable)-action/);
});

test("administrator-disabled Skills stay out of the user catalog but remain repairable on Agents", () => {
  assert.match(
    skillsPageSource,
    /const availableSkills = useMemo\([\s\S]*?skill\.available !== false/,
  );
  assert.match(
    capabilitiesSource,
    /skills\.filter\([\s\S]*?skill\.available \|\| selectedIDs\.has\(skill\.id\)/,
  );
  assert.match(capabilitiesSource, /t\("disabledDescription"\)/);
  assert.match(capabilitiesSource, /cocola-web-agent-skill-unavailable opacity-55/);
  assert.match(capabilitiesSource, /unavailable \? null : <PressableFeedback\.Highlight \/>/);
  assert.match(capabilitiesSource, /t\("removeUnavailable", \{ name: skill\.name \}\)/);
  assert.match(skillDetailSource, /skill\?\.available === false/);
  assert.match(skillDetailSource, /t\("adminDisabled"\)/);
});

test("Skills cards do not repeat enabled state in the card header", () => {
  const skillCard = skillsPageSource.slice(
    skillsPageSource.indexOf("function SkillCard"),
    skillsPageSource.indexOf("async function readError"),
  );

  assert.doesNotMatch(skillCard, /color=\{skill\.enabled \? "success" : "warning"\}/);
});

test("MCP cards use a single Switch control without repeating enabled state", () => {
  assert.match(
    mcpsPageSource,
    /<Switch[\s\S]*?isSelected=\{mcp\.effective_enabled\}[\s\S]*?<Switch\.Content>[\s\S]*?<Switch\.Control>[\s\S]*?<Switch\.Thumb \/>/,
  );
  assert.doesNotMatch(mcpsPageSource, /color=\{mcp\.effective_enabled \? "success" : "warning"\}/);
  assert.doesNotMatch(
    mcpsPageSource,
    /variant=\{mcp\.effective_enabled \? "outline" : "primary"\}/,
  );
});

test("MCP cards use the four-column catalog layout and matching hover motion", () => {
  assert.match(
    mcpsPageSource,
    /cocola-web-catalog-grid grid items-stretch gap-3 md:grid-cols-2 xl:grid-cols-4/,
  );
  assert.match(mcpsPageSource, /cocola-web-mcp-card p-4/);
  assert.doesNotMatch(mcpsPageSource, /cocola-web-mcp-card h-full/);
  assert.doesNotMatch(skillsPageSource, /cocola-web-skill-card h-full/);
  assert.match(mcpsPageSource, /className="cocola-web-catalog-card-icon/);
  assert.doesNotMatch(mcpsPageSource, /min-h-\[15rem\]/);
  assert.match(
    globalStyles,
    /\.cocola-user-ui \.cocola-web-mcp-card:hover,[\s\S]*?translateY\(-3px\) scale\(1\.005\)/,
  );
  assert.match(
    globalStyles,
    /\.cocola-user-ui \.cocola-web-mcp-card:hover \.cocola-web-catalog-card-icon,[\s\S]*?rotate\(-2deg\) scale\(1\.06\)/,
  );
});
