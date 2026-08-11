import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modelsPageSource = await readFile(
  new URL("../app/admin/models/page.tsx", import.meta.url),
  "utf8",
);
const iconRouteSource = await readFile(
  new URL("../app/api/model-icons/[slug]/route.ts", import.meta.url),
  "utf8",
);

test("provider form exposes and submits icon configuration", () => {
  assert.match(modelsPageSource, /icon_type: providerForm\.icon_type/);
  assert.match(modelsPageSource, /icon_slug: providerForm\.icon_slug/);
  assert.match(modelsPageSource, /icon_url: iconURL/);
  assert.doesNotMatch(modelsPageSource, /<DisclosureSummary>Appearance/);
  assert.match(modelsPageSource, /function IconPicker/);
  assert.match(modelsPageSource, /Upload image/);
  assert.match(modelsPageSource, /accept="image\/png,image\/jpeg,image\/webp/);
  assert.doesNotMatch(modelsPageSource, /label="Image URL"|placeholder="https:\/\/\.\.\."/);
  assert.match(modelsPageSource, /iconType=\{providerForm\.icon_type\}/);
  assert.match(modelsPageSource, /iconSlug=\{providerForm\.icon_slug\}/);
});

test("model and provider icon uploads use the managed asset endpoint", () => {
  assert.match(modelsPageSource, /fetch\("\/api\/admin\/model-icons"/);
  assert.match(modelsPageSource, /providerIconFile/);
  assert.match(modelsPageSource, /modelIconFile/);
  assert.match(modelsPageSource, /Model icons must be 1 MB or smaller/);
  assert.match(iconRouteSource, /MANAGED_ICON_ID/);
  assert.match(iconRouteSource, /getManagedIcon/);
  assert.match(iconRouteSource, /\/admin\/model-icons\/\$\{id\}/);
});

test("provider list renders persisted brand or image icons", () => {
  assert.match(modelsPageSource, /slug=\{provider\.icon_slug \|\| guess\}/);
  assert.match(
    modelsPageSource,
    /imageSrc=\{provider\.icon_type === "image" \? provider\.icon_url : undefined\}/,
  );
});

test("model form exposes one consistently styled model name", () => {
  assert.match(modelsPageSource, /<Field label="Model name">/);
  assert.doesNotMatch(modelsPageSource, /<Field label="Alias"/);
  assert.equal(
    modelsPageSource.match(
      /<Field label="Model name">[\s\S]*?<Input[\s\S]*?className=\{inputClass\}/g,
    )?.length,
    2,
  );
  assert.match(
    modelsPageSource,
    /providerIDFromName\(modelForm\.label\)[\s\S]*?providerIDFromName\(modelForm\.real_model\)/,
  );
  assert.match(
    modelsPageSource,
    /<Field label="Upstream model ID">[\s\S]*?<Input[\s\S]*?className=\{inputClass\}/,
  );
});

test("provider credentials use one input boundary", () => {
  assert.doesNotMatch(
    modelsPageSource,
    /flex items-center gap-2 rounded-xl border border-separator bg-background px-3/,
  );
  assert.match(modelsPageSource, /className=\{cn\(inputClass, "pl-10"\)\}/);
  assert.match(modelsPageSource, /pointer-events-none absolute left-3 top-1\/2/);
});
