import assert from "node:assert/strict";
import test from "node:test";

import {
  clearScheduledTaskPageCache,
  readScheduledTaskPageCache,
  writeScheduledTaskPageCache,
} from "./scheduled-task-page-cache.mjs";

test.beforeEach(() => clearScheduledTaskPageCache());

test("cache is isolated by authenticated owner", () => {
  writeScheduledTaskPageCache("user-a", {
    tasks: [{ id: "task-a" }],
    models: [{ id: "model-a" }],
  });

  assert.deepEqual(readScheduledTaskPageCache("user-a"), {
    tasks: [{ id: "task-a" }],
    models: [{ id: "model-a" }],
  });
  assert.equal(readScheduledTaskPageCache("user-b"), null);
});

test("cache snapshots input arrays and ignores empty owner ids", () => {
  const tasks = [{ id: "task-a" }];
  const models = [{ id: "model-a" }];
  writeScheduledTaskPageCache("user-a", { tasks, models });
  tasks.push({ id: "task-b" });
  models.length = 0;

  assert.deepEqual(readScheduledTaskPageCache("user-a"), {
    tasks: [{ id: "task-a" }],
    models: [{ id: "model-a" }],
  });
  assert.equal(writeScheduledTaskPageCache("", { tasks: [], models: [] }), null);
  assert.equal(readScheduledTaskPageCache(""), null);
});

test("partial writes preserve the other resource", () => {
  writeScheduledTaskPageCache("user-a", { tasks: [{ id: "task-a" }] });
  assert.deepEqual(readScheduledTaskPageCache("user-a"), {
    tasks: [{ id: "task-a" }],
    models: null,
  });

  writeScheduledTaskPageCache("user-a", { models: [{ id: "model-a" }] });
  assert.deepEqual(readScheduledTaskPageCache("user-a"), {
    tasks: [{ id: "task-a" }],
    models: [{ id: "model-a" }],
  });
});

test("clear removes one owner without evicting another", () => {
  writeScheduledTaskPageCache("user-a", { tasks: [{ id: "task-a" }], models: [] });
  writeScheduledTaskPageCache("user-b", { tasks: [{ id: "task-b" }], models: [] });

  clearScheduledTaskPageCache("user-a");

  assert.equal(readScheduledTaskPageCache("user-a"), null);
  assert.deepEqual(readScheduledTaskPageCache("user-b")?.tasks, [{ id: "task-b" }]);
});
