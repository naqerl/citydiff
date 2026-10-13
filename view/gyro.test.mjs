import { test } from "node:test";
import assert from "node:assert/strict";
import { attitude, turnBetween, orbitStep, createShake, SHAKE_REST_MS } from "./gyro.js";

const DEG = Math.PI / 180;
const near = (actual, expected, eps, what) => assert.ok(Math.abs(actual - expected) < eps, (what || "") + " " + actual + " ≠ " + expected);

// rotate turns a vector by a quaternion [x, y, z, w].
function rotate(q, v) {
  const [x, y, z, w] = q;
  const tx = 2 * (y * v[2] - z * v[1]);
  const ty = 2 * (z * v[0] - x * v[2]);
  const tz = 2 * (x * v[1] - y * v[0]);
  return [
    v[0] + w * tx + (y * tz - z * ty),
    v[1] + w * ty + (z * tx - x * tz),
    v[2] + w * tz + (x * ty - y * tx),
  ];
}

const nearVec = (actual, expected, what) => actual.forEach((c, i) => near(c, expected[i], 1e-9, what + "[" + i + "]"));

test("gyro: the attitude is alpha about z, then beta about x, then gamma about y", () => {
  nearVec(attitude(0, 0, 0), [0, 0, 0, 1], "flat");
  // Flat on a table, turned 90° left: the top points west.
  nearVec(rotate(attitude(90, 0, 0), [0, 1, 0]), [-1, 0, 0], "alpha");
  // Upright, facing the person: the back looks north and the top is up.
  nearVec(rotate(attitude(0, 90, 0), [0, 0, -1]), [0, 1, 0], "beta back");
  nearVec(rotate(attitude(0, 90, 0), [0, 1, 0]), [0, 0, 1], "beta top");
  // Rolled 90° on its long edge: the right edge points down and the screen
  // faces east.
  nearVec(rotate(attitude(0, 0, 90), [1, 0, 0]), [0, 0, -1], "gamma edge");
  nearVec(rotate(attitude(0, 0, 90), [0, 0, 1]), [1, 0, 0], "gamma screen");
  // The order: the tilt is applied in the frame the turn left behind.
  nearVec(rotate(attitude(90, 90, 0), [0, 0, -1]), [-1, 0, 0], "alpha then beta");
  // A missing alpha is a phone that reports only its tilt.
  nearVec(attitude(null, 30, 10), attitude(0, 30, 10), "null alpha");
});

test("gyro: turning left about the vertical is a positive yaw however the phone is held", () => {
  for (const beta of [0, 30, 60, 89.9, 90, 120]) {
    for (const gamma of [0, 20, -35]) {
      const turn = turnBetween(attitude(10, beta, gamma), attitude(40, beta, gamma));
      near(turn.yaw, 30 * DEG, 1e-9, "yaw at beta " + beta + " gamma " + gamma);
      if (gamma === 0) near(turn.pitch, 0, 1e-9, "pitch at beta " + beta);
    }
  }
  near(turnBetween(attitude(40, 60, 0), attitude(10, 60, 0)).yaw, -30 * DEG, 1e-9, "right");
  // Across north: 350° to 10° is 20° to the left, not 340° to the right.
  near(turnBetween(attitude(350, 60, 0), attitude(10, 60, 0)).yaw, 20 * DEG, 1e-9, "wrap");
});

test("gyro: the top coming toward the person is a positive pitch, and it is no yaw", () => {
  const toward = turnBetween(attitude(0, 60, 0), attitude(0, 80, 0));
  near(toward.pitch, 20 * DEG, 1e-9, "toward");
  near(toward.yaw, 0, 1e-9, "no yaw");
  near(turnBetween(attitude(25, 80, 0), attitude(25, 60, 0)).pitch, -20 * DEG, 1e-9, "away");
});

test("gyro: sideways, the screen's own top is what tips", () => {
  // Turned a quarter counter-clockwise (screen angle 90) the screen's left to
  // right line is the device's -y: a roll about it is the pitch, and a turn
  // about the device's x no longer is.
  const flat = attitude(0, 0, 0);
  near(turnBetween(flat, attitude(0, 0, -20), 90).pitch, 20 * DEG, 1e-9, "landscape");
  near(turnBetween(flat, attitude(0, 20, 0), 90).pitch, 0, 1e-9, "landscape x");
  near(turnBetween(flat, attitude(0, 0, 20), 270).pitch, 20 * DEG, 1e-9, "landscape the other way");
  near(turnBetween(flat, attitude(0, 0, 20), -90).pitch, 20 * DEG, 1e-9, "window.orientation spells 270 as -90");
});

test("gyro: the orbit turns about the look point the way fly.js does, at the same distance", () => {
  const target = { x: 0, y: 0, z: 0 };
  const left = orbitStep({ x: 0, y: 5, z: 10 }, target, Math.PI / 2, 0);
  near(left.x, 10, 1e-9, "x");
  near(left.y, 5, 1e-9, "y");
  near(left.z, 0, 1e-9, "z");
  const off = { x: 3, y: 1, z: -2 };
  const moved = orbitStep({ x: 3, y: 9, z: 8 }, off, 0.7, 0.2, { minPolar: 0, maxPolar: Math.PI / 2 });
  near(Math.hypot(moved.x - off.x, moved.y - off.y, moved.z - off.z), Math.hypot(0, 8, 10), 1e-9, "distance");
});

test("gyro: pitch moves the camera toward the horizon, inside the controls' limits", () => {
  const target = { x: 0, y: 0, z: 0 };
  const start = { x: 0, y: 10, z: 10 };
  assert.ok(orbitStep(start, target, 0, 0.1).y < start.y, "positive pitch lowers it");
  assert.ok(orbitStep(start, target, 0, -0.1).y > start.y, "negative pitch raises it");
  const max = Math.PI / 2 - 0.05;
  const low = orbitStep(start, target, 0, 3, { minPolar: 0, maxPolar: max });
  near(Math.acos(low.y / Math.hypot(low.x, low.y, low.z)), max, 1e-9, "held at the horizon");
  const high = orbitStep(start, target, 0, -3, { minPolar: 0.2, maxPolar: max });
  near(Math.acos(high.y / Math.hypot(high.x, high.y, high.z)), 0.2, 1e-9, "held at the top");
  // A camera already below the limit is not pulled up by a turn.
  const under = { x: 0, y: -1, z: 10 };
  const turned = orbitStep(under, target, 0.3, 0.1, { minPolar: 0, maxPolar: max });
  near(turned.y, under.y, 1e-9, "stays where it was");
});

// jolts are samples along x, each the opposite way to the last, gap ms apart.
function jolts(shake, count, { from = 0, gap = 100, size = 20 } = {}) {
  const hits = [];
  for (let i = 0; i < count; i++) hits.push(shake.push(i % 2 ? -size : size, 0, 0, from + i * gap));
  return hits;
}

test("gyro: a shake is four jolts back and forth inside a second", () => {
  assert.deepEqual(jolts(createShake(), 4), [false, false, false, true]);
});

test("gyro: a bump, a single swing, or a gentle sway is not a shake", () => {
  assert.deepEqual(jolts(createShake(), 3), [false, false, false], "three jolts");
  assert.deepEqual(jolts(createShake(), 6, { size: 8 }), Array(6).fill(false), "too soft");
  assert.deepEqual(jolts(createShake(), 6, { gap: 400 }), Array(6).fill(false), "too slow");
  // A jolt that keeps pushing the same way is still one jolt.
  const shake = createShake();
  const same = [0, 16, 33, 50, 66, 83].map((t) => shake.push(20, 0, 0, t));
  assert.deepEqual(same, Array(6).fill(false));
});

test("gyro: one shake resets once, and the next shake after a rest counts again", () => {
  const shake = createShake();
  const first = jolts(shake, 8);
  assert.equal(first.filter(Boolean).length, 1, "a long shake is one reset");
  const again = jolts(shake, 4, { from: 700 + SHAKE_REST_MS + 1 });
  assert.equal(again[3], true);
});

test("gyro: a phone with only the gravity-laden acceleration still shakes", () => {
  const shake = createShake();
  let t = 0;
  // At rest: only gravity, never a shake.
  for (let i = 0; i < 60; i++) assert.equal(shake.pushWithGravity(0, 0, 9.81, (t += 16)), false);
  let shook = false;
  for (let i = 0; i < 30; i++) {
    const x = i % 6 < 3 ? 25 : -25;
    shook = shake.pushWithGravity(x, 0, 9.81, (t += 16)) || shook;
  }
  assert.ok(shook);
});
