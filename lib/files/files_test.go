package files

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"betterdiff/lib"
)

func TestTreeSkipsVendorHiddenAndNonGo(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n")
	write("a.go", "package a\n")
	write("sub/b.go", "package b\n")
	write("vendor/c.go", "package c\n")
	write(".hidden/d.go", "package d\n")
	write("skip.txt", "nope\n")

	src, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, file.Path)
	}
	want := []string{"a.go", "go.mod", "sub/b.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}

	one, err := Tree(filepath.Join(root, "sub", "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := one.Next()
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != "b.go" || string(file.Src) != "package b\n" {
		t.Fatalf("file = %+v", file)
	}
	if _, err := one.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("second file err = %v", err)
	}
}

func TestMemStops(t *testing.T) {
	src := lib.Mem([]lib.File{{Path: "a.go", Src: []byte("package a")}})
	if _, err := src.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
