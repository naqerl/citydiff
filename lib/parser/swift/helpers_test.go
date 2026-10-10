package swift

import (
	"fmt"
	"testing"

	"citydiff/lib"
)

func parse(t *testing.T, files ...lib.File) []lib.ParsedFile {
	t.Helper()
	out, err := New().Parse(lib.Mem(files))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func file(p, src string) lib.File { return lib.File{Path: p, Src: []byte(src)} }

// entity finds owner.name (owner "" for a function) in one file.
func entity(t *testing.T, files []lib.ParsedFile, path, owner, name string) lib.FunctionEntry {
	t.Helper()
	for _, f := range files {
		if f.Path != path {
			continue
		}
		for _, e := range f.Entities {
			switch e := e.(type) {
			case lib.FunctionEntry:
				if owner == "" && e.Name == name {
					return e
				}
			case lib.MethodEntry:
				if e.Type != nil && e.Type.Name == owner && e.Name == name {
					return e.FunctionEntry
				}
			}
		}
	}
	t.Fatalf("no %s.%s in %s", owner, name, path)
	return lib.FunctionEntry{}
}

// calls renders each call as expr, or expr->path:Recv.Name when resolved.
func calls(t *testing.T, files []lib.ParsedFile, path, owner, name string) []string {
	t.Helper()
	var out []string
	for _, c := range entity(t, files, path, owner, name).Calls {
		s := c.Expr
		if c.Ref != nil {
			target := c.Ref.Name
			if c.Ref.Recv != "" {
				target = c.Ref.Recv + "." + c.Ref.Name
			}
			s += "->" + c.Ref.Path + ":" + target
		}
		out = append(out, s)
	}
	return out
}

func describe(files []lib.ParsedFile) []string {
	var out []string
	for _, f := range files {
		for _, e := range f.Entities {
			switch e := e.(type) {
			case lib.ImportEntry:
				out = append(out, fmt.Sprintf("%s import %s", f.ImportPath, e.Path))
			case lib.TypeEntry:
				out = append(out, fmt.Sprintf("%s type %s %v", f.ImportPath, e.Name, e.Fields))
			case lib.VariableEntry:
				out = append(out, fmt.Sprintf("%s var %s", f.ImportPath, e.Name))
			case lib.FunctionEntry:
				out = append(out, fmt.Sprintf("%s func %s", f.ImportPath, e.Name))
			case lib.MethodEntry:
				out = append(out, fmt.Sprintf("%s method %s.%s", f.ImportPath, e.Type.Name, e.Name))
			}
		}
	}
	return out
}
