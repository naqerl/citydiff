package scene

import (
	"testing"

	"citydiff/lib"
	"citydiff/lib/parser"
)

// Every declaration carries the line and column of its name, for Go, Rust
// and Swift, so the viewer can open it in an editor at the right place.
func TestEntitiesCarryTheirPosition(t *testing.T) {
	files := []lib.File{
		{Path: "go.mod", Src: []byte("module m\n")},
		{Path: "a.go", Src: []byte("package m\n\ntype T struct{}\n\nfunc (t T) M() {}\n\n  func F() {}\n")},
		{Path: "Cargo.toml", Src: []byte("[package]\nname = \"c\"\n")},
		{Path: "src/lib.rs", Src: []byte("struct S;\n\nimpl S {\n    fn m(&self) {}\n}\n\npub fn f() {}\n")},
		{Path: "Package.swift", Src: []byte("let package = Package(name: \"P\", targets: [.target(name: \"A\")])\n")},
		{Path: "Sources/A/a.swift", Src: []byte("struct W {\n  @discardableResult\n  func go() -> Int { 1 }\n}\n")},
	}
	right, err := parser.New().Parse(lib.Mem(files))
	if err != nil {
		t.Fatal(err)
	}
	sc := Build(nil, right)
	want := map[string][2]int{
		"a.go T": {3, 6}, "a.go M": {5, 12}, "a.go F": {7, 8},
		"src/lib.rs S": {1, 8}, "src/lib.rs m": {4, 8}, "src/lib.rs f": {7, 8},
		"Sources/A/a.swift W": {1, 8}, "Sources/A/a.swift go": {3, 8},
	}
	for _, p := range sc.Packages {
		for _, e := range p.Entities {
			k := e.File + " " + e.Name
			if w, ok := want[k]; ok {
				if e.Line != w[0] || e.Col != w[1] {
					t.Errorf("%s at %d:%d, want %d:%d", k, e.Line, e.Col, w[0], w[1])
				}
				delete(want, k)
			}
		}
	}
	if len(want) > 0 {
		t.Errorf("missing %v", want)
	}
}
