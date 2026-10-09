package swift

import (
	"reflect"
	"strings"
	"testing"

	"citydiff/lib"
)

const manifestSrc = `// swift-tools-version:5.9
import PackageDescription

let package = Package(
  name: "acme",
  targets: [
    .target(name: "Core", dependencies: [.product(name: "Dep", package: "dep", path: "nope")]),
    .executableTarget(name: "tool", dependencies: ["Core"], path: "Tools/tool"),
    // .target(name: "Commented"),
    .testTarget(name: "CoreTests", dependencies: ["Core"]),
  ]
)
`

func TestManifestTargets(t *testing.T) {
	m := readManifest(".", []byte(manifestSrc))
	want := []target{{"Core", "Sources/Core"}, {"tool", "Tools/tool"}, {"CoreTests", "Tests/CoreTests"}}
	if m.name != "acme" || !reflect.DeepEqual(m.targets, want) {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestModulesFromTargetsAndFolders(t *testing.T) {
	files := parse(t,
		file("Package.swift", manifestSrc),
		file("Sources/Core/a.swift", "func a() {}"),
		file("Sources/Core/Sub/b.swift", "func b() {}"),
		file("Tools/tool/main.swift", "func main() {}"),
		file("Tests/CoreTests/t.swift", "func t() {}"),
		file("App/Views/v.swift", "func v() {}"),
		file("Sources/Loose/l.swift", "func l() {}"),
		file("top.swift", "func top() {}"),
	)
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.ImportPath + "|" + f.Package + "|" + f.Module
	}
	want := map[string]string{
		"Sources/Core/a.swift":     "Core|Core|acme",
		"Sources/Core/Sub/b.swift": "Core|Core|acme",
		"Tools/tool/main.swift":    "tool|tool|acme",
		"Tests/CoreTests/t.swift":  "CoreTests|CoreTests|acme",
		"App/Views/v.swift":        "App|App|acme",
		"Sources/Loose/l.swift":    "Loose|Loose|acme",
		"top.swift":                "main|main|acme",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("modules = %v", got)
	}
}

func TestNestedPackageKeepsItsModules(t *testing.T) {
	files := parse(t,
		file("Package.swift", manifestSrc),
		file("sub/Package.swift", `let package = Package(name: "sub", targets: [.target(name: "Core")])`),
		file("sub/Sources/Core/a.swift", "func a() {}"),
		file("Sources/Core/a.swift", "func a() {}"),
	)
	if files[0].ImportPath != "sub/Core" || files[0].Module != "sub" || files[1].ImportPath != "Core" {
		t.Fatalf("got %s %s, %s", files[0].ImportPath, files[0].Module, files[1].ImportPath)
	}
}

func TestEntities(t *testing.T) {
	src := `import Foundation
@testable import Core
import struct Other.Thing

typealias Handler = (Int) -> Void
let limit = 3
var count: Int = 0, other = 1

func top(_ x: Int) -> Widget { Widget(x) }

class Widget: Base, Drawable {
    var size: Int
    let child = Child()
    var area: Int { size * size }
    init(_ s: Int) { size = s }
    deinit {}
    subscript(i: Int) -> Int { size + i }
    func draw() {}
    static func make() -> Widget { Widget(2) }
    struct Inner { func f() {} }
}
struct P { var a: Int }
enum E { case a, b(Int)
  case c
  func e() {} }
protocol Drawable { func draw() }
actor A { func act() async {} }
extension Widget { func extra() {} }
extension Widget: Equatable, Hashable { static func == (l: Widget, r: Widget) -> Bool { true } }
extension Widget.Inner { func g() {} }
`
	got := describe(parse(t, file("App/a.swift", src)))
	want := []string{
		"App import Foundation",
		"App import Core",
		"App import Other.Thing",
		"App type Handler []",
		"App var limit",
		"App var count",
		"App var other",
		"App func top",
		"App type Widget [{size} {child}]",
		"App method Widget.area",
		"App method Widget.init",
		"App method Widget.deinit",
		"App method Widget.subscript",
		"App method Widget.draw",
		"App method Widget.make",
		"App type Widget.Inner []",
		"App method Widget.Inner.f",
		"App type P [{a}]",
		"App type E [{a} {b} {c}]",
		"App method E.e",
		"App type Drawable []",
		"App method Drawable.draw",
		"App type A []",
		"App method A.act",
		"App method Widget.extra",
		"App method <Widget as Equatable, Hashable>.==",
		"App method Widget.Inner.g",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entities:\n%s", strings.Join(got, "\n"))
	}
}

func TestRejectsOtherFiles(t *testing.T) {
	if _, err := New().Parse(lib.Mem([]lib.File{file("a.rs", "")})); err == nil {
		t.Fatal("a.rs parsed")
	}
}
