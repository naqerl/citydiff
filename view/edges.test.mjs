import { test } from "node:test";
import assert from "node:assert/strict";
import { packageCallEdges } from "./edges.js";

test("a changed call between packages with an unchanged import is an edge", () => {
  const edges = packageCallEdges([
    { from: "barse/service/flashcard/generator", to: "barse/db", change: "same" },
    { from: "barse/service/flashcard/generator", to: "barse/db", change: "added" },
    { from: "barse/service/flashcard/generator", to: "barse/db", change: "added" },
  ]);
  assert.deepEqual(edges, [{ from: "barse/service/flashcard/generator", to: "barse/db", change: "added" }]);
});

test("unchanged and same-package calls are not edges", () => {
  assert.deepEqual(packageCallEdges([
    { from: "a", to: "b", change: "same" },
    { from: "a", to: "a", change: "added" },
  ]), []);
});

test("added and removed calls between two packages read as changed", () => {
  assert.deepEqual(packageCallEdges([
    { from: "a", to: "b", change: "removed" },
    { from: "a", to: "b", change: "added" },
    { from: "b", to: "a", change: "modified" },
  ]), [
    { from: "a", to: "b", change: "modified" },
    { from: "b", to: "a", change: "modified" },
  ]);
});
