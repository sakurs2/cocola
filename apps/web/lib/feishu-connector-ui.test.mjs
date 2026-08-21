import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { connectorResponseError } from "./connector-response-error.mjs";

const component = await readFile(
  new URL("../components/connectors/feishu-connector-card.tsx", import.meta.url),
  "utf8",
);
const workspaceComponent = await readFile(
  new URL("../components/connectors/workspace-feishu-connector-card.tsx", import.meta.url),
  "utf8",
);
const workspaceSetupDialog = workspaceComponent.slice(
  workspaceComponent.indexOf("function SetupDialog"),
  workspaceComponent.indexOf("function ManualAppDialog"),
);
const githubComponent = await readFile(
  new URL("../components/connectors/github-connector-card.tsx", import.meta.url),
  "utf8",
);
const summaryComponent = await readFile(
  new URL("../components/connectors/connector-summary-card.tsx", import.meta.url),
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
  for (const source of [component, workspaceComponent]) {
    assert.match(source, /type="password"/);
    assert.match(source, /body: JSON\.stringify\(\{[\s\S]*app_secret: appSecret/);
    assert.doesNotMatch(source, />\s*(?:App Secret:)?\s*\{appSecret\}\s*</);
  }
});

test("global Connectors keeps workspace Feishu separate from Agent bots", () => {
  assert.match(connectorsPage, /GitHubConnectorCard/);
  assert.match(connectorsPage, /WorkspaceFeishuConnectorCard/);
  assert.match(connectorsPage, /grid-cols-1/);
  assert.match(connectorsPage, /sm:grid-cols-\[repeat\(2,minmax\(0,300px\)\)\]/);
  assert.match(component, /t\("description"\)/);
  assert.doesNotMatch(component, /Give this Agent its own Feishu entry point/);
  assert.match(component, /`\/api\/agents\/\$\{encodeURIComponent\(agentId\)\}\/channels\/feishu`/);
  assert.match(workspaceComponent, /const endpoint = "\/api\/connectors\/feishu"/);
  assert.match(workspaceComponent, /src="\/feishu-logo\.svg"/);
  assert.match(workspaceComponent, /<Modal/);
  assert.doesNotMatch(workspaceComponent, /<Sheet/);
});

test("workspace Feishu setup keeps its two choices compact and avoids a duplicate close action", () => {
  assert.match(workspaceSetupDialog, /Modal\.Dialog className="mx-auto w-full max-w-\[360px\]"/);
  assert.match(workspaceSetupDialog, /className="h-12 w-full justify-start gap-3 bg-\[#3370FF\]/);
  assert.match(workspaceSetupDialog, /className="h-12 w-full justify-start gap-3 px-4"/);
  assert.doesNotMatch(workspaceSetupDialog, /min-h-20 flex-col/);
  assert.match(workspaceSetupDialog, /\{active \|\| failed \? \(/);
  assert.doesNotMatch(workspaceSetupDialog, /t\("done"\)/);
});

test("connector cards leave checking after a failed initial request and offer retry", () => {
  for (const source of [githubComponent, workspaceComponent, component]) {
    assert.match(source, /type ConnectionLoadState = "checking" \| "ready" \| "failed"/);
    assert.match(source, /setLoadState\("failed"\)/);
    assert.match(source, /t\("retry"\)/);
    assert.doesNotMatch(source, /setConnection\(null\)/);
  }
  assert.match(githubComponent, /loadState === "failed"/);
  assert.match(workspaceComponent, /loadState === "failed"/);
  assert.match(component, /loadState === "failed"/);
});

test("summary cards share compact structure while keeping provider branding", () => {
  assert.match(summaryComponent, /max-w-\[300px\]/);
  assert.match(summaryComponent, /data-provider=\{provider\}/);
  assert.match(summaryComponent, /bg-\[#3370FF\]/);
  assert.match(summaryComponent, /motion-reduce:animate-none/);
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
  assert.match(agentPage, /t\("compatibleModels"\)/);
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
  const workspaceRoot = await readFile(
    new URL("../app/api/connectors/feishu/route.ts", import.meta.url),
    "utf8",
  );
  const workspaceRegistration = await readFile(
    new URL("../app/api/connectors/feishu/registrations/[id]/route.ts", import.meta.url),
    "utf8",
  );
  assert.match(workspaceRoot, /"\/v1\/connectors\/feishu"/);
  assert.match(
    workspaceRegistration,
    /`\/v1\/connectors\/feishu\/registrations\/\$\{encodeURIComponent\(id\)\}`/,
  );
  assert.doesNotMatch(
    root + registrations + workspaceRoot + workspaceRegistration,
    /\[\.\.\.path\]/,
  );
});
