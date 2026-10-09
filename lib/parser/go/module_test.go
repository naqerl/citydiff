package golang

import (
	"testing"

	"betterdiff/lib"
)

func TestParseRecordsModuleImportPath(t *testing.T) {
	files, err := Parser{}.Parse(lib.Mem([]lib.File{
		{Path: "go.mod", Src: []byte("module example.com/acme\n\ngo 1.22\n")},
		{Path: "a.go", Src: []byte("package acme\n\nfunc A() int { return 1 }\n")},
		{Path: "sub/b.go", Src: []byte("package sub\n\nfunc B() {}\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	root, sub := files[0], files[1]
	if root.Path != "a.go" || root.Package != "acme" || root.Module != "example.com/acme" || root.ImportPath != "example.com/acme" {
		t.Fatalf("root = %+v", root)
	}
	if sub.Path != "sub/b.go" || sub.Package != "sub" || sub.Module != "example.com/acme" || sub.ImportPath != "example.com/acme/sub" {
		t.Fatalf("sub = %+v", sub)
	}
}
