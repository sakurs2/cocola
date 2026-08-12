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
  assert.match(modelsPageSource, /t\("uploadImage"\)/);
  assert.match(modelsPageSource, /accept="image\/png,image\/jpeg,image\/webp/);
  assert.doesNotMatch(modelsPageSource, /label="Image URL"|placeholder="https:\/\/\.\.\."/);
  assert.match(modelsPageSource, /iconType=\{providerForm\.icon_type\}/);
  assert.match(modelsPageSource, /iconSlug=\{providerForm\.icon_slug\}/);
});

test("model and provider icon controls share one visual baseline", () => {
  assert.doesNotMatch(modelsPageSource, /t\("(?:source|brand|image)"\)/);
  assert.match(
    modelsPageSource,
    /const iconPickerControlClass = cn\(inputClass, "h-11 min-h-11 rounded-2xl"\)/,
  );
  assert.match(
    modelsPageSource,
    /grid min-w-0 items-start gap-3 sm:grid-cols-2[\s\S]*?<SelectControl[\s\S]*?<SelectControl/,
  );
  assert.match(modelsPageSource, /grid-cols-\[44px_minmax\(0,1fr\)\]/);
  assert.match(modelsPageSource, /className="flex size-11 items-center justify-center"/);
  assert.equal(modelsPageSource.match(/className=\{iconPickerControlClass\}/g)?.length, 2);
  assert.match(
    modelsPageSource,
    /className=\{cn\(iconPickerControlClass, "justify-start overflow-hidden"\)\}/,
  );
  assert.match(modelsPageSource, /className="flex min-w-0 flex-col items-start gap-1\.5"/);
});

test("model and provider icon uploads use the managed asset endpoint", () => {
  assert.match(modelsPageSource, /fetch\("\/api\/admin\/model-icons"/);
  assert.match(modelsPageSource, /providerIconFile/);
  assert.match(modelsPageSource, /modelIconFile/);
  assert.match(modelsPageSource, /t\("icon\.uploadFailed"\)/);
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
  assert.match(modelsPageSource, /<Field label=\{t\("model\.name"\)\}>/);
  assert.doesNotMatch(modelsPageSource, /<Field label=\{t\("model\.alias"\)\}/);
  assert.equal(
    modelsPageSource.match(
      /<Field label=\{t\("model\.name"\)\}>[\s\S]*?<Input[\s\S]*?className=\{inputClass\}/g,
    )?.length,
    2,
  );
  assert.match(
    modelsPageSource,
    /providerIDFromName\(modelForm\.label\)[\s\S]*?providerIDFromName\(modelForm\.real_model\)/,
  );
  assert.match(
    modelsPageSource,
    /<Field label=\{t\("model\.upstreamId"\)\}>[\s\S]*?<Input[\s\S]*?className=\{inputClass\}/,
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
