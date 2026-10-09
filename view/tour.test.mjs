import { test } from "node:test";
import assert from "node:assert/strict";
import { createPlayer } from "./tour.js";

function rig(n = 3) {
  const steps = Array.from({ length: n }, (_, i) => ({ title: "s" + i, duration: i + 1 }));
  const shown = [];
  const states = [];
  const timers = new Map();
  let seq = 0;
  const player = createPlayer(steps, {
    show: (i) => shown.push(i),
    change: (s) => states.push(s),
    setTimer: (fn, ms) => {
      timers.set(++seq, { fn, ms });
      return seq;
    },
    clearTimer: (id) => timers.delete(id),
  });
  const fire = () => {
    const [id, t] = [...timers.entries()].pop();
    timers.delete(id);
    t.fn();
    return t.ms;
  };
  return { player, shown, states, timers, fire };
}

test("start shows the first step and does not play", () => {
  const { player, shown, timers } = rig();
  player.start();
  assert.deepEqual(shown, [0]);
  assert.equal(player.state.playing, false);
  assert.equal(timers.size, 0);
});

test("next and prev stop at the ends", () => {
  const { player, shown } = rig();
  player.start();
  assert.equal(player.prev(), false);
  assert.equal(player.next(), true);
  assert.equal(player.next(), true);
  assert.equal(player.next(), false);
  assert.equal(player.state.index, 2);
  assert.equal(player.prev(), true);
  assert.deepEqual(shown, [0, 1, 2, 1]);
});

test("jump clamps and shows the step once", () => {
  const { player, shown } = rig(4);
  player.start();
  player.jump(2);
  player.jump(2);
  player.jump(99);
  player.jump(-5);
  assert.deepEqual(shown, [0, 2, 3, 0]);
});

test("play advances on each step's duration and ends on the last", () => {
  const { player, shown, timers, fire } = rig(3);
  player.start();
  player.play();
  assert.equal(fire(), 1000);
  assert.equal(fire(), 2000);
  assert.equal(player.state.index, 2);
  assert.equal(fire(), 3000);
  assert.equal(player.state.playing, false);
  assert.equal(player.state.ended, true);
  assert.equal(timers.size, 0);
  assert.deepEqual(shown, [0, 1, 2]);
});

test("play after the end starts over", () => {
  const { player, shown, fire } = rig(2);
  player.start();
  player.play();
  fire();
  fire();
  assert.equal(player.state.ended, true);
  player.play();
  assert.equal(player.state.index, 0);
  assert.equal(player.state.playing, true);
  assert.deepEqual(shown, [0, 1, 0]);
});

test("pause clears the timer and a manual step while playing restarts it", () => {
  const { player, timers } = rig(3);
  player.start();
  player.play();
  assert.equal(timers.size, 1);
  player.next();
  assert.equal(timers.size, 1);
  assert.equal([...timers.values()][0].ms, 2000);
  player.pause();
  assert.equal(timers.size, 0);
  player.toggle();
  assert.equal(player.state.playing, true);
  assert.equal(timers.size, 1);
});

test("an empty tour does nothing", () => {
  const { player, shown } = rig(0);
  assert.equal(player.start(), false);
  player.play();
  assert.deepEqual(shown, []);
});

import { reloadFor, rangeLabel } from "./tour.js";

test("a tour that switched the range reloads without ?tour", () => {
  const reply = { scene: { switched: true, range: "a..b" } };
  assert.equal(reloadFor(reply, "http://h/?tour=x.json&mode=changes"), "http://h/?mode=changes");
  assert.equal(reloadFor(reply, "http://h/"), "http://h/");
});

test("a tour on the current range does not reload", () => {
  assert.equal(reloadFor({ scene: { switched: false } }, "http://h/?tour=x"), null);
  assert.equal(reloadFor({}, "http://h/"), null);
  assert.equal(reloadFor(null, "http://h/"), null);
});

test("the range line", () => {
  assert.equal(rangeLabel("a..b", "a..b"), "a..b");
  assert.equal(rangeLabel("a..b", "aaaa..bbbb"), "a..b (scene aaaa..bbbb)");
  assert.equal(rangeLabel("", "x..y"), "x..y");
  assert.equal(rangeLabel("", ""), "");
});
