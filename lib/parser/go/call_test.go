package golang

import (
	"reflect"
	"testing"

	"citydiff/lib"
)

func TestCallsResolveInEitherFileOrder(t *testing.T) {
	a := []byte("package p\n\nfunc A() { B() }\n")
	b := []byte("package p\n\nfunc B() { A() }\n")
	forward := mustParseFiles(t, lib.File{Path: "a.go", Src: a}, lib.File{Path: "b.go", Src: b})
	backward := mustParseFiles(t, lib.File{Path: "b.go", Src: b}, lib.File{Path: "a.go", Src: a})

	for _, files := range [][]lib.ParsedFile{forward, backward} {
		got := callRefs(t, files, "a.go", "A")
		want := []lib.Call{{Expr: "B", Ref: &lib.CallRef{Path: "b.go", Name: "B"}}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("A calls = %+v", got)
		}
		got = callRefs(t, files, "b.go", "B")
		want = []lib.Call{{Expr: "A", Ref: &lib.CallRef{Path: "a.go", Name: "A"}}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("B calls = %+v", got)
		}
	}
}

func TestCallsKeepSourceOrderAndUnresolved(t *testing.T) {
	src := []byte(`package p

import "fmt"

func A(ok bool) {
	if ok {
		B()
	} else {
		C(D())
	}
	defer E()
	go func(s *Service) {
		s.M()
	}()
	fmt.Println(missing())
}

func B() {}
func C(int) {}
func D() int { return 0 }
func E() {}

type Service struct{}

func (s *Service) M() { s.M() }
`)
	files := mustParseFiles(t, lib.File{Path: "p.go", Src: src})
	got := callRefs(t, files, "p.go", "A")
	ref := func(name, recv string) *lib.CallRef {
		return &lib.CallRef{Path: "p.go", Name: name, Recv: recv}
	}
	want := []lib.Call{
		{Expr: "B", Ref: ref("B", "")},
		{Expr: "C", Ref: ref("C", "")},
		{Expr: "D", Ref: ref("D", "")},
		{Expr: "E", Ref: ref("E", "")},
		{Expr: "func"},
		{Expr: "s.M", Ref: ref("M", "Service")},
		{Expr: "fmt.Println"},
		{Expr: "missing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}

	method := callRefs(t, files, "p.go", "M")
	if len(method) != 1 || method[0].Expr != "s.M" || method[0].Ref == nil || method[0].Ref.Recv != "Service" {
		t.Fatalf("M calls = %+v", method)
	}
}

func TestCallsResolveImportsAliasesAndParameterTypes(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "go.mod", Src: []byte("module example.com/m\n")},
		lib.File{Path: "util/util.go", Src: []byte("package helpers\n\nfunc A() {}\n")},
		lib.File{Path: "db/db.go", Src: []byte("package db\n\ntype Queries struct{}\n\nfunc (q *Queries) Save() {}\nfunc Helper() {}\n")},
		lib.File{Path: "p.go", Src: []byte(`package p

import "example.com/m/util"
import d "example.com/m/db"

func F(q *d.Queries) {
	helpers.A()
	q.Save()
	d.Helper()
	q.Missing()
}
`)},
	)
	got := callRefs(t, files, "p.go", "F")
	want := []lib.Call{
		{Expr: "helpers.A", Ref: &lib.CallRef{Path: "util/util.go", Name: "A"}},
		{Expr: "q.Save", Ref: &lib.CallRef{Path: "db/db.go", Name: "Save", Recv: "Queries"}},
		{Expr: "d.Helper", Ref: &lib.CallRef{Path: "db/db.go", Name: "Helper"}},
		{Expr: "q.Missing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCallsDoNotResolveShadowedReceiver(t *testing.T) {
	src := []byte(`package p

func (s *Service) A() {
	go func(s *Other) {
		s.B()
	}()
}

type Service struct{}
type Other struct{}

func (s *Service) B() {}
func (o *Other) B() {}
`)
	got := callRefs(t, mustParseFiles(t, lib.File{Path: "p.go", Src: src}), "p.go", "A")
	want := []lib.Call{
		{Expr: "func"},
		{Expr: "s.B", Ref: &lib.CallRef{Path: "p.go", Name: "B", Recv: "Other"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v", got)
	}
}

func TestCallsResolveGenericCallsAndKeepConversions(t *testing.T) {
	src := []byte(`package p

func ID[T any](v T) T { var zero T; return zero }

type S struct{}

func (s *S) M(v any) {}

func F(s *S, x any) {
	ID[int](1)
	s.M[int](x)
	ID[int]()
	string(x)
	T(x)
	ID[int](D())
}

func D() int { return 0 }

type T int
`)
	got := callRefs(t, mustParseFiles(t, lib.File{Path: "p.go", Src: src}), "p.go", "F")
	ref := func(name, recv string) *lib.CallRef {
		return &lib.CallRef{Path: "p.go", Name: name, Recv: recv}
	}
	want := []lib.Call{
		{Expr: "ID[int]", Ref: ref("ID", "")},
		{Expr: "s.M[int]", Ref: ref("M", "S")},
		{Expr: "ID[int]", Ref: ref("ID", "")},
		{Expr: "string"},
		{Expr: "T"},
		{Expr: "ID[int]", Ref: ref("ID", "")},
		{Expr: "D", Ref: ref("D", "")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCallsShadowInnerBindings(t *testing.T) {
	src := []byte(`package p

type Service struct{}
type Other struct{}

func (s *Service) N() {}
func (o *Other) N() {}
func (s *Service) B() {}
func (o *Other) B() {}

func helper() {}

func (s *Service) Short() {
	s.N()
	s := &Other{}
	s.N()
}

func (s *Service) Block() {
	s.N()
	{
		s := Other{}
		s.N()
	}
	s.N()
}

func (s *Service) If(v bool) {
	if s := &Other{}; v {
		s.N()
	}
	s.N()
}

func (s *Service) Switch(v any) {
	switch s := v.(type) {
	default:
		s.N()
	}
	s.N()
}

func (s *Service) Result() {
	go func() (s *Other) { s.B() }()
}

func Named() (s *Other) {
	s.B()
}

func (s *Service) Out() (q *Other) {
	q.B()
	s.B()
}

func (s *Service) Var() {
	var s *Other
	s.N()
}

func (s *Service) Range() {
	s.N()
	for s := range []int{1} {
		s.N()
	}
	s.N()
}

func BeforeHelper() {
	helper()
	helper := func() {}
	helper()
}
`)
	files := mustParseFiles(t, lib.File{Path: "p.go", Src: src})
	service := &lib.CallRef{Path: "p.go", Name: "N", Recv: "Service"}
	otherN := &lib.CallRef{Path: "p.go", Name: "N", Recv: "Other"}
	otherB := &lib.CallRef{Path: "p.go", Name: "B", Recv: "Other"}
	serviceB := &lib.CallRef{Path: "p.go", Name: "B", Recv: "Service"}
	cases := []struct {
		name string
		want []lib.Call
	}{
		{name: "Short", want: []lib.Call{{Expr: "s.N", Ref: service}, {Expr: "s.N", Ref: otherN}}},
		{name: "Block", want: []lib.Call{{Expr: "s.N", Ref: service}, {Expr: "s.N", Ref: otherN}, {Expr: "s.N", Ref: service}}},
		{name: "If", want: []lib.Call{{Expr: "s.N", Ref: otherN}, {Expr: "s.N", Ref: service}}},
		{name: "Switch", want: []lib.Call{{Expr: "s.N"}, {Expr: "s.N", Ref: service}}},
		{name: "Result", want: []lib.Call{{Expr: "func"}, {Expr: "s.B", Ref: otherB}}},
		{name: "Named", want: []lib.Call{{Expr: "s.B", Ref: otherB}}},
		{name: "Out", want: []lib.Call{{Expr: "q.B", Ref: otherB}, {Expr: "s.B", Ref: serviceB}}},
		{name: "Var", want: []lib.Call{{Expr: "s.N", Ref: otherN}}},
		{name: "Range", want: []lib.Call{{Expr: "s.N", Ref: service}, {Expr: "s.N"}, {Expr: "s.N", Ref: service}}},
		{name: "BeforeHelper", want: []lib.Call{
			{Expr: "helper", Ref: &lib.CallRef{Path: "p.go", Name: "helper"}},
			{Expr: "helper"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := callRefs(t, files, "p.go", tc.name)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("calls =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestCallsResolveDotImportedMethods(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "go.mod", Src: []byte("module example.com/m\n")},
		lib.File{Path: "db/db.go", Src: []byte("package db\n\ntype Queries struct{}\n\nfunc (q *Queries) Save() {}\nfunc Helper() {}\n")},
		lib.File{Path: "p.go", Src: []byte(`package p

import . "example.com/m/db"

func F(q *Queries) {
	q.Save()
	Helper()
}
`)},
	)
	got := callRefs(t, files, "p.go", "F")
	want := []lib.Call{
		{Expr: "q.Save", Ref: &lib.CallRef{Path: "db/db.go", Name: "Save", Recv: "Queries"}},
		{Expr: "Helper", Ref: &lib.CallRef{Path: "db/db.go", Name: "Helper"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}

	ambiguous := mustParseFiles(t,
		lib.File{Path: "go.mod", Src: []byte("module example.com/m\n")},
		lib.File{Path: "db/db.go", Src: []byte("package db\n\ntype Queries struct{}\n\nfunc (q *Queries) Save() {}\nfunc Helper() {}\n")},
		lib.File{Path: "db2/db2.go", Src: []byte("package db2\n\ntype Queries struct{}\n\nfunc (q *Queries) Save() {}\n")},
		lib.File{Path: "p.go", Src: []byte(`package p

import . "example.com/m/db"
import . "example.com/m/db2"

func F(q *Queries) {
	q.Save()
	Helper()
}
`)},
	)
	got = callRefs(t, ambiguous, "p.go", "F")
	want = []lib.Call{
		{Expr: "q.Save"},
		{Expr: "Helper", Ref: &lib.CallRef{Path: "db/db.go", Name: "Helper"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ambiguous calls =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCallsResolveGOOSGOARCHVariantsToFirstDecl(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "a_linux.go", Src: []byte("package p\n\nfunc impl() {}\n")},
		lib.File{Path: "a_windows.go", Src: []byte("package p\n\nfunc impl() {}\n")},
		lib.File{Path: "a_linux_amd64.go", Src: []byte("package p\n\nfunc combo() {}\n")},
		lib.File{Path: "a_windows_amd64.go", Src: []byte("package p\n\nfunc combo() {}\n")},
		lib.File{Path: "a_amd64.go", Src: []byte("package p\n\nfunc archFn() {}\n")},
		lib.File{Path: "a_arm64.go", Src: []byte("package p\n\nfunc archFn() {}\n")},
		lib.File{Path: "m_linux.go", Src: []byte("package p\n\nfunc (s *S) M() {}\n")},
		lib.File{Path: "m_windows.go", Src: []byte("package p\n\nfunc (s *S) M() {}\n")},
		lib.File{Path: "a.go", Src: []byte(`package p

type S struct{}

func caller() {
	impl()
	combo()
	archFn()
}

func (s *S) Use() { s.M() }
`)},
	)
	got := callRefs(t, files, "a.go", "caller")
	want := []lib.Call{
		{Expr: "impl", Ref: &lib.CallRef{Path: "a_linux.go", Name: "impl"}},
		{Expr: "combo", Ref: &lib.CallRef{Path: "a_linux_amd64.go", Name: "combo"}},
		{Expr: "archFn", Ref: &lib.CallRef{Path: "a_amd64.go", Name: "archFn"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
	}
	got = callRefs(t, files, "a.go", "Use")
	want = []lib.Call{{Expr: "s.M", Ref: &lib.CallRef{Path: "m_linux.go", Name: "M", Recv: "S"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method calls =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCallsLeaveUnsuffixedAndGOOSVariantUnresolved(t *testing.T) {
	files := mustParseFiles(t,
		lib.File{Path: "a.go", Src: []byte("package p\n\nfunc caller() { impl(); other() }\nfunc impl() {}\n")},
		lib.File{Path: "a_linux.go", Src: []byte("package p\n\nfunc impl() {}\n")},
		lib.File{Path: "b_linux.go", Src: []byte("package p\n\nfunc other() {}\n")},
		lib.File{Path: "c_linux.go", Src: []byte("package p\n\nfunc other() {}\n")},
	)
	got := callRefs(t, files, "a.go", "caller")
	want := []lib.Call{{Expr: "impl"}, {Expr: "other"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", got, want)
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
