import assert from "node:assert/strict";
import test from "node:test";
import { scheduledTaskResultView } from "./scheduled-task-result.ts";

test("turns worker heartbeat errors into a compact result while preserving detail", () => {
  const detail = "scheduled task run expired after worker heartbeat timeout";
  assert.deepEqual(scheduledTaskResultView({ last_error: detail }), {
    key: "workerTimeout",
    detail,
    tone: "danger",
  });
});

test("maps successful and not-run states to semantic result tones", () => {
  assert.deepEqual(scheduledTaskResultView({ last_status: "completed", run_count: 1 }), {
    key: "succeeded",
    tone: "success",
  });
  assert.deepEqual(scheduledTaskResultView({ run_count: 0 }), {
    key: "notRun",
    tone: "default",
  });
});
