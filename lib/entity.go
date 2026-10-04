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
}

func (e TypeEntry) Kind() Kind {
	return KindType
}

type VariableEntry struct {
	Name string `json:"name"`
}

func (e VariableEntry) Kind() Kind {
	return KindVariable
}

type FunctionEntry struct {
	Name       string      `json:"name"`
	Parameters []Parameter `json:"parameters,omitempty"`
	ReturnArgs []Parameter `json:"returnArgs,omitempty"`
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
