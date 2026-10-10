import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { KEYBINDS, tourAction } from "./keys.js";

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

test("every key is listed once, and the README lists them all", () => {
  const all = KEYBINDS.flatMap((b) => b.keys);
  for (const k of ["E", "b", "t", ",", ".", "space", "/", "?", "o", "i", "m", "0", "esc"]) assert.ok(all.includes(k), k);
  const readme = readFileSync(new URL("../README.md", import.meta.url), "utf8");
  for (const b of KEYBINDS) {
    for (const k of b.keys) {
      if (k === "wasd" || k === "arrows" || k === "drag" || k === "scroll" || k === "click") continue;
      assert.ok(readme.includes("`" + k + "`"), "README lacks " + k);
    }
  }
});
