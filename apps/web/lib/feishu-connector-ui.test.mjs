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

test("Feishu connector uses owned dialogs and cleans up polling", () => {
  assert.doesNotMatch(component, /\bwindow\.(?:alert|confirm|prompt)\s*\(/);
  assert.match(component, /if \(next\.registration\) setFlow\(next\.registration\)/);
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

test("connector cards expose aligned status rows and the Feishu brand logo", () => {
  assert.match(connectorsPage, /not_configured: \{ label: "Not connected"/);
  assert.match(connectorsPage, /mt-4 flex items-center gap-2 text-xs/);
  assert.match(component, /src="\/feishu-logo\.svg"/);
  assert.doesNotMatch(component, /function FeishuIcon/);
});

test("connector cards leave checking after a failed initial request and offer retry", () => {
  for (const source of [connectorsPage, component]) {
    assert.match(source, /type ConnectionLoadState = "checking" \| "ready" \| "failed"/);
    assert.match(source, /setLoadState\("failed"\)/);
    assert.match(source, /Connection check failed/);
    assert.match(source, /!connection && loadState === "failed"/);
    assert.match(source, />\s*Retry\s*</);
    assert.match(source, /connection && loadState === "failed"/);
    assert.doesNotMatch(source, /setConnection\(null\)/);
  }
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

test("Feishu settings select and persist the model used by new messages", () => {
  assert.match(component, /aria-label="Configure Feishu"/);
  assert.match(component, /<Dialog\.Title[^>]*>Feishu settings<\/Dialog\.Title>/);
  assert.match(component, /fetch\("\/api\/models"/);
  assert.match(component, /fetch\("\/api\/agent-runtimes"/);
  assert.match(component, /model\.protocols\.includes\(runtime\.model_protocol\)/);
  assert.match(component, /method: "PATCH"/);
  assert.match(component, /model_route_id: model\.id/);
  assert.match(component, /model_alias: model\.alias/);
  assert.match(component, /The change applies to the next new message/);
});

test("Feishu proxy routes are fixed rather than arbitrary catch-all paths", async () => {
  const root = await readFile(
    new URL("../app/api/connectors/feishu/route.ts", import.meta.url),
    "utf8",
  );
  const registrations = await readFile(
    new URL("../app/api/connectors/feishu/registrations/[id]/route.ts", import.meta.url),
    "utf8",
  );
  assert.match(root, /"\/v1\/connectors\/feishu"/);
  assert.match(root, /export async function PATCH/);
  assert.match(
    registrations,
    /`\/v1\/connectors\/feishu\/registrations\/\$\{encodeURIComponent\(id\)\}`/,
  );
  assert.doesNotMatch(root + registrations, /\[\.\.\.path\]/);
});
