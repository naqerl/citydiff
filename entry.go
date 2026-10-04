package main

import "fmt"

type Entry interface {
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
	Name string
}

type Parameter struct {
	Name string
	Type string
}

type ImportEntry struct {
	Path string
}

func (e ImportEntry) Kind() Kind {
	return KindImport
}

type TypeEntry struct {
	Name   string
	Fields []Field
}

func (e TypeEntry) Kind() Kind {
	return KindType
}

type VariableEntry struct {
	Name string
}

func (e VariableEntry) Kind() Kind {
	return KindVariable
}

type FunctionEntry struct {
	Name       string
	Parameters []Parameter
	ReturnArgs []Parameter
}

func (e FunctionEntry) Kind() Kind {
	return KindFunction
}

type MethodEntry struct {
	FunctionEntry
	Type *TypeEntry
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
