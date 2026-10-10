package scene

import (
	"encoding/json"
	"strings"
	"testing"

	"citydiff/lib"
	golang "citydiff/lib/parser/go"
)

func TestBuildNestsRootAndDiffsImports(t *testing.T) {
	left := []lib.ParsedFile{
		{
			Path: "a.go", Package: "barse", ImportPath: "barse", Module: "barse",
			Entities: []lib.Entity{
				lib.FunctionEntry{Name: "Main", BodyBytes: 8, Calls: []lib.Call{{Expr: "fmt.Println"}}},
			},
		},
		{
			Path: "sub/b.go", Package: "sub", ImportPath: "barse/sub", Module: "barse",
			Entities: []lib.Entity{
				lib.ImportEntry{Path: "fmt"},
				lib.TypeEntry{Name: "Box", Fields: []lib.Field{{Name: "N"}}},
				lib.MethodEntry{
					FunctionEntry: lib.FunctionEntry{
						Name: "Use", BodyBytes: 12,
						Calls: []lib.Call{
							{Expr: "B"},
							{Expr: "A"},
							{Expr: "B"},
						},
					},
					Type: &lib.TypeEntry{Name: "*Box"},
				},
			},
		},
	}
	right := []lib.ParsedFile{
		{
			Path: "a.go", Package: "barse", ImportPath: "barse", Module: "barse",
			Entities: []lib.Entity{
				lib.ImportEntry{Path: "barse/sub"},
				lib.FunctionEntry{Name: "Main", BodyBytes: 20, Calls: []lib.Call{{Expr: "fmt.Println"}, {Expr: "sub.Use"}}},
			},
		},
		{
			Path: "sub/b.go", Package: "sub", ImportPath: "barse/sub", Module: "barse",
			Entities: []lib.Entity{
				lib.ImportEntry{Path: "fmt"},
				lib.TypeEntry{Name: "Box", Fields: []lib.Field{{Name: "N"}}},
				lib.MethodEntry{
					FunctionEntry: lib.FunctionEntry{
						Name: "Use", BodyBytes: 18,
						Calls: []lib.Call{
							{Expr: "A"},
							{Expr: "B"},
							{Expr: "C", Ref: &lib.CallRef{Path: "sub/b.go", Name: "C"}},
						},
					},
					Type: &lib.TypeEntry{Name: "Box"},
				},
				lib.FunctionEntry{Name: "C", BodyBytes: 4},
			},
		},
	}

	got := Build(left, right)
	if !got.Diff || got.Module != "barse" || got.Root != "barse" {
		t.Fatalf("scene header = %+v", got)
	}
	root := mustPkg(t, got, "barse")
	sub := mustPkg(t, got, "barse/sub")
	if root.Parent != "" || sub.Parent != root.ID {
		t.Fatalf("parents root %q sub %q", root.Parent, sub.Parent)
	}
	if root.Change != modified || sub.Change != modified {
		t.Fatalf("changes root %s sub %s", root.Change, sub.Change)
	}
	dep := mustDep(t, root, "barse/sub")
	if dep.Change != added || dep.External {
		t.Fatalf("root dep = %+v", dep)
	}
	fmtDep := mustDep(t, sub, "fmt")
	if fmtDep.Change != same || !fmtDep.External || fmtDep.Files != 1 {
		t.Fatalf("fmt dep = %+v", fmtDep)
	}
	ext := mustPkg(t, got, "fmt")
	if !ext.External || ext.Parent != "" || ext.Change != same {
		t.Fatalf("external = %+v", ext)
	}

	use := mustEntity(t, sub, "method", "Use")
	if use.Recv != "Box" || use.Parent == "" || use.Change != modified {
		t.Fatalf("Use = %+v", use)
	}
	box := mustEntity(t, sub, "type", "Box")
	if use.Parent != box.ID || len(box.Fields) != 1 || box.Fields[0] != "N" {
		t.Fatalf("Box link parent %s box %+v", use.Parent, box)
	}
	if use.BodyBytes != 18 || use.BodyBytesBefore == nil || *use.BodyBytesBefore != 12 {
		t.Fatalf("Use size = %d before %v", use.BodyBytes, use.BodyBytesBefore)
	}
	want := []string{"removed B", "same A", "same B", "added C"}
	if len(use.Calls) != len(want) {
		t.Fatalf("calls = %+v", use.Calls)
	}
	for i, step := range use.Calls {
		got := step.Change + " " + step.Expr
		if got != want[i] {
			t.Fatalf("call %d = %s, want %s", i, got, want[i])
		}
	}
	if !use.Calls[3].Resolved || use.Calls[3].Name != "C" {
		t.Fatalf("added call = %+v", use.Calls[3])
	}
	if c := mustEntity(t, sub, "function", "C"); c.Change != added || len(c.Calls) != 0 {
		t.Fatalf("C = %+v", c)
	}
}

func TestBuildSnapshotDoesNotMarkAdditions(t *testing.T) {
	right := []lib.ParsedFile{{
		Path: "sub/b.go", Package: "sub", ImportPath: "barse/sub", Module: "barse",
		Entities: []lib.Entity{
			lib.ImportEntry{Path: "fmt"},
			lib.FunctionEntry{Name: "B", BodyBytes: 3, Calls: []lib.Call{{Expr: "fmt.Println"}}},
		},
	}}
	got := Build(nil, right)
	if got.Diff {
		t.Fatal("snapshot scene is a diff")
	}
	root := mustPkg(t, got, "barse")
	if !root.Synthetic || root.Change != same || root.Package != "" {
		t.Fatalf("synthetic root = %+v", root)
	}
	sub := mustPkg(t, got, "barse/sub")
	if sub.Parent != "barse" || sub.Change != same {
		t.Fatalf("sub = %+v", sub)
	}
	fn := mustEntity(t, sub, "function", "B")
	if fn.Change != same || fn.BodyBytesBefore != nil || len(fn.Calls) != 1 || fn.Calls[0].Change != same {
		t.Fatalf("B = %+v", fn)
	}
	if dep := mustDep(t, sub, "fmt"); dep.Change != same {
		t.Fatalf("dep = %+v", dep)
	}
}

func TestBuildEmptyLeftIsAllAdded(t *testing.T) {
	right := []lib.ParsedFile{{
		Path: "a.go", Package: "p", ImportPath: "example.com/p", Module: "example.com/p",
		Entities: []lib.Entity{lib.FunctionEntry{Name: "A", BodyBytes: 2}},
	}}
	got := Build([]lib.ParsedFile{}, right)
	if !got.Diff {
		t.Fatal("empty left was treated as a snapshot")
	}
	pkg := mustPkg(t, got, "example.com/p")
	if pkg.Synthetic || pkg.Change != added {
		t.Fatalf("package = %+v", pkg)
	}
	if fn := mustEntity(t, pkg, "function", "A"); fn.Change != added || fn.BodyBytesBefore != nil {
		t.Fatalf("A = %+v", fn)
	}
}

func TestBuildKeepsRemovedPackageAndDuplicateNames(t *testing.T) {
	left := []lib.ParsedFile{
		{Path: "a.go", Package: "p", ImportPath: "p", Entities: []lib.Entity{
			lib.FunctionEntry{Name: "F", BodyBytes: 5, Calls: []lib.Call{{Expr: "old"}}},
		}},
		{Path: "b.go", Package: "p", ImportPath: "p", Entities: []lib.Entity{
			lib.FunctionEntry{Name: "F", BodyBytes: 6},
			lib.FunctionEntry{Name: "F", BodyBytes: 7},
		}},
	}
	right := []lib.ParsedFile{
		{Path: "b.go", Package: "p", ImportPath: "p", Entities: []lib.Entity{
			lib.FunctionEntry{Name: "F", BodyBytes: 6},
		}},
	}
	got := Build(left, right)
	pkg := mustPkg(t, got, "p")
	if pkg.Change != modified {
		t.Fatalf("package change = %s", pkg.Change)
	}
	var fns []Entity
	for _, entity := range pkg.Entities {
		if entity.Name == "F" {
			fns = append(fns, entity)
		}
	}
	if len(fns) != 3 {
		t.Fatalf("F entities = %+v", fns)
	}
	ids := map[string]bool{}
	for _, fn := range fns {
		if ids[fn.ID] {
			t.Fatalf("duplicate id %s", fn.ID)
		}
		ids[fn.ID] = true
	}
	removedN := 0
	for _, fn := range fns {
		if fn.File == "a.go" {
			if fn.Change != removed || fn.BodyBytes != 0 || fn.BodyBytesBefore == nil || *fn.BodyBytesBefore != 5 {
				t.Fatalf("removed F = %+v", fn)
			}
			if len(fn.Calls) != 1 || fn.Calls[0].Change != removed {
				t.Fatalf("removed calls = %+v", fn.Calls)
			}
			removedN++
		}
	}
	if removedN != 1 {
		t.Fatalf("removed count = %d", removedN)
	}
}

func TestBuildFromParser(t *testing.T) {
	files, err := golang.New().Parse(lib.Mem([]lib.File{
		{Path: "go.mod", Src: []byte("module example.com/acme\n\ngo 1.22\n")},
		{Path: "a.go", Src: []byte("package acme\n\nfunc A() int { return 1 }\n")},
		{Path: "sub/b.go", Src: []byte("package sub\n\nimport \"fmt\"\n\nfunc B() { fmt.Println(1) }\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := Build(nil, files)
	root := mustPkg(t, got, "example.com/acme")
	sub := mustPkg(t, got, "example.com/acme/sub")
	if root.Synthetic || root.Package != "acme" || sub.Parent != root.ID {
		t.Fatalf("root %+v sub %+v", root, sub)
	}
	fn := mustEntity(t, root, "function", "A")
	if fn.BodyBytes != len("{ return 1 }") {
		t.Fatalf("A bytes = %d", fn.BodyBytes)
	}
	if dep := mustDep(t, sub, "fmt"); !dep.External {
		t.Fatalf("fmt dep = %+v", dep)
	}
}

func TestModifiedFunctionAndMethodParts(t *testing.T) {
	params := []lib.Parameter{{Name: "n", Type: "int"}}
	next := []lib.Parameter{{Name: "n", Type: "string"}}
	left := []lib.ParsedFile{{
		Path: "a.go", Package: "p", ImportPath: "p",
		Entities: []lib.Entity{
			lib.FunctionEntry{Name: "Sig", BodyHash: "s", Parameters: params},
			lib.FunctionEntry{Name: "Body", BodyHash: "b", Calls: []lib.Call{{Expr: "old"}}},
			lib.FunctionEntry{Name: "Both", BodyHash: "c", Parameters: params},
			lib.FunctionEntry{Name: "Same", BodyHash: "d"},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "M", BodyHash: "m"}, Type: &lib.TypeEntry{Name: "*T"}},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "N", BodyHash: "n", Parameters: params}, Type: &lib.TypeEntry{Name: "T"}},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "P", BodyHash: "p", Parameters: params}, Type: &lib.TypeEntry{Name: "T"}},
			lib.TypeEntry{Name: "T", Fields: []lib.Field{{Name: "A"}}},
		},
	}}
	right := []lib.ParsedFile{{
		Path: "a.go", Package: "p", ImportPath: "p",
		Entities: []lib.Entity{
			lib.FunctionEntry{Name: "Sig", BodyHash: "s", Parameters: next},
			lib.FunctionEntry{Name: "Body", BodyHash: "b2", Calls: []lib.Call{{Expr: "new"}}},
			lib.FunctionEntry{Name: "Both", BodyHash: "c2", Parameters: next},
			lib.FunctionEntry{Name: "Same", BodyHash: "d"},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "M", BodyHash: "m2"}, Type: &lib.TypeEntry{Name: "*T"}},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "N", BodyHash: "n", Parameters: next}, Type: &lib.TypeEntry{Name: "*T"}},
			lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "P", BodyHash: "p2", Parameters: next}, Type: &lib.TypeEntry{Name: "T"}},
			lib.TypeEntry{Name: "T", Fields: []lib.Field{{Name: "A"}, {Name: "B"}}},
		},
	}}
	pkg := mustPkg(t, Build(left, right), "p")
	want := map[string]string{
		"function Sig":  "signature",
		"function Body": "body",
		"function Both": "both",
		"function Same": "",
		"method M":      "body",
		"method N":      "signature",
		"method P":      "both",
		"type T":        "",
	}
	for _, entity := range pkg.Entities {
		key := entity.Kind + " " + entity.Name
		part, ok := want[key]
		if !ok {
			t.Fatalf("unexpected %s", key)
		}
		if entity.Part != part {
			t.Errorf("%s part = %q, want %q", key, entity.Part, part)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v", want)
	}
	raw, err := json.Marshal(mustEntity(t, pkg, "function", "Same"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"part"`) {
		t.Fatalf("unchanged json = %s", raw)
	}
}

func TestBodyBytesBeforeEncodesZero(t *testing.T) {
	left := []lib.ParsedFile{{
		Path: "a.go", Package: "p", ImportPath: "p",
		Entities: []lib.Entity{lib.FunctionEntry{Name: "A", BodyBytes: 0, BodyHash: "x"}},
	}}
	right := []lib.ParsedFile{{
		Path: "a.go", Package: "p", ImportPath: "p",
		Entities: []lib.Entity{lib.FunctionEntry{Name: "A", BodyBytes: 15, BodyHash: "y"}},
	}}
	fn := mustEntity(t, mustPkg(t, Build(left, right), "p"), "function", "A")
	raw, err := json.Marshal(fn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"bodyBytesBefore":0`) {
		t.Fatalf("json = %s", raw)
	}
}

func mustPkg(t *testing.T, sc Scene, id string) Package {
	t.Helper()
	for _, pkg := range sc.Packages {
		if pkg.ID == id {
			return pkg
		}
	}
	t.Fatalf("package %s not in %d packages", id, len(sc.Packages))
	return Package{}
}

func mustDep(t *testing.T, pkg Package, to string) Dep {
	t.Helper()
	for _, dep := range pkg.Deps {
		if dep.To == to {
			return dep
		}
	}
	t.Fatalf("dep %s not on %s (%v)", to, pkg.ID, pkg.Deps)
	return Dep{}
}

func mustEntity(t *testing.T, pkg Package, kind, name string) Entity {
	t.Helper()
	for _, entity := range pkg.Entities {
		if entity.Kind == kind && entity.Name == name {
			return entity
		}
	}
	t.Fatalf("%s %s not in %s", kind, name, pkg.ID)
	return Entity{}
}

func TestNormRecvUnwrapsTraitImplOwner(t *testing.T) {
	if got := normRecv("<DirEntry as Colorable>"); got != "DirEntry" {
		t.Fatalf("normRecv = %q", got)
	}
}
