package diff

import (
	"fmt"
	"reflect"

	"betterdiff/lib"
)

// Edit is one part of a modified entity that differs between the two sides.
// Field names that part: fields, parameters, returnArgs, receiver, bodyHash, bodyBytes, calls, or methodsHash.
// Left and Right are the part before and after.
// bodyHash and methodsHash values are hex SHA-256 digests of source.
type Edit struct {
	Field string `json:"field"`
	Left  any    `json:"left"`
	Right any    `json:"right"`
}

func (e Edit) String() string {
	return fmt.Sprintf("%s: %+v => %+v", e.Field, e.Left, e.Right)
}

func modified(left, right lib.Entity) Entry {
	found := editsBetween(left, right)
	if len(found) == 0 {
		found = []Edit{{Field: "entry", Left: left, Right: right}}
	}
	return Entry{Action: Modified, Left: left, Right: right, Edits: found}
}

func editsBetween(left, right lib.Entity) []Edit {
	switch left := left.(type) {
	case lib.TypeEntry:
		right, ok := right.(lib.TypeEntry)
		if !ok {
			return nil
		}
		return typeEdits(left, right)
	case lib.FunctionEntry:
		right, ok := right.(lib.FunctionEntry)
		if !ok {
			return nil
		}
		return functionEdits(left, right)
	case lib.MethodEntry:
		right, ok := right.(lib.MethodEntry)
		if !ok {
			return nil
		}
		return methodEdits(left, right)
	default:
		return nil
	}
}

func typeEdits(left, right lib.TypeEntry) []Edit {
	var edits []Edit
	if !reflect.DeepEqual(left.Fields, right.Fields) {
		edits = append(edits, Edit{Field: "fields", Left: left.Fields, Right: right.Fields})
	}
	if left.MethodsHash != right.MethodsHash {
		edits = append(edits, Edit{Field: "methodsHash", Left: left.MethodsHash, Right: right.MethodsHash})
	}
	return edits
}

func functionEdits(left, right lib.FunctionEntry) []Edit {
	var edits []Edit
	if !reflect.DeepEqual(left.Parameters, right.Parameters) {
		edits = append(edits, Edit{Field: "parameters", Left: left.Parameters, Right: right.Parameters})
	}
	if !reflect.DeepEqual(left.ReturnArgs, right.ReturnArgs) {
		edits = append(edits, Edit{Field: "returnArgs", Left: left.ReturnArgs, Right: right.ReturnArgs})
	}
	if left.BodyHash != right.BodyHash {
		edits = append(edits, Edit{Field: "bodyHash", Left: left.BodyHash, Right: right.BodyHash})
	}
	if left.BodyBytes != right.BodyBytes {
		edits = append(edits, Edit{Field: "bodyBytes", Left: left.BodyBytes, Right: right.BodyBytes})
	}
	if !reflect.DeepEqual(left.Calls, right.Calls) {
		edits = append(edits, Edit{Field: "calls", Left: left.Calls, Right: right.Calls})
	}
	return edits
}

// methodEdits follows declaration order: receiver, parameters, results, body.
func methodEdits(left, right lib.MethodEntry) []Edit {
	var edits []Edit
	leftRecv, rightRecv := receiverText(left), receiverText(right)
	if leftRecv != rightRecv {
		edits = append(edits, Edit{Field: "receiver", Left: leftRecv, Right: rightRecv})
	}
	return append(edits, functionEdits(left.FunctionEntry, right.FunctionEntry)...)
}

func receiverText(entry lib.MethodEntry) string {
	if entry.Type == nil {
		return ""
	}
	return entry.Type.Name
}
