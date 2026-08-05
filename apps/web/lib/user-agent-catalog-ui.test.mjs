import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const capabilitiesSource = readFileSync(
  new URL("../components/agents/agent-capabilities-editor.tsx", import.meta.url),
  "utf8",
);
const skillsPageSource = readFileSync(new URL("../app/skills/page.tsx", import.meta.url), "utf8");
const demoStyles = readFileSync(new URL("../app/cocola-web-demo.css", import.meta.url), "utf8");
const globalStyles = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");

test("Agent capabilities use a compact Knowledge empty state and a two-column Skills grid", () => {
  const knowledgeSection = capabilitiesSource.slice(
    capabilitiesSource.indexOf("<Card.Title>Knowledge</Card.Title>"),
    capabilitiesSource.indexOf("<Sheet isOpen={wikiPickerOpen}"),
  );

  assert.match(capabilitiesSource, /columns=\{2\}/);
  assert.match(
    demoStyles,
    /\.cocola-user-ui \.cocola-web-agent-skill-grid \.item-card \{\s*align-items: flex-start;/,
  );
  assert.match(knowledgeSection, /No Knowledge sources yet\./);
  assert.doesNotMatch(knowledgeSection, /min-h-24/);
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
  assert.match(skillsPageSource, /const SKILLS_PER_PAGE = 9/);
  assert.match(skillsPageSource, /paginateCatalog\(/);
  assert.match(skillsPageSource, /aria-label="Skills pagination"/);
  assert.match(skillsPageSource, /setSkillPage\(1\)/);
});

test("Skills cards keep isolated state actions and catalog hover motion", () => {
  const toggleAction = skillsPageSource.slice(
    skillsPageSource.indexOf("const setSkillEnabled"),
    skillsPageSource.indexOf("const deleteSkill"),
  );

  assert.doesNotMatch(toggleAction, /setWorking\(/);
  assert.match(skillsPageSource, /cocola-web-skill-card/);
  assert.match(globalStyles, /\.cocola-user-ui \.cocola-web-skill-card:hover,[\s\S]*?translateY\(-3px\) scale\(1\.005\)/);
  assert.match(skillsPageSource, /variant=\{skill\.enabled \? "danger" : "primary"\}/);
  assert.match(skillsPageSource, /\{skill\.enabled \? "Disable" : "Enable"\}/);
  assert.match(demoStyles, /\.cocola-user-ui \.cocola-web-skill-disable-action \{\s*color: white !important;/);
  assert.match(skillsPageSource, /cocola-web-skill-enable-action/);
  assert.match(
    demoStyles,
    /\.cocola-user-ui \.cocola-web-skill-enable-action \{[\s\S]*?var\(--success\)[\s\S]*?color: white !important;/,
  );
});

test("Skills cards do not repeat enabled state in the card header", () => {
  const skillCard = skillsPageSource.slice(
    skillsPageSource.indexOf("function SkillCard"),
    skillsPageSource.indexOf("async function readError"),
  );

  assert.doesNotMatch(skillCard, /color=\{skill\.enabled \? "success" : "warning"\}/);
});
