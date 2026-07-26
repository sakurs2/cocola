import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { Base64AttachmentAdapter } from "./base64-attachment-adapter.ts";
import {
  fileMatchesPromptStarterSlot,
  firstMissingPromptStarterSlot,
  layoutPromptStarterSlots,
  promptStarterBoundFileMarker,
  promptStarterSlotMarker,
  replacePromptStarterSlotValue,
  restorePromptStarterSlotValue,
} from "./prompt-starter.ts";

const spreadsheetSlot = {
  key: "spreadsheet",
  label: "Choose spreadsheet",
  accept: [".xlsx", ".csv"],
  required: true,
};

const binding = {
  slotKey: spreadsheetSlot.key,
  attachmentId: "attachment-1",
  filename: "sales.xlsx",
};

const threadSource = readFileSync(
  new URL("../components/assistant-ui/thread.tsx", import.meta.url),
  "utf8",
);

test("Prompt Starters fill the composer instead of sending a suggestion", () => {
  assert.match(threadSource, /composer\.setText\(starter\.prompt\)/);
  assert.doesNotMatch(threadSource, /<ThreadPrimitive\.Suggestion/);
  assert.match(threadSource, /aria-hidden=\{!composerIsEmpty\}/);
  assert.match(threadSource, /!composerIsEmpty && "invisible pointer-events-none"/);
  assert.doesNotMatch(threadSource, /\{composerIsEmpty \? \(/);
});

test("Prompt Starter slots preserve text positions before and after binding", () => {
  const marker = promptStarterSlotMarker(spreadsheetSlot);
  const unresolvedText = `Analyze ${marker} and summarize the results.`;
  const unresolved = layoutPromptStarterSlots(unresolvedText, [spreadsheetSlot], {});

  assert.equal(unresolved.segments.map((segment) => segment.text).join(""), unresolvedText);
  assert.deepEqual(unresolved.matchedSlotKeys, [spreadsheetSlot.key]);
  assert.equal(unresolved.segments.find((segment) => segment.slot)?.binding, undefined);

  const resolvedMarker = promptStarterBoundFileMarker(binding.filename);
  const resolvedText = `Analyze ${resolvedMarker} and summarize the results.`;
  const resolved = layoutPromptStarterSlots(resolvedText, [spreadsheetSlot], {
    [spreadsheetSlot.key]: binding,
  });

  assert.equal(resolved.segments.map((segment) => segment.text).join(""), resolvedText);
  assert.deepEqual(resolved.matchedSlotKeys, [spreadsheetSlot.key]);
  assert.equal(resolved.segments.find((segment) => segment.slot)?.binding, binding);
});

test("Selecting and replacing a Prompt Starter file updates only its slot", () => {
  const marker = promptStarterSlotMarker(spreadsheetSlot);
  assert.equal(
    replacePromptStarterSlotValue(
      `Analyze ${marker} and keep this ${marker} note.`,
      spreadsheetSlot,
      undefined,
      binding.filename,
    ),
    `Analyze ${promptStarterBoundFileMarker(binding.filename)} and keep this ${marker} note.`,
  );
  assert.equal(
    replacePromptStarterSlotValue(
      `Analyze ${promptStarterBoundFileMarker(binding.filename)}.`,
      spreadsheetSlot,
      binding,
      "forecast.csv",
    ),
    "Analyze @forecast.csv.",
  );
  assert.equal(
    restorePromptStarterSlotValue(
      `Analyze ${promptStarterBoundFileMarker(binding.filename)}.`,
      spreadsheetSlot,
      binding,
    ),
    `Analyze ${marker}.`,
  );
});

test("Excel analysis accepts XLSX and CSV while rejecting unrelated files", () => {
  assert.equal(
    fileMatchesPromptStarterSlot(
      { name: "sales.XLSX", type: "application/octet-stream" },
      spreadsheetSlot,
    ),
    true,
  );
  assert.equal(
    fileMatchesPromptStarterSlot({ name: "sales.csv", type: "text/csv" }, spreadsheetSlot),
    true,
  );
  assert.equal(
    fileMatchesPromptStarterSlot({ name: "notes.txt", type: "text/plain" }, spreadsheetSlot),
    false,
  );
  assert.match(new Base64AttachmentAdapter().accept, /\.xlsx/);
});

test("Required Prompt Starter slots block sending until they are bound", () => {
  assert.equal(firstMissingPromptStarterSlot([spreadsheetSlot], {}), spreadsheetSlot);
  assert.equal(
    firstMissingPromptStarterSlot([spreadsheetSlot], {
      [spreadsheetSlot.key]: binding,
    }),
    undefined,
  );
  assert.equal(
    firstMissingPromptStarterSlot(
      [spreadsheetSlot],
      {
        [spreadsheetSlot.key]: binding,
      },
      () => false,
    ),
    spreadsheetSlot,
  );
});
