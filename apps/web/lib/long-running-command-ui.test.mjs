import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);
const railSource = readFileSync(
  new URL("../components/assistant-ui/rail.tsx", import.meta.url),
  "utf8",
);
const runtimeSource = readFileSync(new URL("../app/runtime-provider.tsx", import.meta.url), "utf8");

test("running composer exposes an enabled HeroUI stop control", () => {
  assert.match(threadSource, /<ComposerPrimitive\.Cancel asChild>/);
  assert.match(threadSource, /const ComposerStopButton/);
  assert.match(threadSource, /aria-label=\{stopping \? t\("stopping"\) : t\("stop"\)\}/);
  assert.doesNotMatch(threadSource, /<ComposerPrimitive\.Cancel asChild>\s*<PromptInput\.Send/s);
});

test("command execution uses a compact HeroUI activity card", () => {
  assert.match(railSource, /const CommandExecutionCard/);
  assert.match(railSource, /<Card\.Header/);
  assert.match(railSource, /expanded \? "w-full" : "w-fit"/);
  assert.match(railSource, /max-w-full gap-0 overflow-hidden rounded-xl/);
  assert.match(railSource, /min-w-0 flex-row items-center/);
  assert.match(railSource, /max-w-\[10rem\][\s\S]*?xl:max-w-\[40rem\]/);
  assert.doesNotMatch(
    railSource,
    /"w-full overflow-hidden border border-border\/70 bg-surface\/80 p-0 shadow-none"/,
  );
  assert.match(railSource, /max-h-72 overflow-auto/);
  assert.match(railSource, /formatAgentDuration\(elapsedSeconds \* 1000\)/);
  assert.match(railSource, /latestOutput/);
  assert.match(railSource, /motion-reduce:animate-none/);
  assert.match(railSource, /<CodeBlock[\s\S]*?language="shell"/);
  assert.match(railSource, /text-foreground\/85/);
  assert.doesNotMatch(railSource, /rounded-md bg-zinc-950 px-2 py-1/);
  assert.match(railSource, /\{t\("command\.output"\)\}/);
  assert.match(railSource, /aria-label=\{statusLabel\}/);
  assert.match(railSource, /role="status"/);
  assert.match(railSource, /title=\{statusTooltip\}/);
  assert.match(railSource, /<CheckCircle2 className="size-4"/);
  assert.doesNotMatch(railSource, /Live execution/);
  assert.doesNotMatch(railSource, />\s*Command\s*</);
  assert.doesNotMatch(railSource, /const detail = output \|\| command/);
  assert.doesNotMatch(railSource, /hideScrollBar/);
});

test("live command output stays separate from the terminal tool result", () => {
  assert.match(runtimeSource, /case "tool_output":/);
  assert.match(runtimeSource, /cocolaLiveOutput/);
  assert.match(threadSource, /liveOutput=\{toolOutputFromArtifact\(artifact\)\}/);
});

test("cancel waits for the authoritative terminal event", () => {
  const cancelBlock = runtimeSource.slice(
    runtimeSource.indexOf("const onCancel = useCallback"),
    runtimeSource.indexOf("// Replay a stored conversation"),
  );
  assert.match(cancelBlock, /Keep following the Run until Gateway persists/);
  assert.doesNotMatch(cancelBlock, /ctrl\?\.abort\(\)/);
  assert.match(runtimeSource, /authoritative LLM\/tool counts/);
});
