package swift

import (
	"reflect"
	"testing"
)

const inferSrc = `
struct Engine {
    func start() {}
    func clone() -> Engine { self }
    static func shared() -> Engine { Engine() }
}
class Car {
    let engine: Engine
    var spare: Engine?
    lazy var backup = Engine()
    var wheels: [Engine] = []
    init() { engine = Engine() }
    func make() -> Car { Car() }
    func drive(_ other: Car?) {
        engine.start()
        self.engine.start()
        backup.start()
        spare?.start()
        spare!.start()
        spare.start()
        make().engine.clone().start()
        other?.engine.start()
        if let s = spare { s.start() }
        if let spare { spare.start() }
        guard let o = other else { return }
        o.engine.start()
    }
}
func build() -> Engine { Engine() }
func failable() throws -> Engine { Engine() }
func use() throws {
    let a = Engine()
    a.start()
    let b: Engine = .shared()
    b.start()
    let c = build()
    c.start()
    let d = try failable()
    d.start()
    let e = try? failable()
    e?.start()
    let f = Engine.shared()
    f.start()
    var g = a
    g.start()
    let h = Car().engine
    h.start()
    let (x, y) = (a, a)
    x.start()
    for w in Car().wheels { w.start() }
    let a2 = 1
    a2.start()
}
`

func TestInference(t *testing.T) {
	files := parse(t, file("A/a.swift", inferSrc))
	start := "->A/a.swift:Engine.start"
	want := []string{
		"engine.start" + start, "self.engine.start" + start, "backup.start" + start,
		"spare?.start" + start, "spare!.start" + start, "spare.start",
		"make->A/a.swift:Car.make", "make().engine.clone->A/a.swift:Engine.clone", "make().engine.clone().start" + start,
		"other?.engine.start" + start,
		"s.start" + start, "spare.start" + start,
		"o.engine.start" + start,
	}
	if got := calls(t, files, "A/a.swift", "Car", "drive"); !reflect.DeepEqual(got, want) {
		t.Fatalf("drive =\n%v\nwant\n%v", got, want)
	}
	want = []string{
		"Engine", "a.start" + start,
		".shared", "b.start" + start,
		"build->A/a.swift:build", "c.start" + start,
		"failable->A/a.swift:failable", "d.start" + start,
		"failable->A/a.swift:failable", "e?.start" + start,
		"Engine.shared->A/a.swift:Engine.shared", "f.start" + start,
		"g.start" + start,
		"Car->A/a.swift:Car.init", "h.start" + start,
		"x.start",
		"Car->A/a.swift:Car.init", "w.start",
		"a2.start",
	}
	if got := calls(t, files, "A/a.swift", "", "use"); !reflect.DeepEqual(got, want) {
		t.Fatalf("use =\n%v\nwant\n%v", got, want)
	}
}

func TestCastsAndOptionalBinding(t *testing.T) {
	src := `
protocol Cmd { func run() }
protocol AsyncCmd: Cmd { func run() async }
func main(c: Cmd) {
    if var a = c as? AsyncCmd { a.run() }
    (c as? AsyncCmd).run()
    (c as! AsyncCmd).run()
    c.run()
}
`
	files := parse(t, file("A/a.swift", src))
	want := []string{"a.run->A/a.swift:AsyncCmd.run", "(c as? AsyncCmd).run", "(c as! AsyncCmd).run->A/a.swift:AsyncCmd.run", "c.run->A/a.swift:Cmd.run"}
	if got := calls(t, files, "A/a.swift", "", "main"); !reflect.DeepEqual(got, want) {
		t.Fatalf("main = %v", got)
	}
}

func TestShadowingKeepsCallsUnresolved(t *testing.T) {
	src := `
struct Engine { func start() {} }
func run() {}
struct S {
    var run: () -> Void
    func go(engine: Int) { run(); engine.start() }
}
func outer() { let run = { }; run() }
`
	files := parse(t, file("A/a.swift", src))
	if got := calls(t, files, "A/a.swift", "S", "go"); !reflect.DeepEqual(got, []string{"run", "engine.start"}) {
		t.Fatalf("go = %v", got)
	}
	if got := calls(t, files, "A/a.swift", "", "outer"); !reflect.DeepEqual(got, []string{"run"}) {
		t.Fatalf("outer = %v", got)
	}
}

func TestTypealiasAndNestedTypes(t *testing.T) {
	src := `
struct Outer {
    struct Inner { func f() {} }
    func g() { let i = Inner(); i.f(); Outer.Inner().f() }
}
typealias Alias = Outer.Inner
func h(a: Alias) { a.f() }
`
	files := parse(t, file("A/a.swift", src))
	want := []string{"Inner", "i.f->A/a.swift:Outer.Inner.f", "Outer.Inner", "Outer.Inner().f->A/a.swift:Outer.Inner.f"}
	if got := calls(t, files, "A/a.swift", "Outer", "g"); !reflect.DeepEqual(got, want) {
		t.Fatalf("g = %v", got)
	}
	if got := calls(t, files, "A/a.swift", "", "h"); !reflect.DeepEqual(got, []string{"a.f->A/a.swift:Outer.Inner.f"}) {
		t.Fatalf("h = %v", got)
	}
}
