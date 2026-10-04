package git

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"betterdiff/lib"
	"betterdiff/lib/diff"
	"betterdiff/lib/parser/go"
)

// Flashcard sample used by the Makefile. The range is
// 6ca8b06f "Unify loading indicator pattern across search inputs (#29)"
// to 3aff57d4 "Review generated flashcards in one form... (#98)".
// That change adds import barse/lib/dbx and edits Service.List.
const (
	barseFile  = "/home/user/Work/barse/service/flashcard/flashcard.go"
	barseLeft  = "6ca8b06f2209b8b57c209e4d9a448bdc0373ac87"
	barseRight = "3aff57d4730b6a615105d18cbe7eb611ae1ba4f7"
)

func TestBarseFlashcardRange(t *testing.T) {
	if _, err := os.Stat(barseFile); err != nil {
		t.Skip(err)
	}
	commitRange := barseLeft + ".." + barseRight
	left, right, err := Versions(barseFile, commitRange, golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) < 20 || len(right) != len(left)+1 {
		t.Fatalf("entity counts left %d right %d", len(left), len(right))
	}

	threeLeft, threeRight, err := Versions(barseFile, barseLeft+"..."+barseRight, golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, threeLeft) || !reflect.DeepEqual(right, threeRight) {
		t.Fatal("three-dot range differed from two-dot; left commit is an ancestor of right")
	}

	changes := diff.Entries(left, right)
	if len(changes) != 3 {
		t.Fatalf("got %d changes:\n%s", len(changes), formatChanges(changes))
	}

	imp, ok := changes[0].Right.(lib.ImportEntry)
	if changes[0].Action != diff.Added || !ok || imp.Path != "barse/lib/dbx" {
		t.Fatalf("first change = %s", changes[0])
	}

	leftType, rightType := mustTypeChange(t, changes[1], "Service")
	if !reflect.DeepEqual(leftType.Fields, rightType.Fields) {
		t.Fatalf("Service fields changed: %+v -> %+v", leftType.Fields, rightType.Fields)
	}
	if leftType.MethodsHash == "" || leftType.MethodsHash == rightType.MethodsHash {
		t.Fatal("Service methods hash did not change with List")
	}

	leftMethod, rightMethod := mustMethodChange(t, changes[2], "List")
	if leftMethod.BodyHash == "" || leftMethod.BodyHash == rightMethod.BodyHash {
		t.Fatal("List body hash did not change")
	}
	if !reflect.DeepEqual(leftMethod.Parameters, rightMethod.Parameters) || !reflect.DeepEqual(leftMethod.ReturnArgs, rightMethod.ReturnArgs) {
		t.Fatal("List signature changed")
	}

	leftCreate := mustMethod(t, left, "Create")
	rightCreate := mustMethod(t, right, "Create")
	if leftCreate.BodyHash != rightCreate.BodyHash {
		t.Fatal("Create body hash changed")
	}
}

func mustTypeChange(t *testing.T, change diff.Entry, name string) (lib.TypeEntry, lib.TypeEntry) {
	t.Helper()
	left, lok := change.Left.(lib.TypeEntry)
	right, rok := change.Right.(lib.TypeEntry)
	if change.Action != diff.Modified || !lok || !rok || left.Name != name || right.Name != name {
		t.Fatalf("want modified type %s, got %s", name, change)
	}
	return left, right
}

func mustMethodChange(t *testing.T, change diff.Entry, name string) (lib.MethodEntry, lib.MethodEntry) {
	t.Helper()
	left, lok := change.Left.(lib.MethodEntry)
	right, rok := change.Right.(lib.MethodEntry)
	if change.Action != diff.Modified || !lok || !rok || left.Name != name || right.Name != name {
		t.Fatalf("want modified method %s, got %s", name, change)
	}
	return left, right
}

func mustMethod(t *testing.T, entries []lib.Entity, name string) lib.MethodEntry {
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

func formatChanges(changes []diff.Entry) string {
	parts := make([]string, len(changes))
	for i, change := range changes {
		parts[i] = change.String()
	}
	return strings.Join(parts, "\n")
}
