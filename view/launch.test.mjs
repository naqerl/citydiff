import { test } from "node:test";
import assert from "node:assert/strict";
import {
  isLaunch, countdown, boosterThrottle, shipThrottle, ascent, shipClock, follow, rumble, outOfSight,
  APPROACH_S, COUNT_FROM, LIFTOFF, IGNITE_S, STAGE_S, SEP_S, FOLLOW_S, RELEASE_S,
} from "./launch.js";

test("launch: only the full command runs it", () => {
  assert.ok(isLaunch("/launch"));
  assert.ok(isLaunch("  /Launch "));
  assert.ok(!isLaunch("/l"));
  assert.ok(!isLaunch("/launch now"));
  assert.ok(!isLaunch("launch"));
});

test("launch: the count runs 5 to 0, a second each, and the 0 is liftoff", () => {
  assert.equal(countdown(0), null);
  assert.equal(countdown(APPROACH_S - 0.01), null);
  const seen = [];
  for (let t = APPROACH_S; t < LIFTOFF + 1; t += 0.25) {
    const n = countdown(t);
    if (seen[seen.length - 1] !== n) seen.push(n);
  }
  assert.deepEqual(seen, [5, 4, 3, 2, 1, 0]);
  assert.equal(countdown(LIFTOFF), 0);
  assert.equal(countdown(LIFTOFF - 0.01), 1);
  assert.equal(countdown(LIFTOFF + 1), null);
  assert.equal(LIFTOFF - APPROACH_S, COUNT_FROM);
});

test("launch: the engines light in the count, are at full thrust at liftoff, and stage", () => {
  assert.equal(boosterThrottle(LIFTOFF - IGNITE_S - 0.01), 0);
  assert.ok(boosterThrottle(LIFTOFF - IGNITE_S / 2) > 0);
  assert.equal(boosterThrottle(LIFTOFF), 1);
  assert.equal(boosterThrottle(LIFTOFF + STAGE_S - 0.01), 1);
  assert.ok(boosterThrottle(LIFTOFF + STAGE_S + 0.1) < 0.3);
  assert.equal(boosterThrottle(LIFTOFF + STAGE_S + SEP_S), 0);
  assert.equal(shipThrottle(LIFTOFF + STAGE_S - 0.01), 0);
  assert.equal(shipThrottle(LIFTOFF + STAGE_S + 0.3), 1);
  // Hot staging: the ship lights before the booster lets go.
  assert.ok(shipThrottle(LIFTOFF + STAGE_S + SEP_S / 2) > 0 && boosterThrottle(LIFTOFF + STAGE_S + SEP_S / 2) > 0);
});

test("launch: a heavy climb, slow off the pad, then a slight arc", () => {
  assert.equal(ascent(-1).climb, 0);
  // It takes seconds to clear its own height.
  assert.ok(ascent(3).climb < 1 && ascent(4).climb > 1, String(ascent(3).climb));
  let last = ascent(0);
  for (let s = 0.25; s < STAGE_S + 4; s += 0.25) {
    const now = ascent(s);
    assert.ok(now.climb > last.climb, "climbs at " + s);
    assert.ok(now.climb - last.climb >= last.climb - ascent(s - 0.5).climb - 1e-9, "accelerates at " + s);
    assert.ok(now.downrange >= last.downrange && now.pitch >= last.pitch, "turns over at " + s);
    if (now.climb <= 1.5) assert.equal(now.downrange, 0, "straight past the tower at " + s);
    last = now;
  }
  // The nose follows the path: tan(pitch) is the slope of downrange over climb.
  const a = ascent(7);
  const b = ascent(7.001);
  const slope = (b.downrange - a.downrange) / (b.climb - a.climb);
  assert.ok(Math.abs(Math.tan(a.pitch) - slope) < 1e-3, Math.tan(a.pitch) + " vs " + slope);
  // Still well short of horizontal when it stages.
  assert.ok(ascent(STAGE_S).pitch < Math.PI / 4, String(ascent(STAGE_S).pitch));
});

test("launch: the ship keeps the stack's time until it is free, then runs ahead", () => {
  assert.equal(shipClock(STAGE_S), STAGE_S);
  assert.equal(shipClock(STAGE_S + SEP_S), STAGE_S + SEP_S);
  let lead = 0;
  for (let s = STAGE_S + SEP_S + 0.25; s < STAGE_S + 5; s += 0.25) {
    const now = shipClock(s) - s;
    assert.ok(now > lead, "pulls ahead at " + s);
    lead = now;
  }
});

test("launch: the camera eases onto the rocket after liftoff", () => {
  assert.equal(follow(0), 0);
  assert.equal(follow(FOLLOW_S[0]), 0);
  assert.equal(follow(FOLLOW_S[1]), 1);
  assert.equal(follow(RELEASE_S), 1);
  let last = 0;
  for (let s = FOLLOW_S[0]; s <= FOLLOW_S[1]; s += 0.1) {
    assert.ok(follow(s) >= last);
    last = follow(s);
  }
});

test("launch: the ground shakes from ignition and settles as the stack climbs away", () => {
  assert.equal(rumble(LIFTOFF - IGNITE_S - 0.1), 0);
  assert.ok(rumble(LIFTOFF) > 0.9);
  assert.ok(rumble(LIFTOFF + 5) < rumble(LIFTOFF + 2));
  assert.ok(rumble(LIFTOFF + 7) < 0.3);
  assert.equal(rumble(LIFTOFF + STAGE_S + SEP_S + 0.1), 0);
});

test("launch: a stage is out of sight outside the frustum or under a pixel", () => {
  assert.equal(outOfSight({ inView: true, pixels: 40 }), false);
  assert.equal(outOfSight({ inView: false, pixels: 40 }), true);
  assert.equal(outOfSight({ inView: true, pixels: 0.5 }), true);
});
