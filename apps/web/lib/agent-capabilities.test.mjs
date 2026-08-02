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
const chatPageSource = readFileSync(new URL("../app/page.tsx", import.meta.url), "utf8");
const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);

test("Agent Skills use fixed cards, paginate the catalog, and preserve unavailable selections", () => {
  assert.match(capabilitiesSource, /Skills \(Optional\)/);
  assert.match(capabilitiesSource, /Using default skills/);
  assert.match(capabilitiesSource, /Using a custom skill set/);
  assert.match(capabilitiesSource, />unavailable<\/Badge>/);
  assert.match(capabilitiesSource, /<SkillIcon name=/);
  assert.match(capabilitiesSource, /<Badge>.*personal.*shared/s);
  assert.match(capabilitiesSource, /const SKILLS_PER_PAGE = 6/);
  assert.match(capabilitiesSource, /filteredSkills\.slice\(/);
  assert.match(capabilitiesSource, /md:grid-cols-2 xl:grid-cols-3/);
  assert.match(capabilitiesSource, /flex h-40 min-w-0 flex-col overflow-hidden/);
  assert.match(capabilitiesSource, /Previous skills page/);
  assert.match(capabilitiesSource, /Next skills page/);
  assert.doesNotMatch(capabilitiesSource, /line-clamp-2 block/);
  assert.match(agentListSource, /Default skills/);
  assert.match(agentListSource, /selectedSkills\.slice\(0, 2\)/);
  assert.match(agentListSource, /\+\{selectedSkills\.length - 2\}/);
});

test("Skill lists search by name only with consistent input copy", () => {
  assert.match(capabilitiesSource, /placeholder="input skill name"/);
  assert.match(skillsPageSource, /placeholder="input skill name"/);
  assert.match(
    capabilitiesSource,
    /displayedSkills\.filter\(\(skill\) => skill\.name\.toLowerCase\(\)\.includes\(query\)\)/,
  );
  assert.match(
    skillsPageSource,
    /skills\.filter\(\(skill\) => displaySkillName\(skill\)\.toLowerCase\(\)\.includes\(query\)\)/,
  );
  assert.match(capabilitiesSource, /setSkillPage\(1\)/);
});

test("Agent Knowledge can select Cocola Wiki files without requiring a Skill", () => {
  assert.match(capabilitiesSource, /Add from Cocola Wiki/);
  assert.match(capabilitiesSource, /fetch\(\"\/api\/wiki\/tree\"/);
  assert.match(capabilitiesSource, /type: \"cocola_wiki\"/);
  assert.match(capabilitiesSource, /node_id: node\.id/);
  assert.match(capabilitiesSource, /cocola_wiki: \[\]/);
  assert.match(
    capabilitiesSource,
    /Saved Knowledge changes apply from the next message, including in existing/,
  );
});

test("Agent Knowledge accepts Lark Office links and keeps feedback inside its section", () => {
  assert.match(capabilitiesSource, /"feishu\.cn", "larkoffice\.com", "larksuite\.com"/);
  assert.match(capabilitiesSource, /active:scale-\[0\.97\]/);
  assert.match(capabilitiesSource, /id="knowledge-input-feedback"/);
  assert.doesNotMatch(capabilitiesSource, /capabilityMessage/);
  assert.doesNotMatch(capabilitiesSource, /Check access|Not checked/);
  assert.doesNotMatch(agentPageSource, /knowledge\/check|checkKnowledgeAccess/);
});

test("Agent selection hides global starters while global starters only fill the composer", () => {
  assert.match(threadSource, /visiblePromptStarters = selectedAgent \? \[\] : PROMPT_STARTERS/);
  assert.doesNotMatch(threadSource, /suggested_prompts/);
  assert.match(threadSource, /composer\.setText\(starter\.prompt\)/);
  assert.doesNotMatch(threadSource, /<ThreadPrimitive\.Suggestion/);
});

test("Agent editor omits the removed test action and identifies an already-saved default icon", () => {
  assert.doesNotMatch(agentPageSource, /Test Agent|testAgent|window\.open/);
  assert.match(agentPageSource, /dirty \? "Save" : "Saved"/);
  assert.match(chatPageSource, /get\("agent"\)/);
  assert.match(chatPageSource, /This Agent is unavailable\. Standard chat is ready instead\./);
});

test("Agent creation saves the selected icon and color, including the defaults", () => {
  assert.match(agentListSource, /useState<string>\(DEFAULT_AGENT_AVATAR_KEY\)/);
  assert.match(agentListSource, /useState<string>\(DEFAULT_AGENT_AVATAR_COLOR\)/);
  assert.match(agentListSource, /AGENT_AVATAR_KEYS\.map/);
  assert.match(agentListSource, /AGENT_AVATAR_COLORS\.map/);
  assert.match(agentListSource, /avatar_key: avatarKey/);
  assert.match(agentListSource, /avatar_color: avatarColor/);
  assert.match(agentListSource, /max-h-\[calc\(100vh-2rem\)\].*overflow-y-auto/);
});

test("Agent list, create dialog, and editor use a flat primary button color", () => {
  const cyanTheme = globalsSource.match(/\.cocola-user-ui \.user-theme-cyan,[\s\S]*?\n\}/)?.[0];
  assert.ok(cyanTheme, "cyan user theme not found");
  assert.match(cyanTheme, /--page-accent-grad:\s*#0891b2;/);
  assert.doesNotMatch(cyanTheme, /linear-gradient/);
  assert.match(agentListSource, /className="user-accent-btn/);
  assert.match(agentPageSource, /className="user-accent-btn/);
});
