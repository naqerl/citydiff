import { test } from "node:test";
import assert from "node:assert/strict";
import { AR_SIZE_M, arScale, facing, arRig, roomToCity, level } from "./ar.js";

const near = (actual, expected, eps, what) => assert.ok(Math.abs(actual - expected) < eps, (what || "") + " " + actual + " ≠ " + expected);
const nearPoint = (a, b, what) => ["x", "y", "z"].forEach((k) => near(a[k], b[k], 1e-9, what + "." + k));

test("ar: the circle stands 60 cm wide on the table", () => {
  assert.equal(AR_SIZE_M, 0.6);
  near(arScale(300), 1000, 1e-9, "a 600-unit circle");
  // One metre of the room spans the circle's width over 0.6 m.
  const rig = arRig({ x: 0, y: 0, z: 0 }, { x: 0, y: 0, z: 0 }, arScale(300), 0);
  const a = roomToCity(rig, { x: 0, y: 0, z: 0 });
  const b = roomToCity(rig, { x: AR_SIZE_M, y: 0, z: 0 });
  near(Math.hypot(b.x - a.x, b.z - a.z), 600, 1e-9, "the circle's width");
});

test("ar: the spot tapped is the middle of the city's circle, however the city is turned", () => {
  const spot = { x: 0.4, y: -0.75, z: -1.1 };
  const center = { x: 12, y: 0, z: -30 };
  for (const yaw of [0, 0.7, Math.PI / 2, -2.5, Math.PI]) {
    const rig = arRig(spot, center, 800, yaw);
    nearPoint(roomToCity(rig, spot), center, "yaw " + yaw);
  }
});

test("ar: the room is scaled the same way up as across", () => {
  const spot = { x: 0, y: -0.7, z: -1 };
  const rig = arRig(spot, { x: 0, y: 0, z: 0 }, 500, 1.1);
  const above = roomToCity(rig, { x: 0, y: -0.6, z: -1 });
  near(above.y, 50, 1e-9, "10 cm above the table");
  near(above.x, 0, 1e-9, "straight above, x");
  near(above.z, 0, 1e-9, "straight above, z");
});

test("ar: the side the camera saw before AR faces the person", () => {
  const spot = { x: 0.3, y: -0.7, z: -1.2 };
  for (const viewer of [{ x: 0, y: 0, z: 0 }, { x: 1.5, y: 0.2, z: -1 }, { x: -0.4, y: 0, z: -2.5 }]) {
    for (const front of [{ x: 0, z: 1 }, { x: 0.6, z: 0.8 }, { x: -1, z: -0.2 }]) {
      const yaw = facing(spot, viewer, front);
      const rig = arRig(spot, { x: 0, y: 0, z: 0 }, 400, yaw);
      const seen = roomToCity(rig, viewer);
      // From the city's centre, the person stands the way front points.
      const along = Math.atan2(seen.x, seen.z) - Math.atan2(front.x, front.z);
      near(Math.atan2(Math.sin(along), Math.cos(along)), 0, 1e-9, JSON.stringify({ viewer, front }));
    }
  }
});

test("ar: the rig turns the way three's Object3D does, counter-clockwise from above", () => {
  // Three's rotation about y takes +x to -z: a quarter turn left.
  const rig = arRig({ x: 0, y: 0, z: 0 }, { x: 0, y: 0, z: 0 }, 1, Math.PI / 2);
  nearPoint(roomToCity(rig, { x: 1, y: 0, z: 0 }), { x: 0, y: 0, z: -1 }, "+x");
  nearPoint(roomToCity(rig, { x: 0, y: 0, z: 1 }), { x: 1, y: 0, z: 0 }, "+z");
});

test("ar: a table or the floor takes the city, a wall does not", () => {
  const about = (axis, angle) => {
    const s = Math.sin(angle / 2);
    return { x: axis[0] * s, y: axis[1] * s, z: axis[2] * s, w: Math.cos(angle / 2) };
  };
  assert.equal(level({ x: 0, y: 0, z: 0, w: 1 }), true, "flat");
  assert.equal(level(about([0, 1, 0], 2)), true, "flat, turned");
  assert.equal(level(about([1, 0, 0], (20 * Math.PI) / 180)), true, "a slight slope");
  assert.equal(level(about([0, 0, 1], (40 * Math.PI) / 180)), false, "too steep");
  assert.equal(level(about([1, 0, 0], Math.PI / 2)), false, "a wall");
  assert.equal(level(about([1, 0, 0], Math.PI)), false, "a ceiling");
});
