import { test } from "node:test";
import assert from "node:assert/strict";
import { nameIndex, writeNode, readNode, readState, writeState } from "./state.js";

const items = [
  { pkg: "citydiff/lib/diff", entity: { id: "lib/diff/a_test.go#function#TestCoolStuff", kind: "function", name: "TestCoolStuff", file: "lib/diff/a_test.go" } },
  { pkg: "citydiff/lib/parser/go", entity: { id: "lib/parser/go/parser.go#function#New", kind: "function", name: "New", file: "lib/parser/go/parser.go" } },
  { pkg: "citydiff/lib/parser/rust", entity: { id: "lib/parser/rust/parser.go#function#New", kind: "function", name: "New", file: "lib/parser/rust/parser.go" } },
  { pkg: "citydiff/lib", entity: { id: "lib/parser.go#method#Parser.Parse", kind: "method", recv: "Parser", name: "Parse", file: "lib/parser.go" } },
  { pkg: "citydiff/lib", entity: { id: "lib/parser.go#type#Parser", kind: "type", name: "Parser", file: "lib/parser.go" } },
  { pkg: "citydiff/cmd", entity: { id: "cmd/a.go#function#init", kind: "function", name: "init", file: "cmd/a.go" } },
  { pkg: "citydiff/cmd", entity: { id: "cmd/b.go#function#init", kind: "function", name: "init", file: "cmd/b.go" } },
];
const index = nameIndex(items);
const sel = (id) => ({ kind: "entity", id });
const write = (i) => writeNode(sel(items[i].entity.id), items[i].entity, items[i].pkg, index);

test("a function always carries its package", () => {
  assert.equal(write(0), "function:citydiff/lib/diff.TestCoolStuff");
  assert.equal(write(1), "function:citydiff/lib/parser/go.New");
  assert.equal(write(2), "function:citydiff/lib/parser/rust.New");
});

test("a method carries its package and its type", () => {
  assert.equal(write(3), "method:citydiff/lib.Parser.Parse");
});

test("a type carries its package", () => {
  assert.equal(write(4), "type:citydiff/lib.Parser");
});

test("a name shared inside one package is told apart by its file", () => {
  assert.equal(write(5), "function:citydiff/cmd.init@cmd/a.go");
  assert.equal(write(6), "function:citydiff/cmd.init@cmd/b.go");
});

test("a package and an external are their ids", () => {
  assert.equal(writeNode({ kind: "package", id: "citydiff/lib" }, null, "", index), "package:citydiff/lib");
  assert.equal(writeNode({ kind: "external", id: "fmt" }, null, "", index), "external:fmt");
  assert.equal(writeNode(null, null, "", index), "");
});

test("readNode finds what writeNode wrote", () => {
  items.forEach((item, i) => assert.deepEqual(readNode(write(i), index), sel(item.entity.id), write(i)));
  assert.deepEqual(readNode("package:citydiff/lib", index), { kind: "package", id: "citydiff/lib" });
});

test("readNode picks the first of a shared name, and nothing for an unknown one", () => {
  assert.deepEqual(readNode("function:citydiff/cmd.init", index), sel(items[5].entity.id));
  assert.equal(readNode("function:New", index), null);
  assert.equal(readNode("function:citydiff/lib.Missing", index), null);
  assert.equal(readNode("TestCoolStuff", index), null);
  assert.equal(readNode("package:", index), null);
});

test("the defaults leave the address bare", () => {
  const state = { select: "", mode: "overview", refs: "calls", side: true, tourSide: true };
  assert.equal(writeState("", state), "");
  assert.deepEqual(readState(""), state);
});

test("the state is written readably and reads back", () => {
  const state = { select: "function:citydiff/lib/parser/rust.New", mode: "changes", refs: "callers", side: false, tourSide: false };
  const query = writeState("", state);
  assert.equal(query, "?select=function:citydiff/lib/parser/rust.New&mode=changes&refs=callers&side=closed&tourside=closed");
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
