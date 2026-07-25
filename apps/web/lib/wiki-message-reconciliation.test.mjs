import assert from "node:assert/strict";
import test from "node:test";

import { reconcileWikiUserMessage } from "./wiki-message-reconciliation.ts";

test("Wiki message reconciliation swaps only the optimistic user message", () => {
  const optimisticUser = {
    id: "optimistic-user",
    role: "user",
    parts: [
      {
        type: "wiki-file",
        wikiNodeId: "node-1",
        downloadUrl: "/api/wiki/files/node-1/download",
      },
    ],
  };
  const streamingAssistant = {
    id: "run-1-assistant",
    role: "assistant",
    parts: [{ type: "text", text: "working" }],
  };
  const durableUser = {
    id: "run-1-user",
    role: "user",
    parts: [
      {
        type: "wiki-file",
        wikiNodeId: "node-1",
        wikiVersionId: "version-1",
        downloadUrl: "/api/wiki/versions/version-1/download",
      },
    ],
  };

  const reconciled = reconcileWikiUserMessage(
    [optimisticUser, streamingAssistant],
    [durableUser],
    optimisticUser.id,
    durableUser.id,
  );

  assert.strictEqual(reconciled[0], durableUser);
  assert.strictEqual(reconciled[1], streamingAssistant);
});

test("Wiki message reconciliation is a no-op until the durable user message exists", () => {
  const current = [{ id: "optimistic-user", role: "user", parts: [] }];

  assert.strictEqual(
    reconcileWikiUserMessage(current, [], "optimistic-user", "run-1-user"),
    current,
  );
});
