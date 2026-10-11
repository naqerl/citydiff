import { test } from "node:test";
import assert from "node:assert/strict";
import { insets, viewOffsetX, viewOffsetY, freeFov, fitPose, boxOf } from "./viewport.js";

const DIR = [0.32, 0.48, 0.78];
const VIEW = { fov: 50, width: 1600, height: 900 };

// project mirrors a three.js perspective camera looking at target.
function project(p, pos, target, fov, aspect) {
  const f = [target[0] - pos[0], target[1] - pos[1], target[2] - pos[2]];
  const fl = Math.hypot(...f);
  const fw = f.map((v) => v / fl);
  let r = [-fw[2], 0, fw[0]];
  const rl = Math.hypot(...r);
  r = r.map((v) => v / rl);
  const u = [r[1] * fw[2] - r[2] * fw[1], r[2] * fw[0] - r[0] * fw[2], r[0] * fw[1] - r[1] * fw[0]];
  const d = [p[0] - pos[0], p[1] - pos[1], p[2] - pos[2]];
  const z = d[0] * fw[0] + d[1] * fw[1] + d[2] * fw[2];
  const t = Math.tan((fov * Math.PI) / 360);
  return {
    x: (d[0] * r[0] + d[1] * r[1] + d[2] * r[2]) / z / (t * aspect),
    y: (d[0] * u[0] + d[1] * u[1] + d[2] * u[2]) / z / t,
  };
}

function corners(box) {
  const out = [];
  for (const x of [box.min[0], box.max[0]]) for (const y of [box.min[1], box.max[1]]) for (const z of [box.min[2], box.max[2]]) out.push([x, y, z]);
  return out;
}

// extent is how much of the free area the box fills, horizontally and vertically.
function extent(box, opts) {
  const pose = fitPose(box, DIR, opts);
  const full = opts.width / opts.height;
  const free = (opts.width - (opts.left || 0) - (opts.right || 0)) / opts.width;
  let x = 0;
  let y = 0;
  for (const c of corners(box)) {
    const p = project(c, pose.pos, pose.target, opts.fov, full);
    x = Math.max(x, Math.abs(p.x) / free);
    y = Math.max(y, Math.abs(p.y));
  }
  return { x, y, pose };
}

test("insets keep a free area or give up a sidebar", () => {
  assert.deepEqual(insets(1600, 340, 380), { left: 340, right: 380, free: 880 });
  assert.deepEqual(insets(700, 340, 380), { left: 340, right: 0, free: 360 });
  assert.deepEqual(insets(300, 340, 380), { left: 0, right: 0, free: 300 });
});

test("the view offset centres the free area", () => {
  assert.equal(viewOffsetX(340, 0), -170);
  assert.equal(viewOffsetX(340, 380), 20);
  assert.equal(viewOffsetX(0, 0), 0);
});

test("a tall tower fits top to bottom and touches the fill vertically", () => {
  const tower = { min: [0, 0, 0], max: [1, 60, 1] };
  const e = extent(tower, { ...VIEW, left: 340, right: 380 });
  assert.ok(e.y <= 0.861 && e.x <= 0.861, JSON.stringify(e));
  assert.ok(e.y > 0.84, "vertical is the tight side: " + e.y);
});

test("a wide package fits the narrowed free width", () => {
  const wide = { min: [0, 0, 0], max: [200, 4, 40] };
  const both = extent(wide, { ...VIEW, left: 340, right: 380 });
  const none = extent(wide, VIEW);
  assert.ok(both.x <= 0.861 && both.x > 0.84, JSON.stringify(both));
  assert.ok(both.pose.dist > none.pose.dist * 1.5, "sidebars push the camera back");
});

test("a short object is held at the minimum distance", () => {
  const stub = { min: [0, 0, 0], max: [1, 1, 1] };
  assert.equal(fitPose(stub, DIR, { ...VIEW, minDist: 14 }).dist, 14);
});

test("the target is the box centre and the camera sits along dir", () => {
  const box = boxOf([[0, 0, 0], [10, 20, 4], [-2, 5, 8]]);
  assert.deepEqual(box, { min: [-2, 0, 0], max: [10, 20, 8] });
  const pose = fitPose(box, DIR, VIEW);
  assert.deepEqual(pose.target, [4, 10, 4]);
  const v = pose.pos.map((p, i) => p - pose.target[i]);
  const l = Math.hypot(...v);
  const dl = Math.hypot(...DIR);
  v.forEach((c, i) => assert.ok(Math.abs(c / l - DIR[i] / dl) < 1e-9));
});

test("a bottom sheet lifts the look-at point to the middle of the area above it", () => {
  assert.equal(viewOffsetY(0), 0);
  assert.equal(viewOffsetY(400), 200);
  assert.equal(viewOffsetY(-5), 0);
});

test("the free area above a sheet has a narrower vertical field of view", () => {
  assert.equal(freeFov(42, 800, 0), 42);
  assert.equal(freeFov(42, 800, 900), 42);
  const fov = freeFov(42, 800, 400);
  // Half the height: tan of the half angle halves.
  assert.ok(Math.abs(Math.tan((fov * Math.PI) / 360) - Math.tan((42 * Math.PI) / 360) / 2) < 1e-9);
});

test("minAspect draws a box in a tall view as if the view were that wide", () => {
  const box = { min: [-20, 0, -20], max: [20, 10, 20] };
  const tall = { fov: 42, width: 390, height: 800 };
  const plain = fitPose(box, DIR, tall);
  const wide = fitPose(box, DIR, { ...tall, minAspect: 1.25 });
  assert.equal(wide.aspect, 1.25);
  assert.ok(wide.dist < plain.dist);
  // A view already wider than minAspect is left alone.
  assert.equal(fitPose(box, DIR, { ...VIEW, minAspect: 1.25 }).dist, fitPose(box, DIR, VIEW).dist);
});
