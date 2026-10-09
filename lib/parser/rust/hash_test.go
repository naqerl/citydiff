package rust

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"citydiff/lib"
)

func TestBodyHashUsesSourceAndRollsUpToType(t *testing.T) {
	src := []byte(`struct Service;

impl Service {
    fn a(&self) { println!("a"); }
}

impl Service {
    fn b(&self) {
        println!("b");
    }
}

fn new() -> i32 { 1 }

struct Wrap<T>(T);

impl<T: Clone> Clone for Wrap<T> {
    fn clone(&self) -> Self { Wrap(self.0.clone()) }
}
`)
	entries := mustParse(t, src)
	newFn := functionNamed(t, entries, "new")
	if newFn.BodyHash != hashOf("{ 1 }") || newFn.BodyBytes != len("{ 1 }") {
		t.Fatalf("new = %s %d", newFn.BodyHash, newFn.BodyBytes)
	}
	a := methodNamed(t, entries, "a")
	b := methodNamed(t, entries, "b")
	if a.BodyHash != hashOf(`{ println!("a"); }`) {
		t.Fatalf("a body hash = %s", a.BodyHash)
	}
	if service := typeNamed(t, entries, "Service"); service.MethodsHash != cumulative(a.BodyHash, b.BodyHash) {
		t.Fatalf("Service methods hash = %s", service.MethodsHash)
	}
	clone := methodNamed(t, entries, "clone")
	if w := typeNamed(t, entries, "Wrap"); w.MethodsHash != cumulative(clone.BodyHash) || clone.Type.Name != "<Wrap as Clone>" {
		t.Fatalf("Wrap methods hash = %s, receiver %s", w.MethodsHash, clone.Type.Name)
	}

	changed := mustParse(t, bytes.Replace(src, []byte(`println!("a")`), []byte(`println!("z")`), 1))
	if methodNamed(t, changed, "a").BodyHash == a.BodyHash {
		t.Fatal("body change kept the hash")
	}
	if typeNamed(t, changed, "Service").MethodsHash == typeNamed(t, entries, "Service").MethodsHash {
		t.Fatal("method change kept the type hash")
	}
	if methodNamed(t, changed, "b").BodyHash != b.BodyHash {
		t.Fatal("unchanged body changed hash")
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func cumulative(hexHashes ...string) string {
	return cumulativeHash(hexHashes)
}

func mustParse(t *testing.T, src []byte) []lib.Entity {
	t.Helper()
	files := mustParseFiles(t, lib.File{Path: "src/lib.rs", Src: src})
	return files[0].Entities
}

func functionNamed(t *testing.T, entries []lib.Entity, name string) lib.FunctionEntry {
	t.Helper()
	for _, e := range entries {
		if fn, ok := e.(lib.FunctionEntry); ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("no function %s", name)
	return lib.FunctionEntry{}
}

func methodNamed(t *testing.T, entries []lib.Entity, name string) lib.MethodEntry {
	t.Helper()
	for _, e := range entries {
		if m, ok := e.(lib.MethodEntry); ok && m.Name == name {
			return m
		}
	}
	t.Fatalf("no method %s", name)
	return lib.MethodEntry{}
}

func typeNamed(t *testing.T, entries []lib.Entity, name string) lib.TypeEntry {
	t.Helper()
	for _, e := range entries {
		if typ, ok := e.(lib.TypeEntry); ok && typ.Name == name {
			return typ
		}
	}
	t.Fatalf("no type %s", name)
	return lib.TypeEntry{}
}
