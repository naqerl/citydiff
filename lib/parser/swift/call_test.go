package swift

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

type fileT = lib.File

func TestCallsAcrossFilesOfOneModule(t *testing.T) {
	a := file("Sources/M/a.swift", "func a() { b() }\n")
	b := file("Sources/M/b.swift", "func b() { a(); Widget(1).draw() }\nstruct Widget { init(_ x: Int) {}\n func draw() {} }\n")
	for _, order := range [][]int{{0, 1}, {1, 0}} {
		in := []fileT{a, b}
		files := parse(t, in[order[0]], in[order[1]])
		if got := calls(t, files, "Sources/M/a.swift", "", "a"); !reflect.DeepEqual(got, []string{"b->Sources/M/b.swift:b"}) {
			t.Fatalf("a = %v", got)
		}
		want := []string{"a->Sources/M/a.swift:a", "Widget->Sources/M/b.swift:Widget.init", "Widget(1).draw->Sources/M/b.swift:Widget.draw"}
		if got := calls(t, files, "Sources/M/b.swift", "", "b"); !reflect.DeepEqual(got, want) {
			t.Fatalf("b = %v", got)
		}
	}
}

func TestCallsInSourceOrderWithClosures(t *testing.T) {
	src := `
func a(ok: Bool, s: Service) {
    if ok { b() } else { c(d()) }
    let run = { (x: Int) in e(x) }
    s.m()
    items.forEach { f($0) }
    s.with(1) { g() }
    print("\(d())")
    let b = 1
    b()
    run(1)
}
func b() {}
func c(_ x: Int) {}
func d() -> Int { 0 }
func e(_ x: Int) {}
func f(_ x: Int) {}
func g() {}
class Service {
    func m() { self.m(); n(); Service.s() }
    func n() {}
    static func s() {}
    func with(_ x: Int, _ body: () -> Void) {}
}
`
	files := parse(t, file("A/a.swift", src))
	want := []string{
		"b->A/a.swift:b", "c->A/a.swift:c", "d->A/a.swift:d", "e->A/a.swift:e",
		"s.m->A/a.swift:Service.m", "items.forEach", "f->A/a.swift:f",
		"s.with->A/a.swift:Service.with", "g->A/a.swift:g",
		"print", "d->A/a.swift:d", "b", "run",
	}
	if got := calls(t, files, "A/a.swift", "", "a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("a =\n%v\nwant\n%v", got, want)
	}
	want = []string{"self.m->A/a.swift:Service.m", "n->A/a.swift:Service.n", "Service.s->A/a.swift:Service.s"}
	if got := calls(t, files, "A/a.swift", "Service", "m"); !reflect.DeepEqual(got, want) {
		t.Fatalf("m = %v", got)
	}
}

func TestImplicitSelfBeatsTopLevel(t *testing.T) {
	src := "func run() {}\nstruct S { func run() {}\n func go() { run() } }\nstruct T { func go() { run() } }\n"
	files := parse(t, file("A/a.swift", src))
	if got := calls(t, files, "A/a.swift", "S", "go"); !reflect.DeepEqual(got, []string{"run->A/a.swift:S.run"}) {
		t.Fatalf("S.go = %v", got)
	}
	if got := calls(t, files, "A/a.swift", "T", "go"); !reflect.DeepEqual(got, []string{"run->A/a.swift:run"}) {
		t.Fatalf("T.go = %v", got)
	}
}

func TestOverloadsResolveByLabels(t *testing.T) {
	src := `
func f(a: Int) {}
func f(b: Int, c: Int = 0) {}
func g(_ x: Int) {}
func g(_ x: String) {}
func h(_ xs: Int...) {}
func use() { f(a: 1); f(b: 1); f(b: 1, c: 2); g(1); f(z: 1); h(1, 2, 3) }
`
	files := parse(t, file("A/a.swift", src))
	want := []string{"f->A/a.swift:f", "f->A/a.swift:f", "f->A/a.swift:f", "g", "f", "h->A/a.swift:h"}
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use = %v", got)
	}
}

func TestStaticAndInstanceAndInit(t *testing.T) {
	src := `
struct S {
    init() {}
    init(name: String) {}
    static func make() -> S { S() }
    func make() {}
    func use() { S.make(); make(); S(name: "x"); S.init(); Self.make() }
}
`
	files := parse(t, file("A/a.swift", src))
	want := []string{
		"S.make->A/a.swift:S.make", "make->A/a.swift:S.make", "S->A/a.swift:S.init",
		"S.init->A/a.swift:S.init", "Self.make->A/a.swift:S.make",
	}
	if got := calls(t, files, "A/a.swift", "S", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use = %v", got)
	}
}

func TestConformanceExtensionsAreDistinct(t *testing.T) {
	src := `
protocol Named { func name() -> String }
extension Named { func shout() {} }
struct S { func name() -> String { "" } }
extension S: Named { func describe() {} }
struct T: Named { func name() -> String { "" } }
func use(s: S, t: T, n: Named) { s.name(); s.describe(); s.shout(); t.shout(); n.name() }
`
	files := parse(t, file("A/a.swift", src))
	want := []string{
		"s.name->A/a.swift:S.name",
		"s.describe->A/a.swift:<S as Named>.describe",
		"s.shout->A/a.swift:Named.shout",
		"t.shout->A/a.swift:Named.shout",
		"n.name->A/a.swift:Named.name",
	}
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use = %v", got)
	}
}

func TestSuperclassMembers(t *testing.T) {
	src := "class Base { func base() {}\n func over() {} }\nclass Kid: Base { override func over() { super.over(); base() } }\nfunc use(k: Kid) { k.base(); k.over() }\n"
	files := parse(t, file("A/a.swift", src))
	if got := calls(t, files, "A/a.swift", "Kid", "over"); !reflect.DeepEqual(got, []string{"super.over->A/a.swift:Base.over", "base->A/a.swift:Base.base"}) {
		t.Fatalf("over = %v", got)
	}
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, []string{"k.base->A/a.swift:Base.base", "k.over->A/a.swift:Kid.over"}) {
		t.Fatalf("use = %v", got)
	}
}

func TestImportedPackageModules(t *testing.T) {
	manifest := `let package = Package(name: "p", targets: [.target(name: "Core"), .target(name: "App"), .target(name: "Other")])`
	files := parse(t,
		file("Package.swift", manifest),
		file("Sources/Core/c.swift", "public func helper() {}\npublic struct Tool { public init() {}\n public func run() {} }\n"),
		file("Sources/Other/o.swift", "public func helper() {}\n"),
		file("Sources/App/a.swift", "import Core\nfunc use() { helper(); Tool().run(); Core.helper() }\n"),
		file("Sources/App/b.swift", "import Core\nimport Other\nfunc both() { helper(); Other.helper() }\n"),
	)
	want := []string{"helper->Sources/Core/c.swift:helper", "Tool->Sources/Core/c.swift:Tool.init", "Tool().run->Sources/Core/c.swift:Tool.run", "Core.helper->Sources/Core/c.swift:helper"}
	if got := calls(t, files, "Sources/App/a.swift", "", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use = %v", got)
	}
	if got := calls(t, files, "Sources/App/b.swift", "", "both"); !reflect.DeepEqual(got, []string{"helper", "Other.helper->Sources/Other/o.swift:helper"}) {
		t.Fatalf("both = %v", got)
	}
}

func TestAmbiguousStaysUnresolved(t *testing.T) {
	src := "struct S { func m() {} }\nextension S: P { func m() {} }\nextension S: Q { func m() {} }\nfunc use(s: S) { s.m() }\n" +
		"struct U {}\nextension U: P { func k() {} }\nextension U: Q { func k() {} }\nfunc use2(u: U) { u.k() }\n"
	files := parse(t, file("A/a.swift", src))
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, []string{"s.m->A/a.swift:S.m"}) {
		t.Fatalf("use = %v", got)
	}
	if got := calls(t, files, "A/a.swift", "", "use2"); !reflect.DeepEqual(got, []string{"u.k"}) {
		t.Fatalf("use2 = %v", got)
	}
}

func TestMemberwiseInitMakesSameLabelsAmbiguous(t *testing.T) {
	src := `
struct G { var name: String; var size: Int = 1 }
extension G { init(name: [String]) { self.name = ""; size = 0 } }
extension G { init(other: Int) { name = ""; size = other } }
struct H { let id: Int; init(id: Int) { self.id = id } }
extension H { init(name: String) { id = 0 } }
func use() { G(name: ["a"]); G(other: 1); G(name: "a", size: 2); H(id: 1); H(name: "x") }
`
	files := parse(t, file("A/a.swift", src))
	want := []string{"G", "G->A/a.swift:G.init", "G", "H->A/a.swift:H.init", "H->A/a.swift:H.init"}
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use = %v", got)
	}
}
