import { test } from "node:test";
import assert from "node:assert/strict";
import { nameIndex, writeNode, readNode, readState, writeState } from "./state.js";

const entities = [
  { id: "a_test.go#function#TestCoolStuff", kind: "function", name: "TestCoolStuff", file: "a_test.go" },
  { id: "lib/go/parser.go#function#New", kind: "function", name: "New", file: "lib/go/parser.go" },
  { id: "lib/rust/parser.go#function#New", kind: "function", name: "New", file: "lib/rust/parser.go" },
  { id: "lib/parser.go#method#Parser.Parse", kind: "method", recv: "Parser", name: "Parse", file: "lib/parser.go" },
  { id: "lib/parser.go#type#Parser", kind: "type", name: "Parser", file: "lib/parser.go" },
];
const index = nameIndex(entities);
const entity = (id) => entities.find((item) => item.id === id);
const sel = (id) => ({ kind: "entity", id });

test("a declaration with a unique name is kind:name", () => {
  assert.equal(writeNode(sel(entities[0].id), entities[0], index), "function:TestCoolStuff");
  assert.equal(writeNode(sel(entities[4].id), entities[4], index), "type:Parser");
});

test("a method carries its receiver", () => {
  assert.equal(writeNode(sel(entities[3].id), entities[3], index), "method:Parser.Parse");
});

test("a shared name is told apart by its file", () => {
  assert.equal(writeNode(sel(entities[2].id), entities[2], index), "function:New@lib/rust/parser.go");
});

test("a package and an external are their ids", () => {
  assert.equal(writeNode({ kind: "package", id: "citydiff/lib" }, null, index), "package:citydiff/lib");
  assert.equal(writeNode({ kind: "external", id: "fmt" }, null, index), "external:fmt");
  assert.equal(writeNode(null, null, index), "");
});

test("readNode finds what writeNode wrote", () => {
  for (const item of entities) {
    const text = writeNode(sel(item.id), entity(item.id), index);
    assert.deepEqual(readNode(text, index), sel(item.id), text);
  }
  assert.deepEqual(readNode("package:citydiff/lib", index), { kind: "package", id: "citydiff/lib" });
});

test("readNode picks the first of a shared name, and nothing for an unknown one", () => {
  assert.deepEqual(readNode("function:New", index), sel(entities[1].id));
  assert.equal(readNode("function:Missing", index), null);
  assert.equal(readNode("TestCoolStuff", index), null);
  assert.equal(readNode("package:", index), null);
});

test("the defaults leave the address bare", () => {
  const state = { select: "", mode: "overview", refs: "calls", side: true, tourSide: true };
  assert.equal(writeState("", state), "");
  assert.deepEqual(readState(""), state);
});

test("the state is written readably and reads back", () => {
  const state = { select: "function:New@lib/rust/parser.go", mode: "changes", refs: "callers", side: false, tourSide: false };
  const query = writeState("", state);
  assert.equal(query, "?select=function:New@lib/rust/parser.go&mode=changes&refs=callers&side=closed&tourside=closed");
  assert.deepEqual(readState(query), state);
});

test("other parameters are kept, and stale state is replaced", () => {
  const state = { select: "type:Parser", mode: "overview", refs: "calls", side: true, tourSide: true };
  assert.equal(writeState("?tour=./t.json&mode=changes&select=x:y", state), "?tour=./t.json&select=type:Parser");
});

test("a name with characters a query cannot hold is escaped", () => {
  const query = writeState("", { select: "function:a&b c", mode: "overview", refs: "calls", side: true, tourSide: true });
  assert.equal(query, "?select=function:a%26b%20c");
  assert.equal(readState(query).select, "function:a&b c");
});
