import { test } from "node:test";
import assert from "node:assert/strict";
import { clampCenter, freeCenter, moveCursor, pickSector, ringSector, sectorPath, snapSector } from "./ring.js";
import { KEYBINDS, RING, RING_TOUCH } from "./keys.js";

test("the ring holds keys, and only the ones nothing on screen stands in for", () => {
  const keys = KEYBINDS.flatMap((b) => b.keys);
  for (const item of RING) assert.ok(keys.includes(item.key), item.key);
  assert.equal(new Set(RING.map((item) => item.id)).size, RING.length);
  // These have a button, a tab or a gesture already.
  for (const key of ["/", "b", "t", "m", "1", "2", "c", "?", "space", ",", ".", "wasd", "q", "-"]) {
    assert.ok(!RING.some((item) => item.key === key), key);
  }
});

test("items go round clockwise from the top", () => {
  const n = 7;
  const first = ringSector(0, n);
  assert.ok(Math.abs(first.mid + Math.PI / 2) < 1e-9);
  assert.ok(Math.abs(first.to - first.from - (Math.PI * 2) / n) < 1e-9);
  // The next one is to the right of the top: x grows, y is still above.
  const second = ringSector(1, n);
  assert.ok(Math.cos(second.mid) > 0 && Math.sin(second.mid) < 0);
  // Each cell starts where the one before it ends.
  for (let i = 1; i < n; i++) assert.ok(Math.abs(ringSector(i, n).from - ringSector(i - 1, n).to) < 1e-9);
});

test("a slice is an arc out, a line in, and an arc back", () => {
  const { from, to } = ringSector(0, 4);
  const d = sectorPath(from, to, 100, 40);
  assert.match(d, /^M[-\d. ]+A100 100 0 0 1 [-\d. ]+L[-\d. ]+A40 40 0 0 0 [-\d. ]+Z$/);
});

test("a ring by the edge of the screen stays whole", () => {
  assert.deepEqual(clampCenter(500, 400, 240, 1000, 800), [500, 400]);
  assert.deepEqual(clampCenter(10, 10, 240, 1000, 800), [128, 128]);
  assert.deepEqual(clampCenter(995, 795, 240, 1000, 800), [872, 672]);
  // A screen too small for it centres it.
  assert.deepEqual(clampCenter(10, 10, 240, 200, 800), [100, 128]);
});

test("the free view is what the sidebars leave", () => {
  assert.deepEqual(freeCenter(1000, 400), [500, 200]);
  assert.deepEqual(freeCenter(1000, 400, 300, 0), [650, 200]);
  assert.deepEqual(freeCenter(1000, 400, 300, 320), [490, 200]);
});

test("a pointer points at the cell its direction falls in", () => {
  const n = 7;
  // Straight up is item 0, a little either side of it too.
  assert.equal(pickSector(0, -50, n, 20), 0);
  assert.equal(pickSector(10, -50, n, 20), 0);
  assert.equal(pickSector(-10, -50, n, 20), 0);
  // Straight right is between items 1 and 2 of 7, and nearer 2's middle.
  assert.equal(pickSector(50, 0, n, 20), 2);
  // Straight left is item 5, back.
  assert.equal(pickSector(-50, 0, n, 20), 5);
  // Every direction lands on each item's own middle.
  for (let i = 0; i < n; i++) {
    const { mid } = ringSector(i, n);
    assert.equal(pickSector(Math.cos(mid) * 60, Math.sin(mid) * 60, n, 20), i);
  }
});

test("the middle points at nothing, and any distance out points somewhere", () => {
  assert.equal(pickSector(5, 5, 7, 20), -1);
  assert.equal(pickSector(0, -21, 7, 20), 0);
  assert.equal(pickSector(0, -2000, 7, 20), 0);
});

test("a finger's ring has Cancel at the top and every action once", () => {
  assert.equal(RING_TOUCH[0], "cancel");
  assert.deepEqual([...RING_TOUCH.slice(1)].sort(), RING.map((item) => item.id).sort());
  // Back and forward still face each other across the ring.
  const at = (id) => RING_TOUCH.indexOf(id);
  assert.equal(Math.abs(at("back") - at("forward")), RING_TOUCH.length / 2);
});

test("the lit item holds a little past its edge, then snaps to the next", () => {
  const n = 8;
  const toward = (deg) => [Math.sin((deg * Math.PI) / 180) * 100, -Math.cos((deg * Math.PI) / 180) * 100];
  const hold = 10 / 360;
  // Item 0 spans -22.5 to 22.5 degrees. Lit, it holds to 32.5.
  assert.equal(snapSector(...toward(25), n, 20, 0, hold), 0);
  assert.equal(snapSector(...toward(32), n, 20, 0, hold), 0);
  assert.equal(snapSector(...toward(34), n, 20, 0, hold), 1);
  // Unlit, the plain edge decides.
  assert.equal(snapSector(...toward(25), n, 20, -1, hold), 1);
  // Item 1 lit, coming back: it holds until 12.5 degrees.
  assert.equal(snapSector(...toward(14), n, 20, 1, hold), 1);
  assert.equal(snapSector(...toward(11), n, 20, 1, hold), 0);
  // Across the top, where the turn wraps.
  assert.equal(snapSector(...toward(-30), n, 20, 0, hold), 0);
  assert.equal(snapSector(...toward(-34), n, 20, 0, hold), 7);
  // The dead middle is nothing, held or not.
  assert.equal(snapSector(1, 1, n, 20, 0, hold), -1);
});

test("a finger's cursor moves with the finger and stops at the rim", () => {
  let c = [0, 0];
  c = moveCursor(c, 30, -40, 100);
  assert.deepEqual(c, [30, -40]);
  // Back the same way puts it back.
  assert.deepEqual(moveCursor(c, -30, 40, 100), [0, 0]);
  // Past the rim it stays on it, at the angle it was moved to.
  c = moveCursor([0, 0], 0, -300, 100);
  assert.deepEqual(c, [0, -100]);
  // Along the rim from there, a move sideways slides it round.
  c = moveCursor(c, 100, 0, 100);
  assert.ok(Math.abs(Math.hypot(c[0], c[1]) - 100) < 1e-9);
  assert.ok(c[0] > 0 && c[1] < 0);
});

test("the same cursor position always lights the same item", () => {
  const n = 8;
  const dead = 29;
  const hold = 10 / 360;
  // A cursor walked up, round to the right, and back, lights cancel, diff,
  // forward, then diff and cancel again: no jump, no history.
  const walk = [[0, -60], [40, -40], [60, 0], [40, -40], [0, -60]];
  let lit = 0;
  const seen = walk.map(([x, y]) => (lit = Math.max(0, snapSector(x, y, n, dead, lit, hold))));
  assert.deepEqual(seen, [0, 1, 2, 1, 0]);
  // In the middle, nothing is pointed at (the ring lights Cancel there).
  assert.equal(snapSector(5, 5, n, dead, 3, hold), -1);
});
