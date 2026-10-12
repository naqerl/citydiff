import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { KEYBINDS, TAKEN_KEYS, tourAction } from "./keys.js";

const key = (k, extra = {}) => ({ key: k, target: { tagName: "CANVAS" }, ...extra });

test("tour keys", () => {
  assert.equal(tourAction(key(" ")), "toggle");
  assert.equal(tourAction(key(",")), "prev");
  assert.equal(tourAction(key(".")), "next");
  assert.equal(tourAction(key("t")), "sidebar");
  assert.equal(tourAction(key("T")), "sidebar");
});

test("arrows are the camera's again", () => {
  assert.equal(tourAction(key("ArrowLeft")), null);
  assert.equal(tourAction(key("ArrowRight")), null);
});

test("typing, modifiers and repeats are not tour actions", () => {
  assert.equal(tourAction(key("t", { target: { tagName: "INPUT" } })), null);
  assert.equal(tourAction(key(".", { ctrlKey: true })), null);
  assert.equal(tourAction(key(" ", { repeat: true })), null);
});

// The keys are documented in the skill, which is what ships with the binary and
// is installed for the reader. The README is a showcase and no longer lists
// them, so the check follows the documentation rather than the other way round.
test("every key is listed once, and the skill lists them all", () => {
  const all = KEYBINDS.flatMap((b) => b.keys);
  for (const k of ["E", "D", "b", "t", ",", ".", "space", "/", "?", "o", "i", "m", "0", "esc"]) assert.ok(all.includes(k), k);
  const skill = readFileSync(new URL("../skills/citydiff/SKILL.md", import.meta.url), "utf8");
  for (const b of KEYBINDS) {
    for (const k of b.keys) {
      if (k === "wasd" || k === "arrows" || k === "drag" || k === "scroll" || k === "click") continue;
      assert.ok(skill.includes("`" + k + "`"), "the skill lacks " + k);
    }
  }
});

// A plugin's key must not shadow one of the viewer's, so every key the legend
// lists is taken, spelt the way a key event spells it. The pointer gestures
// are not keys.
test("every key the legend lists is taken from plugins", () => {
  const spelt = { wasd: ["w", "a", "s", "d"], arrows: ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"], enter: ["Enter"], esc: ["Escape"], space: [" "] };
  const gestures = new Set(["drag", "scroll", "click", "right hold"]);
  for (const bind of KEYBINDS) {
    for (const k of bind.keys) {
      if (gestures.has(k)) continue;
      for (const event of spelt[k] || [k]) assert.ok(TAKEN_KEYS.has(event), `${k} (${bind.does}) is free for a plugin`);
    }
  }
});
