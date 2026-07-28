import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const capabilitiesSource = readFileSync(
  new URL("../components/agents/agent-capabilities-editor.tsx", import.meta.url),
  "utf8",
);
const agentListSource = readFileSync(new URL("../app/agents/page.tsx", import.meta.url), "utf8");
const agentPageSource = readFileSync(
  new URL("../app/agents/[id]/page.tsx", import.meta.url),
  "utf8",
);
const chatPageSource = readFileSync(new URL("../app/page.tsx", import.meta.url), "utf8");
const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);

test("Agent Skills explain default and custom modes and preserve unavailable selections", () => {
  assert.match(capabilitiesSource, /Skills \(Optional\)/);
  assert.match(capabilitiesSource, /Using default skills/);
  assert.match(capabilitiesSource, /Using a custom skill set/);
  assert.match(capabilitiesSource, />unavailable<\/Badge>/);
  assert.match(capabilitiesSource, /<SkillIcon name=/);
  assert.match(capabilitiesSource, /<Badge>.*personal.*shared/s);
  assert.match(capabilitiesSource, /md:grid-cols-2 xl:grid-cols-3/);
  assert.match(agentListSource, /Default skills/);
  assert.match(agentListSource, /selectedSkills\.slice\(0, 2\)/);
  assert.match(agentListSource, /\+\{selectedSkills\.length - 2\}/);
});

test("Agent Knowledge can select Cocola Wiki files without requiring a Skill", () => {
  assert.match(capabilitiesSource, /Add from Cocola Wiki/);
  assert.match(capabilitiesSource, /fetch\(\"\/api\/wiki\/tree\"/);
  assert.match(capabilitiesSource, /type: \"cocola_wiki\"/);
  assert.match(capabilitiesSource, /node_id: node\.id/);
  assert.match(capabilitiesSource, /cocola_wiki: \[\]/);
});

test("Agent Knowledge accepts Lark Office links and keeps feedback inside its section", () => {
  assert.match(capabilitiesSource, /"feishu\.cn", "larkoffice\.com", "larksuite\.com"/);
  assert.match(capabilitiesSource, /active:scale-\[0\.97\]/);
  assert.match(capabilitiesSource, /id="knowledge-input-feedback"/);
  assert.doesNotMatch(capabilitiesSource, /capabilityMessage/);
  assert.doesNotMatch(capabilitiesSource, /Check access|Not checked/);
  assert.doesNotMatch(agentPageSource, /knowledge\/check|checkKnowledgeAccess/);
});

test("Agent Suggested Prompts replace global starters and only fill the composer", () => {
  assert.match(threadSource, /if \(!selectedAgent\) return PROMPT_STARTERS/);
  assert.match(threadSource, /selectedAgent\.suggested_prompts\.map/);
  assert.match(threadSource, /composer\.setText\(starter\.prompt\)/);
  assert.doesNotMatch(threadSource, /<ThreadPrimitive\.Suggestion/);
});

test("Test Agent opens a fresh preselected chat only for saved changes", () => {
  assert.match(agentPageSource, /if \(!agent \|\| dirty\) return/);
  assert.match(agentPageSource, /window\.open\(`\/\?agent=\$\{encodeURIComponent\(agent\.id\)\}`/);
  assert.match(agentPageSource, /Save changes before testing this Agent\./);
  assert.match(chatPageSource, /get\("agent"\)/);
  assert.match(chatPageSource, /This Agent is unavailable\. Standard chat is ready instead\./);
  assert.doesNotMatch(agentPageSource, /autoSend|sendMessage|composer\.send/);
});
