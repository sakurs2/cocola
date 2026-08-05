import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { connectorResponseError } from "./connector-response-error.mjs";

const component = await readFile(
  new URL("../components/connectors/feishu-connector-card.tsx", import.meta.url),
  "utf8",
);
const connectorsPage = await readFile(
  new URL("../app/connectors/page.tsx", import.meta.url),
  "utf8",
);
const agentPage = await readFile(new URL("../app/agents/[id]/page.tsx", import.meta.url), "utf8");

test("Feishu connector uses owned dialogs and cleans up polling", () => {
  assert.doesNotMatch(component, /\bwindow\.(?:alert|confirm|prompt)\s*\(/);
  assert.match(component, /setFlow\(next\.registration \?\? null\)/);
  assert.match(component, /attempts >= 60/);
  assert.match(component, /setTimeout\(\(\) => void poll\(\), 2000\)/);
  assert.match(component, /controller\.abort\(\)/);
  assert.match(component, /if \(timer\) clearTimeout\(timer\)/);
  assert.match(component, /return \(\) => window\.clearInterval\(timer\)/);
});

test("manual App Secret stays in a password input and is not rendered", () => {
  assert.match(component, /type="password"/);
  assert.match(component, /body: JSON\.stringify\(\{[\s\S]*app_secret: appSecret/);
  assert.doesNotMatch(component, />\s*(?:App Secret:)?\s*\{appSecret\}\s*</);
});

test("global Connectors owns GitHub while each Agent owns its Feishu bot", () => {
  assert.match(connectorsPage, /not_configured: \{[\s\S]*?label: "Not connected"/);
  assert.match(connectorsPage, /mt-4 flex items-center gap-2 text-xs/);
  assert.doesNotMatch(connectorsPage, /FeishuConnectorCard/);
  assert.match(component, /Give this Agent its own Feishu entry point/);
  assert.match(component, /One Agent can have one Bot/);
  assert.match(component, /`\/api\/agents\/\$\{encodeURIComponent\(agentId\)\}\/channels\/feishu`/);
});

test("connector cards leave checking after a failed initial request and offer retry", () => {
  for (const source of [connectorsPage, component]) {
    assert.match(source, /type ConnectionLoadState = "checking" \| "ready" \| "failed"/);
    assert.match(source, /setLoadState\("failed"\)/);
    assert.match(source, /\bRetry\b/);
    assert.doesNotMatch(source, /setConnection\(null\)/);
  }
  assert.match(connectorsPage, /Connection check failed/);
  assert.match(connectorsPage, /!connection && loadState === "failed"/);
  assert.match(connectorsPage, /connection && loadState === "failed"/);
  assert.match(component, /loadState === "failed"/);
});

test("connector errors accept proxy string and structured error responses", async () => {
  assert.equal(
    await connectorResponseError(
      new Response(JSON.stringify({ error: "admin-api unreachable" }), { status: 502 }),
    ),
    "admin-api unreachable",
  );
  assert.equal(
    await connectorResponseError(
      new Response(JSON.stringify({ error: { message: "gateway unreachable" } }), {
        status: 502,
      }),
    ),
    "gateway unreachable",
  );
  assert.equal(
    await connectorResponseError(new Response("not json", { status: 503 })),
    "Request failed (503)",
  );
});

test("the Agent owns the fixed model used by its Feishu bot", () => {
  assert.match(agentPage, /fetch\("\/api\/models"/);
  assert.match(agentPage, /fetch\("\/api\/agent-runtimes"/);
  assert.match(agentPage, /model\.protocols\.includes\(runtime\.modelProtocol\)/);
  assert.match(agentPage, /method: "PATCH"/);
  assert.match(agentPage, /model_route_id: selectedModel\.id/);
  assert.match(agentPage, /model_alias: selectedModel\.alias/);
  assert.match(agentPage, /Conversations using this Agent always use this compatible model/);
  assert.doesNotMatch(component, /fetch\("\/api\/models"/);
});

test("Feishu proxy routes are fixed rather than arbitrary catch-all paths", async () => {
  const root = await readFile(
    new URL("../app/api/agents/[id]/channels/feishu/route.ts", import.meta.url),
    "utf8",
  );
  const registrations = await readFile(
    new URL(
      "../app/api/agents/[id]/channels/feishu/registrations/[flowId]/route.ts",
      import.meta.url,
    ),
    "utf8",
  );
  assert.match(root, /`\/v1\/agents\/\$\{encodeURIComponent\(id\)\}\/channels\/feishu`/);
  assert.match(root, /export async function DELETE/);
  assert.match(
    registrations,
    /`\/v1\/agents\/\$\{encodeURIComponent\(id\)\}\/channels\/feishu\/registrations\/\$\{encodeURIComponent\(/,
  );
  assert.doesNotMatch(root + registrations, /\[\.\.\.path\]/);
});
