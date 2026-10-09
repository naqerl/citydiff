package parser

import (
	"testing"

	"citydiff/lib"
)

func TestParseRoutesEachLanguageAndKeepsOrder(t *testing.T) {
	files, err := New().Parse(lib.Mem([]lib.File{
		{Path: "Cargo.toml", Src: []byte("[package]\nname = \"acme\"\n")},
		{Path: "go.mod", Src: []byte("module example.com/acme\n")},
		{Path: "src/lib.rs", Src: []byte("pub fn a() { b() }\nfn b() {}\n")},
		{Path: "tool/main.go", Src: []byte("package main\n\nfunc main() { run() }\n\nfunc run() {}\n")},
		{Path: "Package.swift", Src: []byte(`let package = Package(name: "acme", targets: [.target(name: "Kit")])`)},
		{Path: "Sources/Kit/k.swift", Src: []byte("func k() { j() }\nfunc j() {}\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].Path != "src/lib.rs" || files[1].Path != "tool/main.go" || files[2].Path != "Sources/Kit/k.swift" {
		t.Fatalf("files = %+v", files)
	}
	if files[0].ImportPath != "acme" || files[1].ImportPath != "example.com/acme/tool" || files[2].ImportPath != "Kit" {
		t.Fatalf("import paths = %q %q %q", files[0].ImportPath, files[1].ImportPath, files[2].ImportPath)
	}
	for _, f := range files {
		fn := f.Entities[0].(lib.FunctionEntry)
		if len(fn.Calls) != 1 || fn.Calls[0].Ref == nil || fn.Calls[0].Ref.Path != f.Path {
			t.Fatalf("%s calls = %+v", f.Path, fn.Calls)
		}
	}
}

func TestParseRejectsUnsupportedFile(t *testing.T) {
	if _, err := New().Parse(lib.Mem([]lib.File{{Path: "a.py", Src: []byte("")}})); err == nil {
		t.Fatal("parsed a.py")
	}
}

func TestInclude(t *testing.T) {
	for name, want := range map[string]bool{
		"a.go": true, "go.mod": true, "src/a.rs": true, "Cargo.toml": true,
		"Sources/A/a.swift": true, "Package.swift": true, "Package.resolved": false,
		"Cargo.lock": false, "a.py": false, "README.md": false,
	} {
		if Include(name) != want {
			t.Fatalf("Include(%q) = %v", name, !want)
		}
	}
}
