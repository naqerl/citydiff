package swift

import (
	"testing"

	"citydiff/lib"
)

func TestBodyHashFollowsTheBodyOnly(t *testing.T) {
	a := parse(t, file("A/a.swift", "func f(x: Int) { print(x) }\nstruct S { var v: Int { 1 } }"))
	b := parse(t, file("A/a.swift", "// moved\n\nfunc f(x: Int)   { print(x) }\nstruct S { var v: Int { 1 } }"))
	c := parse(t, file("A/a.swift", "func f(x: Int) { print(x + 1) }\nstruct S { var v: Int { 2 } }"))
	fa, fb, fc := entity(t, a, "A/a.swift", "", "f"), entity(t, b, "A/a.swift", "", "f"), entity(t, c, "A/a.swift", "", "f")
	if fa.BodyHash == "" || fa.BodyHash != fb.BodyHash || fa.BodyHash == fc.BodyHash {
		t.Fatalf("hashes %q %q %q", fa.BodyHash, fb.BodyHash, fc.BodyHash)
	}
	if fa.BodyBytes != len("{ print(x) }") || fc.BodyBytes != fa.BodyBytes+4 {
		t.Fatalf("bytes %d %d", fa.BodyBytes, fc.BodyBytes)
	}
	va, vc := entity(t, a, "A/a.swift", "S", "v"), entity(t, c, "A/a.swift", "S", "v")
	if va.BodyHash == "" || va.BodyHash == vc.BodyHash {
		t.Fatal("computed property body not hashed")
	}
}

func TestRequirementHasNoBody(t *testing.T) {
	f := entity(t, parse(t, file("A/a.swift", "protocol P { func m() }")), "A/a.swift", "P", "m")
	if f.BodyHash != "" || f.BodyBytes != 0 {
		t.Fatalf("requirement hashed: %+v", f)
	}
}

func methodsHash(t *testing.T, files []lib.ParsedFile, name string) string {
	t.Helper()
	for _, e := range files[0].Entities {
		if typ, ok := e.(lib.TypeEntry); ok && typ.Name == name {
			return typ.MethodsHash
		}
	}
	t.Fatalf("no type %s", name)
	return ""
}

func TestMethodsHashCoversExtensionsAndConformances(t *testing.T) {
	base := "struct S { func a() {} }\nextension S { func b() {} }\nextension S: P { func c() {} }\n"
	h := methodsHash(t, parse(t, file("A/a.swift", base)), "S")
	if h == "" {
		t.Fatal("no methods hash")
	}
	for _, src := range []string{
		"struct S { func a() { x() } }\nextension S { func b() {} }\nextension S: P { func c() {} }\n",
		"struct S { func a() {} }\nextension S { func b() { x() } }\nextension S: P { func c() {} }\n",
		"struct S { func a() {} }\nextension S { func b() {} }\nextension S: P { func c() { x() } }\n",
	} {
		if methodsHash(t, parse(t, file("A/a.swift", src)), "S") == h {
			t.Fatalf("methods hash ignores a change in\n%s", src)
		}
	}
	if methodsHash(t, parse(t, file("A/a.swift", "// c\n"+base)), "S") != h {
		t.Fatal("methods hash moved with a comment")
	}
}
