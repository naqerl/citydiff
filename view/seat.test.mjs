import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { layoutCity, drawnBox, oldBodyBox } from "./layout.js";

const repo = join(dirname(fileURLToPath(import.meta.url)), "..");
const EPS = 1e-9;

// Every draw state a building can be in: changes or full mode, with no
// selection, inside a selection (open) and outside it (shut), and growing.
const STATES = [];
for (const overlay of [false, true]) {
  STATES.push({ overlay, lit: false, open: 1 });
  for (const open of [0, 0.5, 1]) STATES.push({ overlay, lit: true, open });
}

// floating lists every building, and old-body ghost, whose base is not on
// the top of what is under it: its type as drawn, or its package.
function floating(packages) {
  const laid = layoutCity(packages);
  const entities = new Map();
  for (const pkg of packages) for (const e of pkg.entities || []) entities.set(e.id, e);
  const out = [];
  for (const pkg of laid.packages) {
    const slots = new Map(pkg.entities.map((s) => [s.id, { ...s, entity: entities.get(s.id) }]));
    for (const slot of slots.values()) {
      const parent = slot.entity.kind === "method" && slot.entity.parent ? slots.get(slot.entity.parent) : null;
      for (const state of STATES) {
        for (const parentState of parent ? STATES.filter((p) => p.overlay === state.overlay) : [null]) {
          const box = drawnBox(slot, state, parent, parentState);
          const under = parent ? drawnBox(parent, parentState, null) : { y: pkg.y, h: pkg.h };
          const seat = under.y + under.h;
          const tag = `${slot.entity.change} ${slot.entity.kind} ${slot.id} ${JSON.stringify(state)}`;
          if (Math.abs(box.y - seat) > EPS) out.push(tag + ` base ${box.y} on ${seat}`);
          const ghost = oldBodyBox(box, slot.entity, state.overlay);
          if (ghost && Math.abs(ghost.y - box.y) > EPS) out.push(tag + " ghost " + ghost.y);
        }
      }
    }
  }
  return out;
}

function scene(args) {
  return JSON.parse(execFileSync("go", ["run", "./cmd/cli", ...args, "-scene"], { cwd: repo, maxBuffer: 1 << 28 })).packages;
}

test("every change kind stands on its roof in every state", () => {
  const kinds = ["added", "modified", "removed", "same"];
  const entities = [];
  for (const change of kinds) {
    entities.push({ id: "T" + change, kind: "type", change });
    for (const m of kinds) entities.push({ id: `m${change}${m}`, kind: "method", parent: "T" + change, change: m, bodyBytes: 300, bodyBytesBefore: m === "added" ? null : 900 });
    entities.push({ id: "f" + change, kind: "function", change, bodyBytes: 40, bodyBytesBefore: change === "added" ? null : 4000 });
  }
  const packages = [{ id: "m", entities: [] }, { id: "m/a", parent: "m", entities }];
  assert.deepEqual(floating(packages), []);
});

test("the old body is a ghost on the building's own base", () => {
  const box = { x: 1, y: 7.5, z: 2, w: 1, h: 3, d: 1 };
  const ghost = oldBodyBox(box, { change: "removed", bodyBytesBefore: 5000 }, true);
  assert.equal(ghost.y, 7.5);
  assert.ok(ghost.x < box.x && ghost.x + ghost.w > box.x + box.w);
  assert.equal(oldBodyBox(box, { change: "modified", bodyBytesBefore: 5 }, false), null);
  assert.equal(oldBodyBox(box, { change: "added", bodyBytes: 5 }, true), null);
});

const barse = process.env.CITYDIFF_BARSE || "/workspace/barse-scene";
test("nothing floats in the barse city", { skip: !existsSync(join(barse, ".git")) && "no barse checkout" }, () => {
  assert.deepEqual(floating(scene(["-path", barse, "-range", "dbdafc006b07ea29c2735fb00be0e46a6c4842c4..061e9aed03e2a2691cf9a596b193ab0d0f8f8f75"])).slice(0, 10), []);
});

test("nothing floats in citydiff's own city", () => {
  assert.deepEqual(floating(scene(["-path", "."])).slice(0, 10), []);
});

// A range of citydiff's own history that deletes a declaration.
const ownRange = "46b5ba0~12..46b5ba0";
let hasRange = true;
try {
  execFileSync("git", ["rev-parse", "--verify", "46b5ba0~12^{commit}"], { cwd: repo, stdio: "ignore" });
} catch {
  hasRange = false;
}
test("nothing floats in a citydiff range with a deletion", { skip: !hasRange && "shallow history" }, () => {
  const packages = scene(["-path", ".", "-range", ownRange]);
  assert.ok(packages.some((p) => (p.entities || []).some((e) => e.change === "removed")));
  assert.deepEqual(floating(packages).slice(0, 10), []);
});
