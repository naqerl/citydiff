package main

import (
	"strings"
	"testing"

	"citydiff/lib"
)

func TestFormatEntityIncludesMethodCalls(t *testing.T) {
	method := lib.MethodEntry{
		FunctionEntry: lib.FunctionEntry{
			Name:     "M",
			BodyHash: "abc",
			Calls:    []lib.Call{{Expr: "N", Ref: &lib.CallRef{Path: "a.go", Name: "N"}}},
		},
		Type: &lib.TypeEntry{Name: "*T"},
	}
	got := formatEntity(method)
	for _, part := range []string{"BodyHash:abc", "Calls:", "Expr:N", "Type:{Name:*T}"} {
		if !strings.Contains(got, part) {
			t.Fatalf("format %q missing %s", got, part)
		}
	}
}

func TestUsageSaysFileOrDirectory(t *testing.T) {
	if !strings.HasPrefix(usageText(), "Usage: citydiff [-json | -scene | -view [-tour file]] -path file-or-directory [-range A..B]\n") {
		t.Fatalf("usage = %q", usageText())
	}
}
