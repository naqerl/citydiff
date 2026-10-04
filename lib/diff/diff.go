// Package diff compares two slices of language-agnostic entities.
package diff

import (
	"fmt"
	"reflect"
	"strings"

	"betterdiff/lib"
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
type Entry struct {
	Action Action
	Left   lib.Entity
	Right  lib.Entity
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
// A matched pair whose contents differ is Modified. BodyHash and MethodsHash
// are part of that comparison. Unchanged entities are omitted.
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
			out = append(out, Entry{Action: Modified, Left: old, Right: entry})
		}
	}
	for _, key := range leftOrder {
		for _, entry := range pending[key] {
			out = append(out, Entry{Action: Removed, Left: entry})
		}
	}
	return out
}

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

func label(entry lib.Entity) string {
	switch entry := entry.(type) {
	case nil:
		return "?"
	case lib.ImportEntry:
		return "import " + entry.Path
	case lib.VariableEntry:
		return "variable " + entry.Name
	case lib.TypeEntry:
		return "type " + entry.Name
	case lib.FunctionEntry:
		return "function " + entry.Name
	case lib.MethodEntry:
		recv := ""
		if entry.Type != nil {
			recv = entry.Type.Name
		}
		return "method " + recv + "." + entry.Name
	default:
		return entry.Kind().String()
	}
}
