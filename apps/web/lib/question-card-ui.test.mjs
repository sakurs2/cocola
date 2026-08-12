import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const questionCardSource = readFileSync(
  new URL("../components/assistant-ui/rich-message-parts.tsx", import.meta.url),
  "utf8",
);
const workspaceSidebarSource = readFileSync(
  new URL("../components/assistant-ui/heroui-workspace-sidebar.tsx", import.meta.url),
  "utf8",
);
const runtimeProviderSource = readFileSync(
  new URL("../app/runtime-provider.tsx", import.meta.url),
  "utf8",
);

test("Question Card uses the compact HeroUI composition", () => {
  assert.match(questionCardSource, /from "@heroui\/react"/);
  assert.match(questionCardSource, /<Card aria-labelledby=\{titleId\}/);
  assert.match(questionCardSource, /<Card\.Header className="flex-row items-start/);
  assert.match(questionCardSource, /<RadioGroup[\s\S]*?<Radio\.Content/);
  assert.match(questionCardSource, /<TextField[\s\S]*?<TextArea/);
  assert.match(questionCardSource, /<Card\.Footer className="flex justify-end gap-2 px-4 py-3">/);
  assert.match(questionCardSource, /<Separator \/>/);
  assert.match(questionCardSource, /className="my-4 w-full max-w-none/);
});

test("Question Card no longer carries the legacy runtime-specific surface", () => {
  assert.doesNotMatch(questionCardSource, /Claude needs your input/);
  assert.doesNotMatch(questionCardSource, /border-sky-500/);
  assert.doesNotMatch(questionCardSource, /role="radiogroup"/);
  assert.doesNotMatch(questionCardSource, /<textarea/);
  assert.doesNotMatch(questionCardSource, /max-w-\[720px\]/);
});

test("conversations waiting for a plan or question confirmation expose a durable sidebar state", () => {
  assert.match(runtimeProviderSource, /requires_user_action\?: boolean/);
  assert.match(
    workspaceSidebarSource,
    /requiresUserAction=\{conversation\.requires_user_action === true\}/,
  );
  assert.match(workspaceSidebarSource, /<CircleQuestionFill/);
  assert.match(workspaceSidebarSource, /t\("waitingConfirmation"\)/);
  assert.match(
    workspaceSidebarSource,
    /running \? \([\s\S]*?\) : requiresUserAction \? \([\s\S]*?\) : null/,
  );
  assert.match(
    runtimeProviderSource,
    /ev\.kind === "plan_ready" \|\| ev\.kind === "question_ready"/,
  );
  assert.match(
    runtimeProviderSource,
    /markConversationRequiresUserAction\(targetSessionId, true\)/,
  );
  assert.match(
    runtimeProviderSource,
    /!awaitingUserActionIdsRef\.current\.has\(cursor\.conversationId\)/,
  );
});
