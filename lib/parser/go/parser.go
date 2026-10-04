// Package golang parses Go source into language-agnostic entities.
// The directory is parser/go; the package cannot be named go because that is a keyword.
package golang

import (
	"fmt"

	"betterdiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// Parser parses Go source.
type Parser struct{}

// New returns a Go parser.
func New() Parser { return Parser{} }

// Parse implements lib.Parser.
func (Parser) Parse(src []byte) ([]lib.Entity, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return nil, err
	}

	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse returned no tree")
	}
	defer tree.Close()

	return walk(src, tree.RootNode()), nil
}

var _ lib.Parser = Parser{}

func walk(src []byte, root *tree_sitter.Node) []lib.Entity {
	cursor := root.Walk()
	defer cursor.Close()

	entries := make([]lib.Entity, 0)
	q := []tree_sitter.Node{*root}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		switch n.Kind() {
		case "package_clause":
			// Package name is not an entity.
		case "import_declaration":
			for _, e := range parseImportDeclaration(src, &n) {
				entries = append(entries, e)
			}
		case "var_declaration":
			for _, e := range parseVarDeclaration(src, &n) {
				entries = append(entries, e)
			}
		case "type_declaration":
			entries = append(entries, parseTypeDeclaration(src, &n))
		case "method_declaration":
			entries = append(entries, parseMethodDeclaration(src, &n))
		case "function_declaration":
			entries = append(entries, parseFunctionDeclaration(src, &n))
		default:
			for _, c := range n.NamedChildren(cursor) {
				q = append(q, c)
			}
		}
	}
	return entries
}

// FIXME: Process aliased imports that currently are trimmed
func parseImportDeclaration(src []byte, importDeclaration *tree_sitter.Node) []lib.ImportEntry {
	importSpecList := importDeclaration.NamedChild(0)
	entries := make([]lib.ImportEntry, 0, importSpecList.NamedChildCount())
	for i := uint(0); i < importSpecList.NamedChildCount(); i++ {
		importSpec := importSpecList.NamedChild(i)
		path := importSpec.ChildByFieldName("path").Utf8Text(src)
		// trim double quotes
		path = path[1 : len(path)-1]
		entries = append(entries, lib.ImportEntry{Path: path})
	}
	return entries
}

func parseVarDeclaration(src []byte, varDeclaration *tree_sitter.Node) []lib.VariableEntry {
	specs := namedSpecs(varDeclaration.NamedChild(0), "var_spec")
	entries := make([]lib.VariableEntry, 0, len(specs))
	for _, spec := range specs {
		for _, name := range fieldTexts(src, spec, "name") {
			entries = append(entries, lib.VariableEntry{Name: name})
		}
	}
	return entries
}

func parseTypeDeclaration(src []byte, typeDeclaration *tree_sitter.Node) lib.TypeEntry {
	typeSpec := typeDeclaration.NamedChild(0)
	entry := lib.TypeEntry{Name: typeSpec.ChildByFieldName("name").Utf8Text(src)}
	typ := typeSpec.ChildByFieldName("type")
	if typ == nil || typ.Kind() != "struct_type" {
		return entry
	}
	list := typ.NamedChild(0)
	for i := uint(0); i < list.NamedChildCount(); i++ {
		field := list.NamedChild(i)
		if field.Kind() != "field_declaration" {
			continue
		}
		for _, name := range fieldTexts(src, field, "name") {
			entry.Fields = append(entry.Fields, lib.Field{Name: name})
		}
	}
	return entry
}

func parseMethodDeclaration(src []byte, methodDeclaration *tree_sitter.Node) lib.MethodEntry {
	receiver := methodDeclaration.ChildByFieldName("receiver").NamedChild(0).ChildByFieldName("type")
	name := methodDeclaration.ChildByFieldName("name")
	params := methodDeclaration.ChildByFieldName("parameters")
	returns := methodDeclaration.ChildByFieldName("result")
	return lib.MethodEntry{
		FunctionEntry: lib.FunctionEntry{
			Name:       name.Utf8Text(src),
			Parameters: parseParameters(src, params),
			ReturnArgs: parseReturnArgs(src, returns),
		},
		Type: &lib.TypeEntry{Name: receiver.Utf8Text(src)},
	}
}

func parseFunctionDeclaration(src []byte, functionDeclaration *tree_sitter.Node) lib.FunctionEntry {
	name := functionDeclaration.ChildByFieldName("name")
	params := functionDeclaration.ChildByFieldName("parameters")
	returns := functionDeclaration.ChildByFieldName("result")
	return lib.FunctionEntry{
		Name:       name.Utf8Text(src),
		Parameters: parseParameters(src, params),
		ReturnArgs: parseReturnArgs(src, returns),
	}
}

func parseReturnArgs(src []byte, result *tree_sitter.Node) []lib.Parameter {
	if result == nil {
		return nil
	}
	if result.Kind() == "parameter_list" {
		return parseParameters(src, result)
	}
	return []lib.Parameter{{Type: result.Utf8Text(src)}}
}

func parseParameters(src []byte, list *tree_sitter.Node) []lib.Parameter {
	if list == nil {
		return nil
	}
	var params []lib.Parameter
	for i := uint(0); i < list.NamedChildCount(); i++ {
		decl := list.NamedChild(i)
		switch decl.Kind() {
		case "parameter_declaration", "variadic_parameter_declaration":
			typeText := ""
			if typ := decl.ChildByFieldName("type"); typ != nil {
				typeText = typ.Utf8Text(src)
			}
			if decl.Kind() == "variadic_parameter_declaration" {
				typeText = "..." + typeText
			}
			names := fieldTexts(src, decl, "name")
			if len(names) == 0 {
				params = append(params, lib.Parameter{Type: typeText})
				continue
			}
			for _, name := range names {
				params = append(params, lib.Parameter{Name: name, Type: typeText})
			}
		}
	}
	return params
}

// namedSpecs returns spec nodes. A bare spec is returned as itself; a list
// node yields each child of the given kind.
func namedSpecs(node *tree_sitter.Node, kind string) []*tree_sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == kind {
		return []*tree_sitter.Node{node}
	}
	specs := make([]*tree_sitter.Node, 0, node.NamedChildCount())
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == kind {
			specs = append(specs, child)
		}
	}
	return specs
}

func fieldTexts(src []byte, node *tree_sitter.Node, field string) []string {
	cursor := node.Walk()
	defer cursor.Close()
	children := node.ChildrenByFieldName(field, cursor)
	texts := make([]string, 0, len(children))
	for _, child := range children {
		texts = append(texts, child.Utf8Text(src))
	}
	return texts
}
