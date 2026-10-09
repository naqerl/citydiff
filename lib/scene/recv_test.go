package scene

import (
	"testing"

	"citydiff/lib"
)

// A call into a trait impl or a conformance extension names its owner
// <T as P>; the step must name T, the receiver the method's building has,
// or the viewer and tour paths cannot find the target.
func TestCallStepNamesTheTypeOfAConformanceOwner(t *testing.T) {
	method := lib.MethodEntry{FunctionEntry: lib.FunctionEntry{Name: "describe", BodyHash: "aa"}, Type: &lib.TypeEntry{Name: "<S as P>"}}
	caller := lib.FunctionEntry{Name: "use", BodyHash: "bb", Calls: []lib.Call{{Expr: "s.describe", Ref: &lib.CallRef{Path: "A/a.swift", Name: "describe", Recv: "<S as P>"}}}}
	sc := Build(nil, []lib.ParsedFile{{Path: "A/a.swift", Package: "A", ImportPath: "A", Entities: []lib.Entity{lib.TypeEntry{Name: "S"}, method, caller}}})
	var recv, target string
	for _, p := range sc.Packages {
		for _, e := range p.Entities {
			if e.Name == "use" && len(e.Calls) == 1 {
				target = e.Calls[0].Recv
			}
			if e.Name == "describe" {
				recv = e.Recv
			}
		}
	}
	if recv != "S" || target != "S" {
		t.Fatalf("entity recv %q, call recv %q", recv, target)
	}
}
