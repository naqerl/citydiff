package diff

import (
	"encoding/json"
	"strings"
	"testing"

	"citydiff/lib"
)

func TestEntriesAddsRemovesAndEdits(t *testing.T) {
	service := &lib.TypeEntry{Name: "*Service"}
	left := []lib.Entity{
		lib.ImportEntry{Path: "old.example"},
		lib.VariableEntry{Name: "ErrGone"},
		lib.TypeEntry{Name: "Service", Fields: []lib.Field{{Name: "pool"}}, MethodsHash: "same"},
		lib.FunctionEntry{Name: "CanModify", Parameters: []lib.Parameter{{Name: "u", Type: "User"}}},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "before", Parameters: []lib.Parameter{{Name: "page", Type: "int64"}}},
			Type:          service,
		},
		lib.FunctionEntry{Name: "OldHelper"},
	}
	right := []lib.Entity{
		lib.ImportEntry{Path: "new.example"},
		lib.VariableEntry{Name: "ErrGone"},
		lib.TypeEntry{Name: "Service", Fields: []lib.Field{{Name: "pool"}, {Name: "tx"}}, MethodsHash: "same"},
		lib.FunctionEntry{Name: "CanModify", Parameters: []lib.Parameter{{Name: "u", Type: "User"}}},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "after", Parameters: []lib.Parameter{{Name: "page", Type: "int64"}}},
			Type:          &lib.TypeEntry{Name: "Service"},
		},
		lib.FunctionEntry{Name: "NewHelper"},
	}

	changes := Entries(left, right)
	want := []string{
		"added import new.example",
		"modified type Service",
		"modified method Service.List",
		"added function NewHelper",
		"removed import old.example",
		"removed function OldHelper",
	}
	if len(changes) != len(want) {
		t.Fatalf("got %d changes: %v", len(changes), changes)
	}
	for i, change := range changes {
		if change.String() != want[i] {
			t.Fatalf("change %d = %s, want %s", i, change, want[i])
		}
	}

	serviceEdit := changes[1]
	if fields := editFields(serviceEdit); fields != "fields" {
		t.Fatalf("Service edits = %s", fields)
	}
	leftFields := serviceEdit.Edits[0].Left.([]lib.Field)
	rightFields := serviceEdit.Edits[0].Right.([]lib.Field)
	if len(leftFields) != 1 || leftFields[0].Name != "pool" || len(rightFields) != 2 || rightFields[1].Name != "tx" {
		t.Fatalf("fields edit = %s", serviceEdit.Edits[0])
	}

	list := changes[2]
	if list.Left.(lib.MethodEntry).BodyHash != "before" || list.Right.(lib.MethodEntry).BodyHash != "after" {
		t.Fatalf("List sides = %+v %+v", list.Left, list.Right)
	}
	if fields := editFields(list); fields != "receiver,bodyHash" {
		t.Fatalf("List edits = %s", fields)
	}
	if list.Edits[0].Left != "*Service" || list.Edits[0].Right != "Service" {
		t.Fatalf("receiver edit = %s", list.Edits[0])
	}
	if list.Edits[1].Left != "before" || list.Edits[1].Right != "after" {
		t.Fatalf("body edit = %s", list.Edits[1])
	}
	if list.Action != Modified || changes[0].Left != nil || changes[4].Right != nil || len(changes[0].Edits) != 0 {
		t.Fatal("added and removed entries should set only one side")
	}
}

func TestEntriesIgnoresReorderAndUnchanged(t *testing.T) {
	left := []lib.Entity{
		lib.FunctionEntry{Name: "A", BodyHash: "a"},
		lib.FunctionEntry{Name: "B", BodyHash: "b"},
	}
	right := []lib.Entity{
		lib.FunctionEntry{Name: "B", BodyHash: "b"},
		lib.FunctionEntry{Name: "A", BodyHash: "a"},
	}
	if changes := Entries(left, right); len(changes) != 0 {
		t.Fatalf("reorder produced %v", changes)
	}
	if changes := Entries(nil, nil); changes != nil {
		t.Fatalf("empty diff = %#v", changes)
	}
}

func TestEntriesBodyHashAndMethodsHash(t *testing.T) {
	left := []lib.Entity{
		lib.TypeEntry{Name: "Service", MethodsHash: "aaa"},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "Create", BodyHash: "keep"},
			Type:          &lib.TypeEntry{Name: "*Service"},
		},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "one"},
			Type:          &lib.TypeEntry{Name: "*Service"},
		},
	}
	right := []lib.Entity{
		lib.TypeEntry{Name: "Service", MethodsHash: "bbb"},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "Create", BodyHash: "keep"},
			Type:          &lib.TypeEntry{Name: "*Service"},
		},
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "two", ReturnArgs: []lib.Parameter{{Type: "error"}}},
			Type:          &lib.TypeEntry{Name: "*Box[T]"},
		},
	}
	// Box is a different receiver, so List is removed and added rather than modified.
	// The Service case below covers a body change on the same receiver.

	changes := Entries(left, right)
	if len(changes) != 3 {
		t.Fatalf("got %v", changes)
	}
	if changes[0].String() != "modified type Service" {
		t.Fatal(changes[0])
	}
	if fields := editFields(changes[0]); fields != "methodsHash" {
		t.Fatalf("Service edits = %s", fields)
	}
	if changes[0].Edits[0].Left != "aaa" || changes[0].Edits[0].Right != "bbb" {
		t.Fatal(changes[0].Edits[0])
	}
	if changes[1].String() != "added method *Box[T].List" || changes[2].String() != "removed method *Service.List" {
		t.Fatalf("%s / %s", changes[1], changes[2])
	}

	sameReceiver := []lib.Entity{
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "one"},
			Type:          &lib.TypeEntry{Name: "*Service"},
		},
	}
	edited := []lib.Entity{
		lib.MethodEntry{
			FunctionEntry: lib.FunctionEntry{Name: "List", BodyHash: "two"},
			Type:          &lib.TypeEntry{Name: "Service"},
		},
	}
	changes = Entries(sameReceiver, edited)
	if len(changes) != 1 || changes[0].Action != Modified {
		t.Fatalf("pointer and value receivers did not match: %v", changes)
	}
	if fields := editFields(changes[0]); fields != "receiver,bodyHash" {
		t.Fatalf("edits = %s", fields)
	}
}

func TestEditsOmitUnchangedParts(t *testing.T) {
	left := []lib.Entity{
		lib.FunctionEntry{
			Name:       "CanModify",
			Parameters: []lib.Parameter{{Name: "u", Type: "User"}},
			ReturnArgs: []lib.Parameter{{Type: "bool"}},
			BodyHash:   "same",
		},
	}
	right := []lib.Entity{
		lib.FunctionEntry{
			Name:       "CanModify",
			Parameters: []lib.Parameter{{Name: "u", Type: "Account"}},
			ReturnArgs: []lib.Parameter{{Type: "bool"}, {Type: "error"}},
			BodyHash:   "same",
		},
	}
	changes := Entries(left, right)
	if len(changes) != 1 || changes[0].Name() != "CanModify" {
		t.Fatal(changes)
	}
	if fields := editFields(changes[0]); fields != "parameters,returnArgs" {
		t.Fatalf("edits = %s", fields)
	}
	params := changes[0].Edits[0]
	if params.String() != "parameters: [{Name:u Type:User}] => [{Name:u Type:Account}]" {
		t.Fatal(params)
	}
}

func TestEditJSON(t *testing.T) {
	edit := Edit{
		Field: "fields",
		Left:  []lib.Field{{Name: "pool"}},
		Right: []lib.Field{{Name: "pool"}, {Name: "tx"}},
	}
	got, err := json.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"field":"fields","left":[{"name":"pool"}],"right":[{"name":"pool"},{"name":"tx"}]}`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
}

func editFields(change Entry) string {
	names := make([]string, len(change.Edits))
	for i, edit := range change.Edits {
		names[i] = edit.Field
	}
	return strings.Join(names, ",")
}

func TestEntriesBodyBytes(t *testing.T) {
	left := []lib.Entity{lib.FunctionEntry{Name: "A", BodyHash: "a", BodyBytes: 10}}
	right := []lib.Entity{lib.FunctionEntry{Name: "A", BodyHash: "b", BodyBytes: 14}}
	changes := Entries(left, right)
	if len(changes) != 1 {
		t.Fatal(changes)
	}
	if fields := editFields(changes[0]); fields != "bodyHash,bodyBytes" {
		t.Fatalf("edits = %s", fields)
	}
	if changes[0].Edits[1].Left != 10 || changes[0].Edits[1].Right != 14 {
		t.Fatalf("size edit = %+v", changes[0].Edits[1])
	}
}

func TestEntriesCallEdit(t *testing.T) {
	left := []lib.Entity{lib.FunctionEntry{
		Name:     "A",
		BodyHash: "same",
		Calls:    []lib.Call{{Expr: "B"}},
	}}
	right := []lib.Entity{lib.FunctionEntry{
		Name:     "A",
		BodyHash: "same",
		Calls: []lib.Call{{
			Expr: "C",
			Ref:  &lib.CallRef{Path: "c.go", Name: "C"},
		}},
	}}
	changes := Entries(left, right)
	if len(changes) != 1 {
		t.Fatal(changes)
	}
	if fields := editFields(changes[0]); fields != "calls" {
		t.Fatalf("edits = %s", fields)
	}
}

func TestFilesPairByPath(t *testing.T) {
	left := []lib.ParsedFile{
		{Path: "a.go", Entities: []lib.Entity{lib.FunctionEntry{Name: "F", BodyHash: "1"}}},
		{Path: "b.go", Entities: []lib.Entity{lib.FunctionEntry{Name: "F", BodyHash: "1"}}},
	}
	right := []lib.ParsedFile{
		{Path: "a.go", Entities: []lib.Entity{lib.FunctionEntry{Name: "F", BodyHash: "1"}}},
		{Path: "b.go", Entities: []lib.Entity{lib.FunctionEntry{Name: "F", BodyHash: "2"}}},
		{Path: "c.go", Entities: []lib.Entity{lib.FunctionEntry{Name: "F"}}},
	}
	changes := Files(left, right)
	if len(changes) != 2 || changes[0].String() != "modified b.go" || changes[1].String() != "added c.go" {
		t.Fatalf("got %v", changes)
	}
	if changes[0].Changes[0].String() != "modified function F" {
		t.Fatal(changes[0].Changes)
	}
	if len(changes[1].Changes) != 1 || changes[1].Changes[0].String() != "added function F" {
		t.Fatal(changes[1].Changes)
	}
}

func TestEntriesPairsDuplicateNamesInOrder(t *testing.T) {
	left := []lib.Entity{
		lib.VariableEntry{Name: "x"},
		lib.VariableEntry{Name: "x"},
	}
	right := []lib.Entity{
		lib.VariableEntry{Name: "x"},
	}
	changes := Entries(left, right)
	if len(changes) != 1 || changes[0].String() != "removed variable x" {
		t.Fatalf("got %v", changes)
	}
}
