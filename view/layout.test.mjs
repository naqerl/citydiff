import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { layoutCity } from "./layout.js";

const repo = join(dirname(fileURLToPath(import.meta.url)), "..");
const EPS = 1e-6;

function boxesOf(laid) {
  const boxes = [];
  for (const pkg of laid.packages) {
    boxes.push({ id: "package " + pkg.id, ...pkg });
    for (const entity of pkg.entities) boxes.push({ id: entity.kind + " " + entity.id, ...entity });
  }
  return boxes;
}

function overlaps(laid) {
  const boxes = boxesOf(laid).sort((a, b) => a.x - b.x);
  const hit = (a, b) =>
    a.y + EPS < b.y + b.h && b.y + EPS < a.y + a.h &&
    a.z + EPS < b.z + b.d && b.z + EPS < a.z + a.d;
  const out = [];
  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length && boxes[j].x + EPS < boxes[i].x + boxes[i].w; j++) {
      if (hit(boxes[i], boxes[j])) out.push(boxes[i].id + " / " + boxes[j].id);
    }
  }
  return out;
}

function escapes(packages, laid) {
  const parentOf = new Map(packages.filter((pkg) => !pkg.external).map((pkg) => [pkg.id, pkg.parent]));
  const byId = new Map(laid.packages.map((box) => [box.id, box]));
  const inside = (a, b) => a.x >= b.x - EPS && a.z >= b.z - EPS && a.x + a.w <= b.x + b.w + EPS && a.z + a.d <= b.z + b.d + EPS;
  const out = [];
  for (const box of laid.packages) {
    const parent = byId.get(parentOf.get(box.id));
    if (parent && !inside(box, parent)) out.push("package " + box.id);
    for (const entity of box.entities) if (!inside(entity, box)) out.push(entity.kind + " " + entity.id);
  }
  return out;
}

function assertClean(packages) {
  const laid = layoutCity(packages);
  assert.deepEqual(overlaps(laid).slice(0, 10), []);
  assert.deepEqual(escapes(packages, laid).slice(0, 10), []);
}

function scene(args) {
  return JSON.parse(execFileSync("go", ["run", "./cmd/cli", ...args, "-scene"], { cwd: repo, maxBuffer: 1 << 28 })).packages;
}

test("a crowded type and package do not spill onto their neighbours", () => {
  const methods = Array.from({ length: 40 }, (_, i) => ({ id: "m" + i, kind: "method", parent: "T", bodyBytes: 500 }));
  const funcs = Array.from({ length: 400 }, (_, i) => ({ id: "f" + i, kind: "function", bodyBytes: 100 }));
  const packages = [
    { id: "m", entities: [] },
    { id: "m/a", parent: "m", entities: [{ id: "T", kind: "type" }, ...methods, ...funcs] },
    ...Array.from({ length: 30 }, (_, i) => ({ id: "m/p" + i, parent: "m", entities: [{ id: "g" + i, kind: "function" }] })),
  ];
  assertClean(packages);
});

test("nothing overlaps in citydiff's own city", () => {
  assertClean(scene(["-path", "."]));
});

const barse = process.env.CITYDIFF_BARSE || "/workspace/barse-scene";
test("nothing overlaps in the barse city", { skip: !existsSync(join(barse, ".git")) && "no barse checkout" }, () => {
  assertClean(scene(["-path", barse, "-range", "dbdafc006b07ea29c2735fb00be0e46a6c4842c4..061e9aed03e2a2691cf9a596b193ab0d0f8f8f75"]));
});
