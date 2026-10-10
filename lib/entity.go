package lib

import "fmt"

// Entity is one language-agnostic declaration extracted from source.
type Entity interface {
	Kind() Kind
}

type Kind int

const (
	KindImport Kind = iota
	KindType
	KindVariable
	KindFunction
	KindMethod
)

func (k Kind) String() string {
	switch k {
	case KindImport:
		return "import"
	case KindType:
		return "type"
	case KindVariable:
		return "variable"
	case KindFunction:
		return "function"
	case KindMethod:
		return "method"
	default:
		return "unknown"
	}
}

type Field struct {
	Name string `json:"name"`
}

type Parameter struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

type ImportEntry struct {
	Path string `json:"path"`
}

func (e ImportEntry) Kind() Kind {
	return KindImport
}

type TypeEntry struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields,omitempty"`
	// MethodsHash is the hex SHA-256 of this type's method body hashes, in source order.
	MethodsHash string `json:"-"`
	Pos
}

func (e TypeEntry) Kind() Kind {
	return KindType
}

type VariableEntry struct {
	Name string `json:"name"`
	Pos
}

func (e VariableEntry) Kind() Kind {
	return KindVariable
}

// Call is one call in a function or method body, in source order.
// Expr is the callee as written, without arguments.
// Ref is set when that callee is a function or method declared in the same snapshot.
type Call struct {
	Expr string   `json:"expr"`
	Ref  *CallRef `json:"ref,omitempty"`
}

// CallRef names the declaration a call resolved to.
// Path is the snapshot path of the file that declares it.
// Recv is set for a method and is the receiver type without a pointer or type parameters.
type CallRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Recv string `json:"recv,omitempty"`
}

type FunctionEntry struct {
	Name       string      `json:"name"`
	Parameters []Parameter `json:"parameters,omitempty"`
	ReturnArgs []Parameter `json:"returnArgs,omitempty"`
	// Calls are the direct calls in the body, in source order.
	// Nested calls, both branches, and calls inside function literals are included.
	Calls []Call `json:"calls,omitempty"`
	// BodyHash is the hex SHA-256 of the function or method body source.
	BodyHash string `json:"-"`
	// BodyBytes is the length of that same body source.
	// A declaration with no body has length 0. The hash says whether the
	// body changed. The length says whether it grew or shrank.
	BodyBytes int `json:"-"`
	Pos
}

func (e FunctionEntry) Kind() Kind {
	return KindFunction
}

type MethodEntry struct {
	FunctionEntry
	Type *TypeEntry `json:"type,omitempty"`
}

func (e MethodEntry) Kind() Kind {
	return KindMethod
}

func (e MethodEntry) String() string {
	receiver := ""
	if e.Type != nil {
		receiver = e.Type.Name
	}
	return fmt.Sprintf("{Name:%s Parameters:%+v ReturnArgs:%+v Type:{Name:%s}}", e.Name, e.Parameters, e.ReturnArgs, receiver)
}

// Pos is where a declaration is written: the 1-based line and column of its
// name. Zero means unknown.
type Pos struct {
	Line   int `json:"-"`
	Column int `json:"-"`
}

// At returns entry with its position set. Imports have none.
func At(entry Entity, line, column int) Entity {
	p := Pos{Line: line, Column: column}
	switch e := entry.(type) {
	case TypeEntry:
		e.Pos = p
		return e
	case VariableEntry:
		e.Pos = p
		return e
	case FunctionEntry:
		e.Pos = p
		return e
	case MethodEntry:
		e.Pos = p
		return e
	}
	return entry
}

// PosOf is the position of an entity, zero when it has none.
func PosOf(entry Entity) Pos {
	switch e := entry.(type) {
	case TypeEntry:
		return e.Pos
	case VariableEntry:
		return e.Pos
	case FunctionEntry:
		return e.Pos
	case MethodEntry:
		return e.Pos
	}
	return Pos{}
}
