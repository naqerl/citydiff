package diff

import (
	"testing"

	"betterdiff/lib"
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

	list := changes[2]
	if list.Left.(lib.MethodEntry).BodyHash != "before" || list.Right.(lib.MethodEntry).BodyHash != "after" {
		t.Fatalf("List sides = %+v %+v", list.Left, list.Right)
	}
	if list.Action != Modified || changes[0].Left != nil || changes[4].Right != nil {
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
