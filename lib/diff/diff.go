// Package diff compares two slices of language-agnostic entities.
package diff

import (
	"fmt"
	"reflect"
	"strings"

	"citydiff/lib"
)

// Action is how one entity differs between the left and right slices.
type Action int

const (
	Added Action = iota
	Removed
	Modified
)

func (a Action) String() string {
	switch a {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Modified:
		return "modified"
	default:
		return "unknown"
	}
}

// Entry is one difference between two entity slices.
// Left is set for Removed and Modified. Right is set for Added and Modified.
// Edits is set for Modified and lists only the parts that differ.
type Entry struct {
	Action Action
	Left   lib.Entity
	Right  lib.Entity
	Edits  []Edit
}

func (e Entry) String() string {
	switch e.Action {
	case Added:
		return "added " + label(e.Right)
	case Removed:
		return "removed " + label(e.Left)
	case Modified:
		return "modified " + label(e.Right)
	default:
		return "unknown"
	}
}

// Entries returns the differences between left and right.
//
// An import matches on its path, a variable or type or function on its name,
// and a method on its declared receiver type plus its name. Pointer and value
// receivers of the same named type match, as do receivers that differ only
// by type parameters. The nth occurrence of an identity on the left pairs
// with the nth on the right, so a reorder of the same declarations produces
// no entry.
//
// A matched pair whose contents differ is Modified. Edits lists each part
// that differs, including BodyHash and MethodsHash. Unchanged entities are omitted.
// Additions and modifications follow the right slice. Removals follow, in
// left-slice order.
func Entries(left, right []lib.Entity) []Entry {
	pending := map[string][]lib.Entity{}
	var leftOrder []string
	for _, entry := range left {
		key := identity(entry)
		if _, ok := pending[key]; !ok {
			leftOrder = append(leftOrder, key)
		}
		pending[key] = append(pending[key], entry)
	}

	var out []Entry
	for _, entry := range right {
		key := identity(entry)
		queue := pending[key]
		if len(queue) == 0 {
			out = append(out, Entry{Action: Added, Right: entry})
			continue
		}
		old := queue[0]
		pending[key] = queue[1:]
		if !reflect.DeepEqual(old, entry) {
			out = append(out, modified(old, entry))
		}
	}
	for _, key := range leftOrder {
		for _, entry := range pending[key] {
			out = append(out, Entry{Action: Removed, Left: entry})
		}
	}
	return out
}

// Identity is the pairing key Entries uses.
func Identity(entry lib.Entity) string { return identity(entry) }

func identity(entry lib.Entity) string {
	switch entry := entry.(type) {
	case nil:
		return "nil"
	case lib.ImportEntry:
		return "import\x00" + entry.Path
	case lib.VariableEntry:
		return "variable\x00" + entry.Name
	case lib.TypeEntry:
		return "type\x00" + entry.Name
	case lib.FunctionEntry:
		return "function\x00" + entry.Name
	case lib.MethodEntry:
		return "method\x00" + receiverName(entry) + "\x00" + entry.Name
	default:
		return "other\x00" + entry.Kind().String() + "\x00" + fmt.Sprintf("%#v", entry)
	}
}

// receiverName is the declared type of a method receiver.
// "*Service", "Service", and "*Box[T]" all name one type.
func receiverName(entry lib.MethodEntry) string {
	if entry.Type == nil {
		return ""
	}
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(entry.Type.Name), "*"))
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return name
}

// Name is the declared name of the entity on this entry.
// Removed entries use the left side. Added and modified entries use the right side.
// A method name is the receiver as written, a dot, and the method name.
func (e Entry) Name() string {
	entry := e.Right
	if e.Action == Removed || entry == nil {
		entry = e.Left
	}
	return entityName(entry)
}

func entityName(entry lib.Entity) string {
	switch entry := entry.(type) {
	case lib.ImportEntry:
		return entry.Path
	case lib.VariableEntry:
		return entry.Name
	case lib.TypeEntry:
		return entry.Name
	case lib.FunctionEntry:
		return entry.Name
	case lib.MethodEntry:
		recv := ""
		if entry.Type != nil {
			recv = entry.Type.Name
		}
		return recv + "." + entry.Name
	default:
		return ""
	}
}

// FileChange is one difference between two snapshots.
// Files pair by path. Changes are the entity differences inside that file.
// An unchanged file is omitted.
type FileChange struct {
	Action  Action
	Path    string
	Changes []Entry
}

func (f FileChange) String() string {
	return f.Action.String() + " " + f.Path
}

// Files returns the differences between two snapshots.
//
// Paths pair in snapshot order: the nth file with a path on the left pairs
// with the nth on the right. Additions and modifications follow the right
// snapshot. Removals follow, in left-snapshot order. Declarations inside a
// paired file use the identity Entries already uses, so the same name in two
// files stays two declarations.
func Files(left, right []lib.ParsedFile) []FileChange {
	pending := map[string][]lib.ParsedFile{}
	var leftOrder []string
	for _, file := range left {
		if _, ok := pending[file.Path]; !ok {
			leftOrder = append(leftOrder, file.Path)
		}
		pending[file.Path] = append(pending[file.Path], file)
	}

	var out []FileChange
	for _, file := range right {
		queue := pending[file.Path]
		if len(queue) == 0 {
			out = append(out, FileChange{
				Action:  Added,
				Path:    file.Path,
				Changes: Entries(nil, file.Entities),
			})
			continue
		}
		old := queue[0]
		pending[file.Path] = queue[1:]
		changes := Entries(old.Entities, file.Entities)
		if len(changes) == 0 {
			continue
		}
		out = append(out, FileChange{Action: Modified, Path: file.Path, Changes: changes})
	}
	for _, path := range leftOrder {
		for _, file := range pending[path] {
			out = append(out, FileChange{
				Action:  Removed,
				Path:    path,
				Changes: Entries(file.Entities, nil),
			})
		}
	}
	return out
}

func label(entry lib.Entity) string {
	if entry == nil {
		return "?"
	}
	name := entityName(entry)
	if name == "" {
		return entry.Kind().String()
	}
	return entry.Kind().String() + " " + name
}
