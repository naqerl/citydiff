package rust

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

func TestCallsResolveInEitherFileOrder(t *testing.T) {
	libFile := lib.File{Path: "src/lib.rs", Src: []byte("mod b;\npub fn a() { b::b() }\n")}
	b := lib.File{Path: "src/b.rs", Src: []byte("pub fn b() { crate::a() }\n")}
	for _, files := range [][]lib.ParsedFile{mustParseFiles(t, libFile, b), mustParseFiles(t, b, libFile)} {
		if got := callRefs(t, files, "src/lib.rs", "a"); !reflect.DeepEqual(got, []lib.Call{{Expr: "b::b", Ref: &lib.CallRef{Path: "src/b.rs", Name: "b"}}}) {
			t.Fatalf("a calls = %+v", got)
		}
		if got := callRefs(t, files, "src/b.rs", "b"); !reflect.DeepEqual(got, []lib.Call{{Expr: "crate::a", Ref: &lib.CallRef{Path: "src/lib.rs", Name: "a"}}}) {
			t.Fatalf("b calls = %+v", got)
		}
	}
}

func TestCallsKeepSourceOrderAndUnresolved(t *testing.T) {
	src := []byte(`
fn a(ok: bool, s: &Service) {
    if ok {
        b();
    } else {
        c(d());
    }
    let run = |x: i32| e(x);
    s.m();
    println!("{}", f());
    std::mem::drop(missing());
    let b = 1;
    b();
}

fn b() {}
fn c(_: i32) {}
fn d() -> i32 { 0 }
fn e(_: i32) {}
fn f() -> i32 { 0 }

struct Service;

impl Service {
    fn m(&self) { self.m(); Self::n(); }
    fn n() {}
}
`)
	files := mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: src})
	ref := func(name, recv string) *lib.CallRef {
		return &lib.CallRef{Path: "src/lib.rs", Name: name, Recv: recv}
	}
	want := []lib.Call{
		{Expr: "b", Ref: ref("b", "")},
		{Expr: "c", Ref: ref("c", "")},
		{Expr: "d", Ref: ref("d", "")},
		{Expr: "e", Ref: ref("e", "")},
		{Expr: "s.m", Ref: ref("m", "Service")},
		{Expr: "println!"},
		{Expr: "f", Ref: ref("f", "")},
		{Expr: "std::mem::drop"},
		{Expr: "missing"},
		{Expr: "b"},
	}
	if got := callRefs(t, files, "src/lib.rs", "a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
	want = []lib.Call{{Expr: "self.m", Ref: ref("m", "Service")}, {Expr: "Self::n", Ref: ref("n", "Service")}}
	if got := callRefs(t, files, "src/lib.rs", "m"); !reflect.DeepEqual(got, want) {
		t.Fatalf("m calls = %+v", got)
	}
}

func TestCallsResolveUsesAliasesAndAssociatedFunctions(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "Cargo.toml", Src: []byte("[package]\nname = \"acme\"\n")},
		lib.File{Path: "src/lib.rs", Src: []byte("pub mod store;\npub mod util;\n")},
		lib.File{Path: "src/store.rs", Src: []byte(`
use crate::util::{helper, other as renamed};
use super::util::*;

pub struct Store;

impl Store {
    pub fn new() -> Self { Store }
    pub fn save(&self) {}
}

pub fn run() {
    helper();
    renamed();
    globbed();
    let s = Store::new();
    let t = Store {};
    t.save();
    s.save();
    util_call();
}

fn util_call() { crate::util::helper(); super::util::other(); }
`)},
		lib.File{Path: "src/util.rs", Src: []byte("pub fn helper() {}\npub fn other() {}\npub fn globbed() {}\n")},
		lib.File{Path: "src/main.rs", Src: []byte("fn main() { acme::store::run(); }\n")},
		lib.File{Path: "tests/api.rs", Src: []byte("use acme::store::Store;\n#[test]\nfn t() { Store::new(); }\n")},
	)
	util := func(name string) *lib.CallRef { return &lib.CallRef{Path: "src/util.rs", Name: name} }
	store := func(name, recv string) *lib.CallRef {
		return &lib.CallRef{Path: "src/store.rs", Name: name, Recv: recv}
	}
	want := []lib.Call{
		{Expr: "helper", Ref: util("helper")},
		{Expr: "renamed", Ref: util("other")},
		{Expr: "globbed", Ref: util("globbed")},
		{Expr: "Store::new", Ref: store("new", "Store")},
		{Expr: "t.save", Ref: store("save", "Store")},
		{Expr: "s.save", Ref: store("save", "Store")},
		{Expr: "util_call", Ref: store("util_call", "")},
	}
	if got := callRefs(t, files, "src/store.rs", "run"); !reflect.DeepEqual(got, want) {
		t.Fatalf("run calls =\n%+v\nwant\n%+v", got, want)
	}
	want = []lib.Call{{Expr: "crate::util::helper", Ref: util("helper")}, {Expr: "super::util::other", Ref: util("other")}}
	if got := callRefs(t, files, "src/store.rs", "util_call"); !reflect.DeepEqual(got, want) {
		t.Fatalf("util_call calls = %+v", got)
	}
	if got := callRefs(t, files, "src/main.rs", "main"); !reflect.DeepEqual(got, []lib.Call{{Expr: "acme::store::run", Ref: store("run", "")}}) {
		t.Fatalf("main calls = %+v", got)
	}
	if got := callRefs(t, files, "tests/api.rs", "t"); !reflect.DeepEqual(got, []lib.Call{{Expr: "Store::new", Ref: store("new", "Store")}}) {
		t.Fatalf("test calls = %+v", got)
	}
}

func TestCallsInInlineModuleUseItsPath(t *testing.T) {
	files := mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: []byte(`
fn top() {}
mod inner {
    fn a() { b(); super::top(); top(); }
    fn b() {}
}
`)})
	want := []lib.Call{
		{Expr: "b", Ref: &lib.CallRef{Path: "src/lib.rs", Name: "inner::b"}},
		{Expr: "super::top", Ref: &lib.CallRef{Path: "src/lib.rs", Name: "top"}},
		{Expr: "top"},
	}
	if got := callRefs(t, files, "src/lib.rs", "inner::a"); !reflect.DeepEqual(got, want) {
		t.Fatalf("a calls = %+v", got)
	}
}

func TestCallsLeaveAmbiguousMethodsUnresolved(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "src/lib.rs", Src: []byte("mod a;\nmod b;\nfn run(x: Thing) { x.go(); }\n")},
		lib.File{Path: "src/a.rs", Src: []byte("pub struct Thing;\nimpl Thing { pub fn go(&self) {} }\n")},
		lib.File{Path: "src/b.rs", Src: []byte("pub struct Thing;\nimpl Thing { pub fn go(&self) {} }\n")},
	)
	if got := callRefs(t, files, "src/lib.rs", "run"); !reflect.DeepEqual(got, []lib.Call{{Expr: "x.go"}}) {
		t.Fatalf("run calls = %+v", got)
	}
}

func mustParseFiles(t *testing.T, files ...lib.File) []lib.ParsedFile {
	t.Helper()
	parsed, err := Parser{}.Parse(lib.Mem(files))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func callRefs(t *testing.T, files []lib.ParsedFile, path, name string) []lib.Call {
	t.Helper()
	for _, file := range files {
		if file.Path != path {
			continue
		}
		for _, entry := range file.Entities {
			switch entry := entry.(type) {
			case lib.FunctionEntry:
				if entry.Name == name {
					return entry.Calls
				}
			case lib.MethodEntry:
				if entry.Name == name {
					return entry.Calls
				}
			}
		}
	}
	t.Fatalf("no %s in %s", name, path)
	return nil
}
