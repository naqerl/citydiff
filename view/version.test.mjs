import { test } from "node:test";
import assert from "node:assert/strict";
import { versionLabel } from "./version.js";

test("version: a release is its version", () => {
  assert.deepEqual(versionLabel({ version: "v0.0.27" }), { text: "v0.0.27", update: null });
});

test("version: a development build is its commit, and says when the tree was modified", () => {
  assert.deepEqual(versionLabel({ version: "dev", commit: "68537c2" }), { text: "dev 68537c2", update: null });
  assert.equal(versionLabel({ version: "dev", commit: "68537c2", dirty: true }).text, "dev 68537c2 (modified)");
  assert.equal(versionLabel({ version: "dev" }).text, "dev");
});

test("version: a newer release is a link to it", () => {
  const url = "https://github.com/naqerl/citydiff/releases/tag/v0.0.28";
  assert.deepEqual(
    versionLabel({ version: "v0.0.27", update: { version: "v0.0.28", url } }),
    { text: "v0.0.27", update: { text: "v0.0.28 available", url } },
  );
  assert.equal(versionLabel({ version: "v0.0.27", update: { version: "v0.0.28" } }).update, null);
});

test("version: nothing to say is no label", () => {
  assert.equal(versionLabel(null), null);
  assert.equal(versionLabel({}), null);
});

test("version: the legend asks for the version each time it opens", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("./main.js", import.meta.url), "utf8");
  const set = src.slice(src.indexOf("function setLegend("));
  assert.match(set.slice(0, set.indexOf("\n}\n")), /showVersion\(/);
});
