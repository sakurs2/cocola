import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modelsPageSource = await readFile(
  new URL("../app/admin/models/page.tsx", import.meta.url),
  "utf8",
);

test("provider form exposes and submits icon configuration", () => {
  assert.match(modelsPageSource, /icon_type: providerForm\.icon_type/);
  assert.match(modelsPageSource, /icon_slug: providerForm\.icon_slug/);
  assert.match(modelsPageSource, /icon_url: providerForm\.icon_url/);
  assert.match(modelsPageSource, /Appearance/);
  assert.match(modelsPageSource, /value=\{providerForm\.icon_type\}/);
  assert.match(modelsPageSource, /value=\{providerForm\.icon_slug\}/);
});

test("provider list renders persisted brand or image icons", () => {
  assert.match(modelsPageSource, /slug=\{provider\.icon_slug \|\| guess\}/);
  assert.match(
    modelsPageSource,
    /imageSrc=\{provider\.icon_type === "image" \? provider\.icon_url : undefined\}/,
  );
});
