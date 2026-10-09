import { test } from "node:test";
import assert from "node:assert/strict";
import { deletedFirst } from "./changes.js";

test("deleted changes come first, the rest keep their order", () => {
  const list = [
    { id: "a", change: "added" },
    { id: "b", change: "removed" },
    { id: "c", change: "modified" },
    { id: "d", change: "removed" },
    { id: "e", change: "added" },
  ];
  assert.deepEqual(deletedFirst(list).map((x) => x.id), ["b", "d", "a", "c", "e"]);
});

test("each group is ordered on its own", () => {
  const types = deletedFirst([{ id: "T1", change: "modified" }, { id: "T2", change: "removed" }]);
  const methods = deletedFirst([{ id: "m1", change: "added" }, { id: "m2", change: "modified" }]);
  assert.deepEqual(types.map((x) => x.id), ["T2", "T1"]);
  assert.deepEqual(methods.map((x) => x.id), ["m1", "m2"]);
});

test("the main view sorts every changes section with it", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("./main.js", import.meta.url), "utf8");
  assert.match(src, /hot: deletedFirst\(split\.hot\)/);
  assert.match(src, /deletedFirst\(packageList\)/);
});
