package golang

import (
	"testing"

	"citydiff/lib"
)

// A comment that opens a grouped type declaration used to be taken for the
// spec, and the parser crashed on its missing name.
func TestGroupedTypeAfterComment(t *testing.T) {
	src := "package p\n\ntype (\n\t// A is the first.\n\tA struct{ X int }\n\tB int\n)\n"
	files, err := Parser{}.Parse(lib.Mem([]lib.File{{Path: "a.go", Src: []byte(src)}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	typ := typeNamed(t, files[0].Entities, "A")
	if len(typ.Fields) != 1 || typ.Fields[0].Name != "X" {
		t.Fatalf("type = %+v", typ)
	}
}
