package javascript

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

func TestModulesAreDirectoriesUnderTheNamedPackage(t *testing.T) {
	out := parse(t,
		file("package.json", `{"name": "app"}`),
		file("index.js", "export const x = 1\n"),
		file("lib/router/index.js", "export function route() {}\n"),
		file("packages/ui/package.json", `{"name": "@acme/ui"}`),
		file("packages/ui/src/button.jsx", "export function Button() { return <b/> }\n"),
		file("test/fixtures/package.json", `{"private": true}`),
		file("test/fixtures/a.cjs", "module.exports = 1\n"),
	)
	got := map[string][3]string{}
	for _, f := range out {
		got[f.Path] = [3]string{f.ImportPath, f.Package, f.Module}
	}
	want := map[string][3]string{
		"index.js":                   {"app", "app", "app"},
		"lib/router/index.js":        {"app/lib/router", "router", "app"},
		"packages/ui/src/button.jsx": {"@acme/ui/src", "src", "@acme/ui"},
		"test/fixtures/a.cjs":        {"app/test/fixtures", "fixtures", "app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestNoManifestUsesTheDirectory(t *testing.T) {
	out := parse(t, file("src/a.js", "function f() {}\n"), file("b.mjs", "const y = 2\n"))
	if out[0].ImportPath != "src" || out[0].Package != "src" || out[1].ImportPath != "" || out[1].Package != "root" {
		t.Fatalf("%+v %+v", out[0], out[1])
	}
}

func TestImportsResolveRelativeSpecifiers(t *testing.T) {
	out := parse(t,
		file("package.json", `{"name": "app"}`),
		file("lib/a.js", `import x from "./util"
import * as ns from "../src/index.js"
import { y } from "./same.js"
import fs from "node:fs"
import fp from "lodash/fp"
import "@scope/pkg/deep/path"
export * from "../src"
const r = require("../src/r")
const { z } = require("express")
`),
		file("lib/util/index.mjs", "export default 1\n"),
		file("lib/same.js", "export const y = 1\n"),
		file("src/index.js", ""),
		file("src/r.cjs", ""),
	)
	var got []string
	for _, e := range out[0].Entities {
		if imp, ok := e.(lib.ImportEntry); ok {
			got = append(got, imp.Path)
		}
	}
	want := []string{"app/lib/util", "app/src", "node:fs", "lodash", "@scope/pkg", "express"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imports %v", got)
	}
}

func TestEntities(t *testing.T) {
	out := parse(t, file("a.js", `import { h } from "./b.js"
export function f(a, { b }, ...rest) {}
function* gen() {}
const arrow = (x) => x
let expr = function named() {}
var plain = 1, other = "s"
const { d1, d2: renamed } = obj
export class Shape extends Base {
  sides = 3
  #secret
  static count = 0
  handler = () => {}
  constructor() {}
  area() {}
  static make() {}
  get name() {}
  set name(v) {}
}
const K = class {}
export const api = { list() {}, get: (id) => id, size: 3 }
const hidden = { nope() {} }
function Router() {}
Router.prototype.handle = function () {}
Router.create = function () {}
module.exports.helper = function () {}
exports.value = 42
`))
	got := describe(out)
	want := []string{
		" import ./b.js",
		" func f", " func gen", " func arrow", " func expr",
		" var plain", " var other", " var d1", " var renamed",
		" type Shape [{sides} {#secret} {count}]",
		" method Shape.handler", " method Shape.constructor", " method Shape.area", " method Shape.make",
		" method Shape.name", " method Shape.name",
		" type K []",
		" var api", " var hidden",
		" func Router", " method Router.handle", " method Router.create",
		" func helper", " var value",
		" method api.list", " method api.get",
	}
	// Owner types made from assignments and objects come last, in any order.
	if len(got) < len(want) || !reflect.DeepEqual(got[:len(want)], want) {
		t.Fatalf("got\n%v", got)
	}
	rest := map[string]bool{}
	for _, g := range got[len(want):] {
		rest[g] = true
	}
	if len(rest) != 2 || !rest[" type Router []"] || !rest[" type api []"] {
		t.Fatalf("owner types %v", got[len(want):])
	}
}

func TestPositionsAreTheName(t *testing.T) {
	out := parse(t, file("a.js", "export function f() {}\nclass C {\n  static m() {}\n}\nconst g = () => 1\n"))
	pos := map[string][2]int{}
	for _, e := range out[0].Entities {
		p := lib.PosOf(e)
		switch e := e.(type) {
		case lib.FunctionEntry:
			pos[e.Name] = [2]int{p.Line, p.Column}
		case lib.MethodEntry:
			pos[e.Name] = [2]int{p.Line, p.Column}
		case lib.TypeEntry:
			pos[e.Name] = [2]int{p.Line, p.Column}
		}
	}
	want := map[string][2]int{"f": {1, 17}, "C": {2, 7}, "m": {3, 10}, "g": {5, 7}}
	if !reflect.DeepEqual(pos, want) {
		t.Fatalf("%v", pos)
	}
}

func TestRejectsOtherFiles(t *testing.T) {
	if _, err := New().Parse(lib.Mem([]lib.File{{Path: "a.ts", Src: []byte("")}})); err == nil {
		t.Fatal("want an error")
	}
}
