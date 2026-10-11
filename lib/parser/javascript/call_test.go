package javascript

import (
	"reflect"
	"strings"
	"testing"

	"citydiff/lib"
)

func check(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestSameFileAndSourceOrderWithCallbacks(t *testing.T) {
	out := parse(t, file("a.js", `function a() {}
function b() {}
const c = () => {}
function run(list) {
  a(b())
  list.forEach((x) => { c(); x() })
  setTimeout(function () { a() }, 1)
  unknown()
}
`))
	check(t, calls(t, out, "a.js", "", "run"), []string{
		"a->a.js:a", "b->a.js:b", "list.forEach", "c->a.js:c", "x", "setTimeout", "a->a.js:a", "unknown",
	})
}

func TestImportedBindings(t *testing.T) {
	out := parse(t,
		file("package.json", `{"name": "app"}`),
		file("lib/util.js", "export function named() {}\nexport default function main() {}\nfunction inner() {}\nexport { inner as renamed }\n"),
		file("lib/cjs.js", "function one() {}\nfunction two() {}\nmodule.exports = { one, two }\n"),
		file("lib/single.js", "module.exports = function single() {}\n"),
		file("lib/proto.js", "exports.go = function () {}\n"),
		file("lib/barrel.js", "export * from './util.js'\nexport { default as Main } from './util.js'\n"),
		file("src/use.mjs", `import main, { named, renamed as r } from "../lib/util.js"
import * as ns from "../lib/util.js"
import { Main, named as viaBarrel } from "../lib/barrel.js"
const cjs = require("../lib/cjs")
const { two } = require("../lib/cjs.js")
const single = require("../lib/single")
const proto = require("../lib/proto")
export function use() {
  main(); named(); r(); ns.named(); ns.missing(); Main(); viaBarrel()
  cjs.one(); two(); single(); proto.go()
}
`),
	)
	check(t, calls(t, out, "src/use.mjs", "", "use"), []string{
		"main->lib/util.js:main", "named->lib/util.js:named", "r->lib/util.js:inner", "ns.named->lib/util.js:named", "ns.missing",
		"Main->lib/util.js:main", "viaBarrel->lib/util.js:named",
		"cjs.one->lib/cjs.js:one", "two->lib/cjs.js:two", "single->lib/single.js:single", "proto.go->lib/proto.js:go",
	})
}

func TestClassesThisSuperAndNew(t *testing.T) {
	out := parse(t,
		file("base.js", "export class Base {\n  constructor() {}\n  hello() {}\n  static make() {}\n}\n"),
		file("a.js", `import { Base } from "./base.js"
class Shape extends Base {
  area() { this.hello(); this.size(); super.hello() }
  size() { const self = this; return function () { this.area() } }
  static build() { this.make(); Shape.make(); new Shape(); new Base() }
}
function use() {
  const s = new Shape()
  s.area()
  let t = new Shape()
  t = other
  t.area()
  Base.make()
  Shape.area()
}
`),
	)
	check(t, calls(t, out, "a.js", "Shape", "area"), []string{"this.hello->base.js:Base.hello", "this.size->a.js:Shape.size", "super.hello->base.js:Base.hello"})
	check(t, calls(t, out, "a.js", "Shape", "size"), []string{"this.area"})
	check(t, calls(t, out, "a.js", "Shape", "build"), []string{"this.make->base.js:Base.make", "Shape.make->base.js:Base.make", "new Shape->base.js:Base.constructor", "new Base->base.js:Base.constructor"})
	check(t, calls(t, out, "a.js", "", "use"), []string{"new Shape->base.js:Base.constructor", "s.area->a.js:Shape.area", "new Shape->base.js:Base.constructor", "t.area", "Base.make->base.js:Base.make", "Shape.area"})
}

func TestPrototypeAndObjectOwners(t *testing.T) {
	out := parse(t, file("app.js", `var app = exports = module.exports = {}
app.init = function () { this.set(); app.set() }
app.set = function () {}
function Router() {}
Router.prototype.handle = function () { this.next() }
Router.prototype.next = function () {}
function make() { const r = new Router(); r.handle(); Router() }
`))
	check(t, calls(t, out, "app.js", "app", "init"), []string{"this.set->app.js:app.set", "app.set->app.js:app.set"})
	check(t, calls(t, out, "app.js", "Router", "handle"), []string{"this.next->app.js:Router.next"})
	check(t, calls(t, out, "app.js", "", "make"), []string{"new Router->app.js:Router", "r.handle->app.js:Router.handle", "Router->app.js:Router"})
}

func TestJSDocParamType(t *testing.T) {
	out := parse(t, file("a.js", `class Store { get() {} }
/**
 * @param {Store} store the store
 * @param {Store} other
 */
function use(store, other) { store.get(); other = 1; other.get() }
`))
	check(t, calls(t, out, "a.js", "", "use"), []string{"store.get->a.js:Store.get", "other.get"})
}

func TestShadowingAndAmbiguity(t *testing.T) {
	out := parse(t, file("a.js", `function f() {}
function f() {}
function g() {}
class C { m() {} m() {} }
function use(g) { f(); g(); const c = new C(); c.m() }
function local() { const g = 1; g() }
`))
	check(t, calls(t, out, "a.js", "", "use"), []string{"f", "g", "new C", "c.m"})
	check(t, calls(t, out, "a.js", "", "local"), []string{"g"})
}

// Express's style: var f = require("./utils").f, and a require that is
// called at once, still import their modules.
func TestRequireMemberAndCalledRequire(t *testing.T) {
	out := parse(t,
		file("package.json", `{"name": "app"}`),
		file("lib/utils.js", "exports.compile = function () {}\n"),
		file("lib/app.js", "var compile = require('./utils').compile\nvar debug = require('debug')('app')\nvar Emitter = require('node:events').EventEmitter\nfunction run() { compile(); debug() }\n"),
	)
	check(t, calls(t, out, "lib/app.js", "", "run"), []string{"compile->lib/utils.js:compile", "debug"})
	var imps []string
	for _, f := range out {
		if f.Path == "lib/app.js" {
			for _, e := range describe([]lib.ParsedFile{f}) {
				if strings.Contains(e, " import ") {
					imps = append(imps, e)
				}
			}
		}
	}
	check(t, imps, []string{"app/lib import node:events", "app/lib import debug"})
}

func TestChainedMemberAssignmentNamesBoth(t *testing.T) {
	out := parse(t, file("res.js", "var res = {}\nres.set = res.header = function header() {}\nres.json = function () { this.set(); this.header() }\n"))
	check(t, calls(t, out, "res.js", "res", "json"), []string{"this.set->res.js:res.set", "this.header->res.js:res.header"})
}
