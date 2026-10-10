import { test } from "node:test";
import assert from "node:assert/strict";
import { nameIndex, writeNode, readNode, readState, writeState } from "./state.js";

const ent = (pkg, id, kind, name, file, recv) => ({ kind: "entity", pkg, entity: { id, kind, name, file, recv } });
const nodes = [
  { kind: "package", id: "citydiff" },
  { kind: "package", id: "citydiff/lib" },
  { kind: "package", id: "citydiff/lib/diff" },
  { kind: "external", id: "github.com/x/y" },
  ent("citydiff/lib/diff", "lib/diff/a_test.go#function#TestCoolStuff", "function", "TestCoolStuff", "lib/diff/a_test.go"),
  ent("citydiff/lib/parser/go", "lib/parser/go/parser.go#function#New", "function", "New", "lib/parser/go/parser.go"),
  ent("citydiff/lib/parser/rust", "lib/parser/rust/parser.go#function#New", "function", "New", "lib/parser/rust/parser.go"),
  ent("citydiff/lib", "lib/parser.go#type#Parser", "type", "Parser", "lib/parser.go"),
  ent("citydiff/lib", "lib/parser.go#method#Parser.Parse", "method", "Parse", "lib/parser.go", "Parser"),
  ent("citydiff/cmd", "cmd/a.go#function#init", "function", "init", "cmd/a.go"),
  ent("citydiff/cmd", "cmd/b.go#function#init", "function", "init", "cmd/b.go"),
  ent("citydiff", "main.go#function#lib", "function", "lib", "main.go"),
];
const index = nameIndex(nodes);
const back = (node) => (node.kind === "entity" ? { kind: "entity", id: node.entity.id } : { kind: node.kind, id: node.id });

test("a package is its path, top to bottom", () => {
  assert.equal(writeNode(nodes[0], index), "citydiff");
  assert.equal(writeNode(nodes[2], index), "citydiff:lib:diff");
  assert.equal(writeNode(nodes[3], index), "github.com:x:y");
});

test("a function is its package's path, then its name", () => {
  assert.equal(writeNode(nodes[4], index), "citydiff:lib:diff:TestCoolStuff");
  assert.equal(writeNode(nodes[5], index), "citydiff:lib:parser:go:New");
  assert.equal(writeNode(nodes[6], index), "citydiff:lib:parser:rust:New");
});

test("a type is its package's path, then its name, and a method follows its type", () => {
  assert.equal(writeNode(nodes[7], index), "citydiff:lib:Parser");
  assert.equal(writeNode(nodes[8], index), "citydiff:lib:Parser:Parse");
});

test("a path two declarations share is told apart by the file", () => {
  assert.equal(writeNode(nodes[9], index), "citydiff:cmd:init@cmd/a.go");
  assert.equal(writeNode(nodes[10], index), "citydiff:cmd:init@cmd/b.go");
});

test("nothing selected is an empty path", () => {
  assert.equal(writeNode(null, index), "");
});

test("readNode finds what writeNode wrote", () => {
  for (const node of nodes) {
    const text = writeNode(node, index);
    if (text === "citydiff:lib@main.go") continue;
    assert.deepEqual(readNode(text, index), back(node), text);
  }
});

test("a package keeps its bare path over a declaration that spells the same", () => {
  assert.deepEqual(readNode("citydiff:lib", index), { kind: "package", id: "citydiff/lib" });
  assert.deepEqual(readNode("citydiff:lib@main.go", index), back(nodes[11]));
});

test("readNode picks the first of a shared path, and nothing for an unknown one", () => {
  assert.deepEqual(readNode("citydiff:cmd:init", index), back(nodes[9]));
  assert.equal(readNode("New", index), null);
  assert.equal(readNode("citydiff:lib:Missing", index), null);
  assert.equal(readNode("", index), null);
});

test("the defaults leave the address bare", () => {
  const state = { select: "", mode: "overview", refs: "calls", side: true, tourSide: true };
  assert.equal(writeState("", state), "");
  assert.deepEqual(readState(""), state);
});

test("the state is written readably and reads back", () => {
  const state = { select: "citydiff:lib:parser:rust:New", mode: "changes", refs: "callers", side: false, tourSide: false };
  const query = writeState("", state);
  assert.equal(query, "?select=citydiff:lib:parser:rust:New&mode=changes&refs=callers&side=closed&tourside=closed");
  assert.deepEqual(readState(query), state);
});

test("other parameters are kept, and stale state is replaced", () => {
  const state = { select: "citydiff:lib:Parser", mode: "overview", refs: "calls", side: true, tourSide: true };
  assert.equal(writeState("?tour=./t.json&mode=changes&select=x:y", state), "?tour=./t.json&select=citydiff:lib:Parser");
});

test("a name with characters a query cannot hold is escaped", () => {
  const query = writeState("", { select: "citydiff:a&b c", mode: "overview", refs: "calls", side: true, tourSide: true });
  assert.equal(query, "?select=citydiff:a%26b%20c");
  assert.equal(readState(query).select, "citydiff:a&b c");
});
