package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"betterdiff/lib"
)

func TestBodyHashUsesSourceAndRollsUpToType(t *testing.T) {
	src := []byte(strings.ReplaceAll(`package p

type Service struct{}

func (s *Service) A() { println("a") }
func (s Service) B() {
	println("b")
}
func New() int { return 1 }

type Box[T any] struct{}

func (b *Box[T]) Get() T {
	var zero T
	return zero
}
`, "\r\n", "\n"))

	entries := mustParse(t, src)
	newFn := functionNamed(t, entries, "New")
	if newFn.BodyHash != hashOf("{ return 1 }") {
		t.Fatalf("New body hash = %s", newFn.BodyHash)
	}

	a := methodNamed(t, entries, "A")
	b := methodNamed(t, entries, "B")
	if a.BodyHash != hashOf(`{ println("a") }`) {
		t.Fatalf("A body hash = %s", a.BodyHash)
	}
	if a.BodyHash == b.BodyHash {
		t.Fatal("different method bodies produced the same hash")
	}

	service := typeNamed(t, entries, "Service")
	if service.MethodsHash != cumulative(a.BodyHash, b.BodyHash) {
		t.Fatalf("Service methods hash = %s", service.MethodsHash)
	}
	box := typeNamed(t, entries, "Box")
	get := methodNamed(t, entries, "Get")
	if box.MethodsHash != cumulative(get.BodyHash) {
		t.Fatalf("Box methods hash = %s, method receiver %s", box.MethodsHash, get.Type.Name)
	}

	changed := bytesReplace(src, `println("a")`, `println("z")`)
	entries2 := mustParse(t, changed)
	a2 := methodNamed(t, entries2, "A")
	b2 := methodNamed(t, entries2, "B")
	service2 := typeNamed(t, entries2, "Service")
	if a2.BodyHash == a.BodyHash {
		t.Fatal("edited method body kept the same hash")
	}
	if b2.BodyHash != b.BodyHash {
		t.Fatal("unedited method hash changed")
	}
	if service2.MethodsHash == service.MethodsHash {
		t.Fatal("type hash did not change with a method body")
	}
}

func mustParse(t *testing.T, src []byte) []lib.Entity {
	t.Helper()
	entries, err := Parser{}.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func functionNamed(t *testing.T, entries []lib.Entity, name string) lib.FunctionEntry {
	t.Helper()
	for _, entry := range entries {
		fn, ok := entry.(lib.FunctionEntry)
		if ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return lib.FunctionEntry{}
}

func methodNamed(t *testing.T, entries []lib.Entity, name string) lib.MethodEntry {
	t.Helper()
	for _, entry := range entries {
		method, ok := entry.(lib.MethodEntry)
		if ok && method.Name == name {
			return method
		}
	}
	t.Fatalf("method %s not found", name)
	return lib.MethodEntry{}
}

func typeNamed(t *testing.T, entries []lib.Entity, name string) lib.TypeEntry {
	t.Helper()
	for _, entry := range entries {
		typ, ok := entry.(lib.TypeEntry)
		if ok && typ.Name == name {
			return typ
		}
	}
	t.Fatalf("type %s not found", name)
	return lib.TypeEntry{}
}

func hashOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func cumulative(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		raw, err := hex.DecodeString(part)
		if err != nil {
			panic(err)
		}
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func bytesReplace(src []byte, old, new string) []byte {
	return []byte(strings.Replace(string(src), old, new, 1))
}
