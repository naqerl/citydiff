package rust

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

func ref(name, recv string) *lib.CallRef {
	return &lib.CallRef{Path: "src/lib.rs", Name: name, Recv: recv}
}

func one(t *testing.T, src string, fn string) []lib.Call {
	t.Helper()
	return callRefs(t, mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: []byte(src)}), "src/lib.rs", fn)
}

func TestTraitImplMethodsStayDistinctAndInherentWins(t *testing.T) {
	src := `
trait Colorable { fn path(&self) -> String; fn paint(&self); }
trait Other { fn paint(&self); }
struct Entry;
impl Entry { fn path(&self) -> String { String::new() } }
impl Colorable for Entry {
    fn path(&self) -> String { self.path() }
    fn paint(&self) {}
}
impl Other for Entry { fn paint(&self) {} }
struct Lone;
impl Colorable for Lone { fn path(&self) -> String { String::new() } fn paint(&self) {} }
fn run(e: Entry, l: Lone) { e.path(); l.path(); e.paint(); }
`
	files := mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: []byte(src)})
	var owners []string
	for _, e := range files[0].Entities {
		if m, ok := e.(lib.MethodEntry); ok && m.Name == "path" {
			owners = append(owners, m.Type.Name)
		}
	}
	if want := []string{"Colorable", "Entry", "<Entry as Colorable>", "<Lone as Colorable>"}; !reflect.DeepEqual(owners, want) {
		t.Fatalf("path owners = %v", owners)
	}
	want := []lib.Call{
		{Expr: "e.path", Ref: ref("path", "Entry")},
		{Expr: "l.path", Ref: ref("path", "<Lone as Colorable>")},
		{Expr: "e.paint"},
	}
	if got := callRefs(t, files, "src/lib.rs", "run"); !reflect.DeepEqual(got, want) {
		t.Fatalf("run calls = %+v", got)
	}
	entry := typeNamed(t, files[0].Entities, "Entry")
	if entry.MethodsHash == "" {
		t.Fatal("trait impl methods did not roll up to Entry")
	}
}

func TestInlineModuleQualifiesEveryItem(t *testing.T) {
	src := `
struct Config;
impl Config { fn load() -> Self { Config } }
mod tests {
    use super::*;
    struct Config;
    impl Config { fn load() -> Self { Config } fn check(&self) {} }
    const N: usize = 1;
    fn helper() { let c = Config::load(); c.check(); super::Config::load(); }
}
`
	files := mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: []byte(src)})
	entries := files[0].Entities
	typeNamed(t, entries, "tests::Config")
	if m := methodNamed(t, entries, "check"); m.Type.Name != "tests::Config" {
		t.Fatalf("check owner = %s", m.Type.Name)
	}
	found := false
	for _, e := range entries {
		if v, ok := e.(lib.VariableEntry); ok && v.Name == "tests::N" {
			found = true
		}
	}
	if !found {
		t.Fatal("no tests::N")
	}
	want := []lib.Call{
		{Expr: "Config::load", Ref: ref("load", "tests::Config")},
		{Expr: "c.check", Ref: ref("check", "tests::Config")},
		{Expr: "super::Config::load", Ref: ref("load", "Config")},
	}
	if got := callRefs(t, files, "src/lib.rs", "tests::helper"); !reflect.DeepEqual(got, want) {
		t.Fatalf("helper calls = %+v", got)
	}
}

func TestMacroArgumentsAreParsedAsExpressions(t *testing.T) {
	src := `
struct S;
impl S { fn name(&self) -> String { String::new() } fn new() -> S { S } }
fn f() -> i32 { 0 }
fn run(s: S) {
    println!("{} {}", s.name(), S::new().name());
    let v = vec![f(); 3];
    assert_eq!(f(), format!("{}", f()).len() as i32, "{}", s.name());
    custom!(f());
}
`
	want := []lib.Call{
		{Expr: "println!"},
		{Expr: "s.name", Ref: ref("name", "S")},
		{Expr: "S::new", Ref: ref("new", "S")},
		{Expr: "S::new().name", Ref: ref("name", "S")},
		{Expr: "vec!"},
		{Expr: "f", Ref: ref("f", "")},
		{Expr: "assert_eq!"},
		{Expr: "f", Ref: ref("f", "")},
		{Expr: "format!"},
		{Expr: "format!(\"{}\", f()).len"},
		{Expr: "f", Ref: ref("f", "")},
		{Expr: "s.name", Ref: ref("name", "S")},
		{Expr: "custom!"},
		{Expr: "f", Ref: ref("f", "")},
	}
	if got := one(t, src, "run"); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
}

func TestInferLocalTypes(t *testing.T) {
	src := `
use std::io;
struct Db { conn: Conn, spare: Option<Conn> }
struct Conn;
struct Rows;
impl Db {
    fn new() -> Self { Db { conn: Conn, spare: None } }
    fn open(path: &str) -> io::Result<Self> { Ok(Db::new()) }
    fn find() -> Option<Db> { None }
    fn query(&self) -> Rows { self.conn.exec(); Rows }
}
impl Conn { fn exec(&self) {} }
impl Rows { fn count(&self) -> usize { 0 } }
fn connect() -> Conn { Conn }
fn run() -> io::Result<()> {
    let a = Db::new();
    let b = Db::open("x")?;
    let c = Db::find().unwrap();
    let d = Db { conn: Conn, spare: None };
    let e = connect();
    let f: Db = make();
    a.query();
    b.query();
    c.query();
    d.conn.exec();
    e.exec();
    f.query();
    a.query().count();
    let g = Db::find();
    g.query();
    d.spare.exec();
    Ok(())
}
`
	want := []lib.Call{
		{Expr: "Db::new", Ref: ref("new", "Db")},
		{Expr: "Db::open", Ref: ref("open", "Db")},
		{Expr: "Db::find", Ref: ref("find", "Db")},
		{Expr: "Db::find().unwrap"},
		{Expr: "connect", Ref: ref("connect", "")},
		{Expr: "make"},
		{Expr: "a.query", Ref: ref("query", "Db")},
		{Expr: "b.query", Ref: ref("query", "Db")},
		{Expr: "c.query", Ref: ref("query", "Db")},
		{Expr: "d.conn.exec", Ref: ref("exec", "Conn")},
		{Expr: "e.exec", Ref: ref("exec", "Conn")},
		{Expr: "f.query", Ref: ref("query", "Db")},
		{Expr: "a.query", Ref: ref("query", "Db")},
		{Expr: "a.query().count", Ref: ref("count", "Rows")},
		{Expr: "Db::find", Ref: ref("find", "Db")},
		{Expr: "g.query"},
		{Expr: "d.spare.exec"},
		{Expr: "Ok"},
	}
	if got := one(t, src, "run"); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
	if got := one(t, src, "query"); !reflect.DeepEqual(got, []lib.Call{{Expr: "self.conn.exec", Ref: ref("exec", "Conn")}}) {
		t.Fatalf("query calls = %+v", got)
	}
}

func TestInferLeavesUnknownAndShadowedUnresolved(t *testing.T) {
	src := `
mod a { pub struct T; impl T { pub fn go(&self) {} } }
mod b { pub struct T; impl T { pub fn go(&self) {} } }
use a::*;
use b::*;
struct U;
impl U { fn go(&self) {} }
fn run(x: T, items: Vec<U>) {
    x.go();
    let u = U;
    {
        let u = other();
        u.go();
    }
    u.go();
    items.go();
    for u in items { u.go(); }
}
`
	want := []lib.Call{
		{Expr: "x.go"},
		{Expr: "other"},
		{Expr: "u.go"},
		{Expr: "u.go", Ref: ref("go", "U")},
		{Expr: "items.go"},
		{Expr: "u.go"},
	}
	if got := one(t, src, "run"); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
}
