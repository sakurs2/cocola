import assert from "node:assert/strict";
import test from "node:test";

import {
  finalAgentOutputText,
  formatAgentDuration,
  inferAgentDurationMs,
  splitAgentTurnParts,
} from "./agent-turn-summary.mjs";

test("short text-only answers remain fully visible without a process summary", () => {
  assert.deepEqual(splitAgentTurnParts([{ type: "text", text: "Done." }]), {
    processIndices: [],
    outputIndices: [0],
    hasProcess: false,
  });
});

test("reasoning, tools, progress, and intermediate text form the process section", () => {
  const parts = [
    { type: "text", text: "I will inspect the project." },
    { type: "reasoning", text: "Finding the relevant files." },
    { type: "tool-call", toolName: "Read" },
    { type: "text", text: "I found the entry point." },
    { type: "progress", items: [] },
    { type: "tool-call", toolName: "Edit" },
    { type: "text", text: "Implemented and verified." },
  ];

  assert.deepEqual(splitAgentTurnParts(parts), {
    processIndices: [0, 1, 2, 3, 4, 5],
    outputIndices: [6],
    hasProcess: true,
  });
});

test("file cards always stay in final output even when emitted before the last tool", () => {
  const parts = [
    { type: "reasoning", text: "Creating a report." },
    { type: "file", filename: "report.pdf", data: "/files/report.pdf" },
    { type: "tool-call", toolName: "Verify" },
    { type: "text", text: "The report is ready." },
  ];

  assert.deepEqual(splitAgentTurnParts(parts), {
    processIndices: [0, 2],
    outputIndices: [1, 3],
    hasProcess: true,
  });
});

test("terminal error or interruption text remains visible after process steps collapse", () => {
  const parts = [
    { type: "tool-call", toolName: "Bash" },
    { type: "text", text: "Run was interrupted before completion." },
  ];

  assert.deepEqual(splitAgentTurnParts(parts), {
    processIndices: [0],
    outputIndices: [1],
    hasProcess: true,
  });
});

test("environment preparation alone is a process step", () => {
  assert.deepEqual(splitAgentTurnParts([{ type: "text", text: "Ready." }], true), {
    processIndices: [],
    outputIndices: [0],
    hasProcess: true,
  });
});

test("duration formatting uses seconds, minute-seconds, and hour-minutes", () => {
  assert.equal(formatAgentDuration(59_000), "59s");
  assert.equal(formatAgentDuration(60_000), "1m 0s");
  assert.equal(formatAgentDuration(118_000), "1m 58s");
  assert.equal(formatAgentDuration(3_840_000), "1h 4m");
  assert.equal(formatAgentDuration(Number.NaN), "");
  assert.equal(formatAgentDuration(-1), "");
});

test("duration inference prefers metadata and falls back to adjacent timestamps", () => {
  assert.equal(inferAgentDurationMs(8_000, "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z"), 8_000);
  assert.equal(
    inferAgentDurationMs(undefined, "2026-01-01T00:00:00Z", "2026-01-01T00:01:58Z"),
    118_000,
  );
  assert.equal(inferAgentDurationMs(undefined, 1_000, 9_000), 8_000);
  assert.equal(inferAgentDurationMs(undefined, "invalid", "2026-01-01T00:01:58Z"), undefined);
  assert.equal(inferAgentDurationMs(undefined, 9_000, 1_000), undefined);
});

test("copy text contains only final text and file references", () => {
  const parts = [
    { type: "text", text: "I will create it." },
    { type: "tool-call", toolName: "Write" },
    { type: "file", filename: "report.pdf", data: '{"url":"/files/report.pdf"}' },
    { type: "text", text: "The report is ready." },
  ];
  const { outputIndices } = splitAgentTurnParts(parts);

  assert.equal(
    finalAgentOutputText(parts, outputIndices),
    "[file] report.pdf /files/report.pdf\n\nThe report is ready.",
  );
});
