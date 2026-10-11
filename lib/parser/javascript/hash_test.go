package javascript

import (
	"testing"

	"citydiff/lib"
)

func classHash(t *testing.T, src string) (string, string) {
	t.Helper()
	out := parse(t, file("a.js", src))
	var typ, body string
	for _, e := range out[0].Entities {
		switch e := e.(type) {
		case lib.TypeEntry:
			typ = e.MethodsHash
		case lib.MethodEntry:
			if e.Name == "m" {
				body = e.BodyHash
			}
		}
	}
	return typ, body
}

func TestBodyAndMethodsHash(t *testing.T) {
	t1, b1 := classHash(t, "class C {\n  m() { return 1 }\n  n() {}\n}\n")
	t2, b2 := classHash(t, "\n\nclass C {\n    m() { return 1 }\n  n() {}\n}\n")
	if t1 == "" || b1 == "" || t1 != t2 || b1 != b2 {
		t.Fatal("moving the class must keep its hashes")
	}
	t3, b3 := classHash(t, "class C {\n  m() { return 2 }\n  n() {}\n}\n")
	if b3 == b1 || t3 == t1 {
		t.Fatal("a body change must change both hashes")
	}
	t4, b4 := classHash(t, "class C {\n  m() { return 1 }\n  n() { x() }\n}\n")
	if b4 != b1 || t4 == t1 {
		t.Fatal("another method's change moves only the class hash")
	}
}
