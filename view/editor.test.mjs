import { test } from "node:test";
import assert from "node:assert/strict";
import { editAction } from "./keys.js";
import { editTarget, editQuery } from "./editor.js";

const key = (k, extra = {}) => ({ key: k, target: { tagName: "CANVAS" }, ...extra });

test("E opens the editor, e still orbits", () => {
  assert.equal(editAction(key("E")), true);
  assert.equal(editAction(key("e")), false);
});

test("E typed into a field, with a modifier or repeated is not the editor", () => {
  assert.equal(editAction(key("E", { target: { tagName: "INPUT" } })), false);
  assert.equal(editAction(key("E", { target: { tagName: "TEXTAREA" } })), false);
  assert.equal(editAction(key("E", { target: { isContentEditable: true } })), false);
  assert.equal(editAction(key("E", { ctrlKey: true })), false);
  assert.equal(editAction(key("E", { metaKey: true })), false);
  assert.equal(editAction(key("E", { repeat: true })), false);
});

const pkg = { id: "p", dir: "lib/db", entities: [{ id: "x", file: "lib/db/old.go", change: "removed" }, { id: "f", file: "lib/db/db.go", change: "modified", line: 12, col: 6 }] };
const byPackage = new Map([["p", pkg], ["ext", { id: "ext", external: true }]]);
const byEntity = new Map(pkg.entities.map((entity) => [entity.id, { entity, pkg }]));

test("a function opens at its line and column", () => {
  assert.deepEqual(editTarget({ kind: "entity", id: "f" }, byEntity, byPackage), { file: "lib/db/db.go", line: 12, col: 6 });
});

test("a removed entity opens its file without a position", () => {
  assert.deepEqual(editTarget({ kind: "entity", id: "x" }, byEntity, byPackage), { file: "lib/db/old.go" });
});

test("a package opens its directory, with its first live file as fallback", () => {
  assert.deepEqual(editTarget({ kind: "package", id: "p" }, byEntity, byPackage), { dir: "lib/db", file: "lib/db/db.go" });
});

test("nothing, or an external package, opens nothing", () => {
  assert.equal(editTarget(null, byEntity, byPackage), null);
  assert.equal(editTarget({ kind: "package", id: "ext" }, byEntity, byPackage), null);
  assert.equal(editTarget({ kind: "external", id: "ext" }, byEntity, byPackage), null);
});

test("the query carries the target and the terminal size", () => {
  assert.equal(editQuery({ file: "a.go", line: 3, col: 2 }, 100, 30), "file=a.go&line=3&col=2&cols=100&rows=30");
});

test("the city does not render under the editor, and restarts when it closes", async () => {
  const { readFileSync } = await import("node:fs");
  const main = readFileSync(new URL("./main.js", import.meta.url), "utf8");
  const animate = main.slice(main.indexOf("function animate(now) {"));
  const head = animate.slice(0, animate.indexOf("const t = now"));
  assert.match(head, /if \(editorActive\(\)\) \{[^}]*return;/, "animate returns before drawing while the editor is up");
  assert.doesNotMatch(head, /requestAnimationFrame|requestFrame\(/, "and schedules no next frame");
  const close = main.slice(main.indexOf("openEditor(target, () => {"), main.indexOf("openEditor(target, () => {") + 400);
  assert.match(close, /noteActivity\(\)[\s\S]*requestFrame\(\)/, "closing resets the idle clock, then restarts the guarded loop");
  const editor = readFileSync(new URL("./editor.js", import.meta.url), "utf8");
  assert.match(editor, /cursorBlink: false/);
});
