import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const nativeDialogPattern = /\bwindow\.(?:alert|confirm|prompt)\s*\(/;

test("Wiki interactions do not use browser-native dialogs", async () => {
  const files = [
    new URL("../components/wiki/wiki-workspace.tsx", import.meta.url),
    new URL("../components/wiki/wiki-markdown-editor.tsx", import.meta.url),
    new URL("../components/assistant-ui/app-sidebar.tsx", import.meta.url),
    new URL("../components/assistant-ui/command-palette.tsx", import.meta.url),
    new URL("../components/assistant-ui/workspace-unsaved-changes.tsx", import.meta.url),
  ];

  for (const file of files) {
    const source = await readFile(file, "utf8");
    assert.doesNotMatch(source, nativeDialogPattern, file.pathname);
  }
});
