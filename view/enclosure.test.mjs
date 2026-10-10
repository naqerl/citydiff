import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { gunzipSync } from "node:zlib";
import { layoutCity } from "./layout.js";
import { enclosedBlocks, cityMetrics } from "./enclosure.js";

const box = (id, x, z, w = 10, d = 10) => ({ id, x, z, w, d });

test("the middle of a tight 3x3 grid is enclosed, and a street opens it", () => {
  const grid = [];
  for (let i = 0; i < 3; i++) for (let j = 0; j < 3; j++) grid.push(box(`b${i}${j}`, i * 10.5, j * 10.5));
  assert.deepEqual(enclosedBlocks(grid, new Map()), ["b11"]);
  const opened = grid.map((b) => (b.x > 15 ? { ...b, x: b.x + 2 } : b));
  assert.deepEqual(enclosedBlocks(opened, new Map()), []);
});

test("a side less than half covered is open; parents and children do not count", () => {
  const b = box("b", 0, 0);
  const around = [box("l", -10, 0), box("r", 10, 0), box("n", 0, -10), box("f", 0, 10, 4, 10)];
  assert.deepEqual(enclosedBlocks([b, ...around], new Map()), []);
  const parent = box("p", -20, -20, 50, 50);
  assert.deepEqual(enclosedBlocks([b, parent], new Map([["b", "p"]])), []);
});

// zeroshot.sh (github.com/the-open-engine/zeroshot @ c1e65ac): 896 internal
// packages, 12.8k declarations, trimmed to what the layout reads. On main
// 169 of its blocks were enclosed, the footprint was 55114 and the density
// (building footprint over the city's) 0.0767.
const zeroshot = JSON.parse(gunzipSync(readFileSync(new URL("./testdata/zeroshot.scene.json.gz", import.meta.url))));

test("no block of the zeroshot city is enclosed, and the city is no sparser than on main", () => {
  const laid = layoutCity(zeroshot.packages);
  const parentOf = new Map(zeroshot.packages.map((p) => [p.id, p.parent]));
  assert.deepEqual(enclosedBlocks(laid.packages, parentOf), []);
  const { footprint, density } = cityMetrics(laid);
  assert.ok(footprint <= 55114 * 1.001, `footprint ${footprint}`);
  assert.ok(density >= 0.0767, `density ${density}`);
});
