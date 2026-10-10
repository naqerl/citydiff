import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { gunzipSync } from "node:zlib";
import { layoutCity } from "./layout.js";
import { chooseDistricts, districtStatus, depEdges, aggregateEdges, labelBox, placeLabels, BirdToggle, DISTRICT_CAP, DISTRICT_MIN } from "./bird.js";
import { birdAction } from "./keys.js";

const zeroshot = JSON.parse(gunzipSync(readFileSync(new URL("./testdata/zeroshot.scene.json.gz", import.meta.url)))).packages;
const repo = new URL("..", import.meta.url).pathname;
const barsePath = process.env.CITYDIFF_BARSE || "/workspace/barse-scene";
const haveBarse = existsSync(join(barsePath, ".git"));
const barse = haveBarse
  ? JSON.parse(execFileSync("go", ["run", "./cmd/cli", "-path", barsePath, "-range", "dbdafc006b07ea29c2735fb00be0e46a6c4842c4..061e9aed03e2a2691cf9a596b193ab0d0f8f8f75", "-scene"], { cwd: repo, maxBuffer: 1 << 28 })).packages
  : null;

function checkCut(packages) {
  const laid = layoutCity(packages);
  const cut = chooseDistricts(laid, packages);
  assert.ok(cut.districts.length <= DISTRICT_CAP, `${cut.districts.length} districts`);
  const span = Math.max(laid.bounds.maxX - laid.bounds.minX, laid.bounds.maxZ - laid.bounds.minZ);
  const roots = new Set(laid.packages.filter((x) => !packages.find((p) => p.id === x.id).parent || !laid.packages.some((y) => y.id === packages.find((p) => p.id === x.id).parent)).map((x) => x.id));
  for (const d of cut.districts) if (!roots.has(d.id)) assert.ok(Math.min(d.w, d.d) >= DISTRICT_MIN * span, d.id + " too small");
  for (const x of laid.packages) assert.ok(cut.districtOf.has(x.id), x.id + " has no district");
  return { laid, cut };
}

test("district level: zeroshot is cut deeper than its 6 top packages, within the cap and the size floor", () => {
  const { cut } = checkCut(zeroshot);
  assert.equal(cut.districts.length, 60);
  assert.equal(cut.depth, 2);
});

test("district level: barse", { skip: !haveBarse && "no barse checkout" }, () => {
  const { cut } = checkCut(barse);
  assert.ok(cut.districts.length > 20 && cut.depth >= 2, `${cut.districts.length} at ${cut.depth}`);
});

test("a small tree splits fully; a tighter cap keeps it at the top", () => {
  const packages = [{ id: "m" }, { id: "m/a", parent: "m", entities: Array.from({ length: 30 }, (_, i) => ({ id: "a" + i, kind: "function" })) }, { id: "m/b", parent: "m", entities: Array.from({ length: 30 }, (_, i) => ({ id: "b" + i, kind: "function" })) }];
  const laid = layoutCity(packages);
  assert.deepEqual(chooseDistricts(laid, packages).districts.map((d) => d.id).sort(), ["m", "m/a", "m/b"]);
  assert.equal(chooseDistricts(laid, packages).districts.find((d) => d.id === "m").base, true);
  assert.deepEqual(chooseDistricts(laid, packages, { cap: 2 }).districts.map((d) => d.id), ["m"]);
});

function checkEdges(packages, mode) {
  const { cut } = checkCut(packages);
  const edges = depEdges(packages, mode);
  const pairs = aggregateEdges(edges, cut.districtOf);
  const distinct = new Set();
  let self = 0;
  for (const e of edges) {
    const a = cut.districtOf.get(e.from);
    const b = cut.districtOf.get(e.to);
    if (a === b) self++;
    else distinct.add([a, b].sort().join("\0"));
  }
  assert.equal(pairs.length, distinct.size, "one curve per district pair");
  assert.equal(pairs.reduce((n, p) => n + p.count, 0) + self, edges.length, "the counts add up to the edges");
  return { edges: edges.length, curves: pairs.length, self };
}

test("aggregation on zeroshot: one curve per distinct pair, counts sum to the edges", () => {
  // The district cut follows the layout weights, and variables are weighed
  // as declarations now, so the cut (and with it the self-edge count) moved
  // when const and static entries stopped being dropped.
  assert.deepEqual(checkEdges(zeroshot, "overview"), { edges: 2009, curves: 215, self: 1300 });
});

test("aggregation on barse, both modes", { skip: !haveBarse && "no barse checkout" }, () => {
  const full = checkEdges(barse, "overview");
  const changes = checkEdges(barse, "overlay");
  assert.ok(full.curves > changes.curves && changes.edges > 0, JSON.stringify({ full, changes }));
});

test("aggregation drops self-edges, merges both directions and keeps the strongest change", () => {
  const of = new Map([["a1", "A"], ["a2", "A"], ["b", "B"]]);
  const pairs = aggregateEdges([{ from: "a1", to: "a2" }, { from: "a1", to: "b", change: "same" }, { from: "b", to: "a2", change: "added" }], of);
  assert.deepEqual(pairs, [{ from: "A", to: "B", count: 2, change: "added" }]);
});

test("status tint", () => {
  const e = (...c) => c.map((change) => ({ change }));
  assert.equal(districtStatus(e("same", "same")).status, "same");
  assert.equal(districtStatus(e("added", "added")).status, "added");
  assert.equal(districtStatus(e("removed")).status, "removed");
  assert.equal(districtStatus(e("added", "same")).status, "modified");
  assert.equal(districtStatus(e("added", "removed")).status, "modified");
  assert.equal(districtStatus(e("moved")).status, "modified");
  assert.equal(districtStatus([]).status, "same");
  assert.deepEqual(districtStatus(e("added", "modified", "same", "same")).share, { added: 0.25, modified: 0.25, removed: 0 });
});

test("label collision pass: the bigger label wins, a smaller one shrinks or hides", () => {
  const big = { id: "big", x: 0, z: 0, size: 4, w: 20, h: 4, priority: 10 };
  const near = { id: "near", x: 0, z: 3, size: 4, w: 20, h: 4, priority: 5 };
  const far = { id: "far", x: 100, z: 0, size: 4, w: 20, h: 4, priority: 1 };
  const out = placeLabels([near, far, big], 1);
  assert.equal(out.get("big").hidden, false);
  assert.equal(out.get("big").scale, 1);
  assert.equal(out.get("near").hidden, false);
  assert.ok(out.get("near").scale < 1);
  assert.equal(out.get("far").scale, 1);
  const blocked = placeLabels([big, { ...near, z: 0 }], 1);
  assert.equal(blocked.get("near").hidden, true);
  assert.equal(placeLabels([{ ...far, avoid: [{ x0: 90, x1: 110, z0: -5, z1: 5 }] }], 1).get("far").hidden, true);
  assert.equal(placeLabels([{ ...far, size: 0.5 }], 1).get("far").hidden, true);
  const fit = labelBox("generator", 30, 10, 99);
  assert.ok(fit.w <= 30 * 0.85 + 1e-9 && fit.h <= 10 * 0.4 + 1e-9);
});

test("no two district labels overlap on zeroshot", () => {
  const { cut } = checkCut(zeroshot);
  const items = cut.districts.map((d) => ({ id: d.id, x: d.x + d.w / 2, z: d.z + d.d / 2, priority: d.w * d.d, ...labelBox(d.name || d.id, d.w, d.d, 8) }));
  const out = placeLabels(items, 0.9);
  const shown = items.filter((i) => !out.get(i.id).hidden).map((i) => {
    const k = out.get(i.id).scale;
    return { x0: i.x - (i.w * k) / 2, x1: i.x + (i.w * k) / 2, z0: i.z - (i.h * k) / 2, z1: i.z + (i.h * k) / 2 };
  });
  for (let i = 0; i < shown.length; i++) for (let j = i + 1; j < shown.length; j++) {
    const a = shown[i], b = shown[j];
    assert.ok(!(a.x0 < b.x1 && a.x1 > b.x0 && a.z0 < b.z1 && a.z1 > b.z0), "overlap");
  }
  assert.ok(shown.length > 30, `${shown.length} shown`);
});

test("toggle restores exactly the camera it left", () => {
  const t = new BirdToggle();
  const cam = { pos: [1, 2, 3], target: [4, 5, 6], up: [0, 1, 0], zoom: 1.5, extra: { maxDistance: 400, mode: "overlay" } };
  assert.equal(t.enter(cam), true);
  assert.equal(t.enter({ ...cam, pos: [9, 9, 9] }), false, "a second enter does not overwrite");
  cam.pos[0] = 99;
  cam.extra.maxDistance = 1;
  assert.deepEqual(t.leave(), { pos: [1, 2, 3], target: [4, 5, 6], up: [0, 1, 0], zoom: 1.5, extra: { maxDistance: 400, mode: "overlay" } });
  assert.equal(t.on, false);
  assert.equal(t.leave(), null);
});

test("y toggles bird view, but not while typing, with modifiers or on repeat", () => {
  const k = (key, extra = {}) => ({ key, target: { tagName: "CANVAS" }, ...extra });
  assert.equal(birdAction(k("y")), true);
  assert.equal(birdAction(k("y", { target: { tagName: "INPUT" } })), false);
  assert.equal(birdAction(k("y", { ctrlKey: true })), false);
  assert.equal(birdAction(k("y", { repeat: true })), false);
  assert.equal(birdAction(k("u")), false);
});
