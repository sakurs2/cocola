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
  assert.match(
    threadSource,
    /aria-label=\{stopping \? "Stopping current run" : "Stop current run"\}/,
  );
  assert.doesNotMatch(threadSource, /<ComposerPrimitive\.Cancel asChild>\s*<PromptInput\.Send/s);
});

test("command execution uses a compact HeroUI activity card", () => {
  assert.match(railSource, /const CommandExecutionCard/);
  assert.match(railSource, /<Card\.Header/);
  assert.match(railSource, /max-h-72 overflow-auto/);
  assert.match(railSource, /formatAgentDuration\(elapsedSeconds \* 1000\)/);
  assert.match(railSource, /latestOutput/);
  assert.match(railSource, /motion-reduce:animate-none/);
  assert.match(railSource, /<HighlightedCode[\s\S]*?language="shell"/);
  assert.match(railSource, />\s*Command\s*</);
  assert.match(railSource, />\s*Output\s*</);
  assert.match(railSource, /aria-label=\{copied \? "Command copied" : "Copy command"\}/);
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
