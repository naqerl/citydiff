package rust

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

func TestParseRecordsCrateImportPath(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "Cargo.toml", Src: []byte("[package]\nname = \"acme-core\"\nversion = \"0.1.0\"\n")},
		lib.File{Path: "src/lib.rs", Src: []byte("pub mod net;\npub fn a() -> i32 { 1 }\n")},
		lib.File{Path: "src/net/mod.rs", Src: []byte("pub mod tcp;\n")},
		lib.File{Path: "src/net/tcp.rs", Src: []byte("pub fn b() {}\n")},
		lib.File{Path: "src/bin/tool.rs", Src: []byte("fn main() {}\n")},
	)
	got := make([][3]string, len(files))
	for i, f := range files {
		if f.Module != "acme_core" {
			t.Fatalf("%s module = %q", f.Path, f.Module)
		}
		got[i] = [3]string{f.Path, f.Package, f.ImportPath}
	}
	want := [][3]string{
		{"src/lib.rs", "acme_core", "acme_core"},
		{"src/net/mod.rs", "net", "acme_core/net"},
		{"src/net/tcp.rs", "tcp", "acme_core/net/tcp"},
		{"src/bin/tool.rs", "tool", "acme_core/src/bin/tool"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v", got)
	}
}

func TestParseImportsPointAtSnapshotModules(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "Cargo.toml", Src: []byte("[package]\nname = \"acme\"\n")},
		lib.File{Path: "src/lib.rs", Src: []byte("mod net;\nuse crate::net::{tcp::connect, Addr as A};\nuse std::collections::HashMap;\nuse serde::*;\nextern crate alloc;\n")},
		lib.File{Path: "src/net.rs", Src: []byte("use super::*;\npub struct Addr;\n")},
		lib.File{Path: "src/net/tcp.rs", Src: []byte("pub fn connect() {}\n")},
	)
	var got []string
	for _, e := range files[0].Entities {
		if imp, ok := e.(lib.ImportEntry); ok {
			got = append(got, imp.Path)
		}
	}
	want := []string{"acme/net/tcp", "acme/net", "std::collections::HashMap", "serde::*", "alloc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imports = %v", got)
	}
	if imp := files[1].Entities[0].(lib.ImportEntry); imp.Path != "acme" {
		t.Fatalf("net import = %v", imp)
	}
}

func TestParseEntities(t *testing.T) {
	entries := mustParse(t, []byte(`
const LIMIT: usize = 3;
static NAME: &str = "a";
pub struct Point { pub x: i32, y: i32 }
struct Unit;
enum Shape { Circle(f64), Square { side: f64 } }
type Alias = Point;
trait Area { fn area(&self) -> f64; fn twice(&self) -> f64 { self.area() * 2.0 } }
impl Point { pub fn new(x: i32, y: i32) -> Self { Point { x, y } } }
pub fn run(p: &Point, n: usize) -> Result<(), String> { Ok(()) }
mod inner { pub fn hidden() {} }
`))
	var got []string
	for _, e := range entries {
		got = append(got, e.Kind().String()+" "+name(e))
	}
	want := []string{
		"variable LIMIT", "variable NAME", "type Point", "type Unit", "type Shape", "type Alias",
		"type Area", "method area", "method twice", "method new", "function run", "function inner::hidden",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v", got)
	}
	if p := typeNamed(t, entries, "Point"); !reflect.DeepEqual(p.Fields, []lib.Field{{Name: "x"}, {Name: "y"}}) {
		t.Fatalf("Point fields = %+v", p.Fields)
	}
	if s := typeNamed(t, entries, "Shape"); !reflect.DeepEqual(s.Fields, []lib.Field{{Name: "Circle"}, {Name: "Square"}}) {
		t.Fatalf("Shape fields = %+v", s.Fields)
	}
	run := functionNamed(t, entries, "run")
	if !reflect.DeepEqual(run.Parameters, []lib.Parameter{{Name: "p", Type: "&Point"}, {Name: "n", Type: "usize"}}) ||
		!reflect.DeepEqual(run.ReturnArgs, []lib.Parameter{{Type: "Result<(), String>"}}) {
		t.Fatalf("run = %+v", run)
	}
	newFn := methodNamed(t, entries, "new")
	if newFn.Type.Name != "Point" || len(newFn.Parameters) != 2 {
		t.Fatalf("new = %+v", newFn)
	}
	if area := methodNamed(t, entries, "area"); area.Type.Name != "Area" || area.BodyHash != "" {
		t.Fatalf("area = %+v", area)
	}
}

func name(e lib.Entity) string {
	switch e := e.(type) {
	case lib.ImportEntry:
		return e.Path
	case lib.TypeEntry:
		return e.Name
	case lib.VariableEntry:
		return e.Name
	case lib.FunctionEntry:
		return e.Name
	case lib.MethodEntry:
		return e.Name
	}
	return ""
}

func TestParseTestHelperModuleIsSharedByTestCrates(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "Cargo.toml", Src: []byte("[package]\nname = \"acme\"\n")},
		lib.File{Path: "tests/tests.rs", Src: []byte("mod testenv;\nuse testenv::TestEnv;\nfn t() { TestEnv::new(); }\n")},
		lib.File{Path: "tests/testenv/mod.rs", Src: []byte("pub struct TestEnv;\nimpl TestEnv { pub fn new() -> Self { TestEnv } }\n")},
		lib.File{Path: "src/bin/tool/main.rs", Src: []byte("fn main() {}\n")},
	)
	got := []string{files[0].ImportPath, files[1].ImportPath, files[1].Package, files[2].ImportPath}
	want := []string{"acme/tests/tests", "acme/tests/testenv", "testenv", "acme/src/bin/tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import paths = %v", got)
	}
	if imp := files[0].Entities[0].(lib.ImportEntry); imp.Path != "acme/tests/testenv" {
		t.Fatalf("use = %v", imp)
	}
	want2 := []lib.Call{{Expr: "TestEnv::new", Ref: &lib.CallRef{Path: "tests/testenv/mod.rs", Name: "new", Recv: "TestEnv"}}}
	if got := callRefs(t, files, "tests/tests.rs", "t"); !reflect.DeepEqual(got, want2) {
		t.Fatalf("t calls = %+v", got)
	}
}
