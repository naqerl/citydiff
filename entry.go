package main

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

type ImportEntry struct {
	Path string
}

func (e ImportEntry) Kind() Kind {
	return KindImport
}

type TypeEntry struct {
	Name string
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
	Name string
	Parameters []Parameter
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
