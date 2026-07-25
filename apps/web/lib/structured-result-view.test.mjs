import assert from "node:assert/strict";
import test from "node:test";
import {
  buildListItems,
  buildMetrics,
  buildSummaryView,
  buildTableView,
  humanizeResultKey,
} from "./structured-result-view.ts";

test("result keys become readable labels without damaging acronyms", () => {
  assert.equal(humanizeResultKey("RAM_overhead"), "RAM overhead");
  assert.equal(humanizeResultKey("modelRouteId"), "Model route ID");
});

test("summary presentation separates the lead, status, details, and extra fields", () => {
  assert.deepEqual(
    buildSummaryView({
      title: "Build result",
      summary: "The focused checks passed.",
      status: "Ready",
      details: [{ label: "Tests", value: 42 }],
      elapsed_ms: 850,
    }),
    {
      lead: "The focused checks passed.",
      status: "Ready",
      fields: [
        { key: "detail:0", label: "Tests", value: 42 },
        {
          key: "summary:elapsed_ms:0",
          label: "Elapsed ms",
          value: 850,
        },
      ],
    },
  );
});

test("summary presentation preserves noncanonical fields instead of dropping data", () => {
  assert.deepEqual(buildSummaryView({ summary: 3, details: "Unavailable" }), {
    lead: "",
    status: "",
    fields: [
      { key: "summary:summary:0", label: "Summary", value: 3 },
      {
        key: "summary:details:1",
        label: "Details",
        value: "Unavailable",
      },
    ],
  });
});

test("list presentation promotes object names and preserves their remaining fields", () => {
  assert.deepEqual(
    buildListItems({
      items: [
        { name: "PostgreSQL", status: "Recommended", replicas: 2 },
        "Keep SQLite for local prototypes",
      ],
    }),
    [
      {
        key: "item:0",
        title: "PostgreSQL",
        fields: [
          { key: "item:0:status:0", label: "Status", value: "Recommended" },
          { key: "item:0:replicas:1", label: "Replicas", value: 2 },
        ],
      },
      {
        key: "item:1",
        title: "Keep SQLite for local prototypes",
        fields: [],
      },
    ],
  );
});

test("metrics presentation preserves units and trends", () => {
  assert.deepEqual(
    buildMetrics({
      metrics: [{ label: "Passed", value: 248, unit: "tests", trend: "+12" }],
    }),
    [
      {
        key: "metric:0",
        label: "Passed",
        value: 248,
        unit: "tests",
        trend: "+12",
      },
    ],
  );
});

test("table presentation supports string and keyed column contracts", () => {
  assert.deepEqual(
    buildTableView({
      columns: ["Name", { key: "status", label: "Build status" }],
      rows: [{ Name: "API", status: "Ready" }],
    }),
    {
      columns: [
        { key: "Name:0", dataKey: "Name", label: "Name" },
        { key: "status:1", dataKey: "status", label: "Build status" },
      ],
      rows: [{ Name: "API", status: "Ready" }],
    },
  );
});
