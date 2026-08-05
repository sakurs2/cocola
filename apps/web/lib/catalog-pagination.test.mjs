import assert from "node:assert/strict";
import test from "node:test";
import { paginateCatalog } from "./catalog-pagination.ts";

test("catalog pagination returns the requested bounded page", () => {
  const result = paginateCatalog(
    Array.from({ length: 20 }, (_, index) => index + 1),
    2,
    9,
  );

  assert.deepEqual(result.items, [10, 11, 12, 13, 14, 15, 16, 17, 18]);
  assert.equal(result.page, 2);
  assert.equal(result.pageCount, 3);
  assert.equal(result.start, 9);
  assert.equal(result.end, 18);
});

test("catalog pagination clamps a stale page after filtering or deletion", () => {
  const result = paginateCatalog(["shared", "personal"], 4, 9);

  assert.deepEqual(result.items, ["shared", "personal"]);
  assert.equal(result.page, 1);
  assert.equal(result.pageCount, 1);
  assert.equal(result.start, 0);
  assert.equal(result.end, 2);
});
