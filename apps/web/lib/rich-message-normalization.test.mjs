import assert from "node:assert/strict";
import test from "node:test";
import {
  normalizeQuestionPart,
  normalizeRichMessagePart,
  normalizeRunSummaryPart,
} from "./rich-message-normalization.ts";

test("optionless questions normalize to a safe empty options array", () => {
  assert.deepEqual(
    normalizeQuestionPart({
      type: "question",
      questionId: "question-1",
      version: 1,
      status: "pending",
      question: "Which database?",
    }),
    {
      type: "question",
      questionId: "question-1",
      version: 1,
      status: "pending",
      question: "Which database?",
      options: [],
      answer: null,
    },
  );
});

test("persisted snake-case question answers normalize for both renderers", () => {
  const normalized = normalizeRichMessagePart({
    type: "question",
    questionId: "question-1",
    version: 1,
    status: "answered",
    question: "Which database?",
    answer: { option_id: "postgres", text: "Use the existing cluster" },
  });

  assert.equal(normalized?.type, "question");
  assert.deepEqual(normalized?.answer, {
    optionId: "postgres",
    text: "Use the existing cluster",
  });
});

test("zero run-summary counters remain explicit zero values", () => {
  assert.deepEqual(
    normalizeRunSummaryPart({
      type: "run-summary",
      runId: "run-1",
      status: "success",
    }),
    {
      type: "run-summary",
      runId: "run-1",
      status: "success",
      modelLabel: "",
      durationMs: 0,
      toolCallCount: 0,
      llmCallCount: 0,
      errorCode: "",
    },
  );
});
