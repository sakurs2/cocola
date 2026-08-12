import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const capabilitiesSource = readFileSync(
  new URL("../components/agents/agent-capabilities-editor.tsx", import.meta.url),
  "utf8",
);
const skillsPageSource = readFileSync(new URL("../app/skills/page.tsx", import.meta.url), "utf8");
const agentListSource = readFileSync(new URL("../app/agents/page.tsx", import.meta.url), "utf8");
const agentPageSource = readFileSync(
  new URL("../app/agents/[id]/page.tsx", import.meta.url),
  "utf8",
);
const globalsSource = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");
const demoStylesSource = readFileSync(
  new URL("../app/cocola-web-demo.css", import.meta.url),
  "utf8",
);
const chatPageSource = readFileSync(new URL("../app/page.tsx", import.meta.url), "utf8");
const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);

test("Agent Skills use fixed cards, paginate the catalog, and preserve unavailable selections", () => {
  assert.match(capabilitiesSource, /<Card\.Title>\{t\("skills"\)\}<\/Card\.Title>/);
  assert.match(capabilitiesSource, /t\("usingDefault"\)/);
  assert.match(capabilitiesSource, /t\("usingCustom"\)/);
  assert.match(capabilitiesSource, /skill\.unavailable_reason === "disabled_by_administrator"/);
  assert.match(capabilitiesSource, /\? t\("adminDisabled"\)\s*: t\("unavailable"\)/);
  assert.match(capabilitiesSource, /<SkillIcon name=/);
  assert.match(
    capabilitiesSource,
    /skill\.source === "personal" \? t\("personal"\) : t\("shared"\)/,
  );
  assert.match(capabilitiesSource, /const SKILLS_PER_PAGE = 6/);
  assert.match(capabilitiesSource, /filteredSkills\.slice\(/);
  assert.match(capabilitiesSource, /columns=\{2\} layout="grid"/);
  assert.match(capabilitiesSource, /min-h-\[9\.5rem\] w-full overflow-hidden/);
  assert.match(capabilitiesSource, /t\("previousPage"\)/);
  assert.match(capabilitiesSource, /t\("nextPage"\)/);
  assert.doesNotMatch(capabilitiesSource, /line-clamp-2 block/);
  assert.match(agentListSource, /t\("defaultSkills"\)/);
  assert.match(agentListSource, /selectedSkills\.slice\(0, 2\)/);
  assert.match(agentListSource, /\+\{selectedSkills\.length - 2\}/);
});

test("Skill lists search by name only with consistent input copy", () => {
  assert.match(capabilitiesSource, /placeholder=\{t\("searchSkills"\)\}/);
  assert.match(skillsPageSource, /placeholder=\{t\("search"\)\}/);
  assert.match(
    capabilitiesSource,
    /displayedSkills\.filter\(\(skill\) => skill\.name\.toLowerCase\(\)\.includes\(query\)\)/,
  );
  assert.match(
    skillsPageSource,
    /availableSkills\.filter\(\(skill\) => displaySkillName\(skill\)\.toLowerCase\(\)\.includes\(query\)\)/,
  );
  assert.match(capabilitiesSource, /setSkillPage\(1\)/);
});

test("Agent Knowledge can select Cocola Wiki files without requiring a Skill", () => {
  assert.match(capabilitiesSource, /t\("addFromWiki"\)/);
  assert.match(capabilitiesSource, /fetch\(\"\/api\/wiki\/tree\"/);
  assert.match(capabilitiesSource, /type: \"cocola_wiki\"/);
  assert.match(capabilitiesSource, /node_id: node\.id/);
  assert.match(capabilitiesSource, /cocola_wiki: \[\]/);
  assert.match(capabilitiesSource, /t\("knowledgeDescription"\)/);
});

test("Agent Knowledge accepts Lark Office links and keeps feedback inside its section", () => {
  assert.match(capabilitiesSource, /"feishu\.cn", "larkoffice\.com", "larksuite\.com"/);
  assert.match(
    capabilitiesSource,
    /className="cocola-web-page-primary-action" onPress=\{addKnowledge\}/,
  );
  assert.match(capabilitiesSource, /knowledgeNotice \? \(/);
  assert.doesNotMatch(capabilitiesSource, /capabilityMessage/);
  assert.doesNotMatch(capabilitiesSource, /Check access|Not checked/);
  assert.doesNotMatch(agentPageSource, /knowledge\/check|checkKnowledgeAccess/);
});

test("Agent selection keeps global starters available while starters only fill the composer", () => {
  assert.match(threadSource, /\{promptStarters\.map\(\(starter\) => \{/);
  assert.doesNotMatch(threadSource, /selectedAgent \? \[\] : PROMPT_STARTERS/);
  assert.doesNotMatch(threadSource, /suggested_prompts/);
  assert.match(threadSource, /composer\.setText\(starter\.prompt\)/);
  assert.doesNotMatch(threadSource, /<ThreadPrimitive\.Suggestion/);
});

test("Agent editor omits the removed test action and identifies an already-saved default icon", () => {
  assert.doesNotMatch(agentPageSource, /Test Agent|testAgent|window\.open/);
  assert.match(agentPageSource, /dirty \? t\("save"\) : t\("saved"\)/);
  assert.match(chatPageSource, /get\("agent"\)/);
  assert.match(chatPageSource, /t\("agentUnavailable"\)/);
});

test("Agent creation saves the selected icon and color, including the defaults", () => {
  assert.match(agentListSource, /useState<string>\(DEFAULT_AGENT_AVATAR_KEY\)/);
  assert.match(agentListSource, /useState<string>\(DEFAULT_AGENT_AVATAR_COLOR\)/);
  assert.match(agentListSource, /AGENT_AVATAR_KEYS\.map/);
  assert.match(agentListSource, /AGENT_AVATAR_COLORS\.map/);
  assert.match(agentListSource, /avatar_key: avatarKey/);
  assert.match(agentListSource, /avatar_color: avatarColor/);
  assert.match(agentListSource, /<WorkspaceEntitySheet/);
});

test("Agent list, create dialog, and editor use a flat primary button color", () => {
  const cyanTheme = globalsSource.match(/\.cocola-user-ui \.user-theme-cyan,[\s\S]*?\n\}/)?.[0];
  assert.ok(cyanTheme, "cyan user theme not found");
  assert.match(cyanTheme, /--accent:/);
  assert.match(
    demoStylesSource,
    /\.cocola-web-page-primary-action,[\s\S]*?color: white !important/,
  );
  assert.match(agentListSource, /<WorkspacePageAction/);
  assert.match(agentPageSource, /className="cocola-web-page-primary-action"/);
});
