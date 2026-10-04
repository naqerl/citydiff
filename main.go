package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func main() {
	if err := test(); err != nil {
		log.Fatal("failed with", err)
	}
}

func test() error {
	code, err := os.ReadFile("/home/user/Work/barse/service/flashcard/flashcard.go")
	if err != nil {
		return err
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return err
	}

	tree := parser.Parse(code, nil)
	if tree == nil {
		return fmt.Errorf("parse returned no tree")
	}
	defer tree.Close()

	walk(code, tree.RootNode())

	return nil
}

func walk(src []byte, root *tree_sitter.Node) {
	cursor := root.Walk()
	defer cursor.Close()

	q := []tree_sitter.Node{*root}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		var entries []Entry
		switch n.Kind() {
		case "package_clause":
			slog.Info("package", "text", n.Utf8Text(src))
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
		for _, entry := range entries {
			slog.Info("entry", "kind", entry.Kind(), "data", entry)
		}
	}

	slog.Info("walked")
}

// FIXME: Process aliased imports that currently are trimmed
func parseImportDeclaration(src []byte, importDeclaration *tree_sitter.Node) []ImportEntry {
	importSpecList := importDeclaration.NamedChild(0)
	entries := make([]ImportEntry, 0, importSpecList.NamedChildCount())
	for i := uint(0); i < importSpecList.NamedChildCount(); i++ {
		importSpec := importSpecList.NamedChild(i)
		path := importSpec.ChildByFieldName("path").Utf8Text(src)
		// trim double quotes
		path = path[1 : len(path)-1]
		entries = append(entries, ImportEntry{Path: path})
	}
	return entries
}

func parseVarDeclaration(src []byte, varDeclaration *tree_sitter.Node) []VariableEntry {
	specs := namedSpecs(varDeclaration.NamedChild(0), "var_spec")
	entries := make([]VariableEntry, 0, len(specs))
	for _, spec := range specs {
		for _, name := range fieldTexts(src, spec, "name") {
			entries = append(entries, VariableEntry{Name: name})
		}
	}
	return entries
}

func parseTypeDeclaration(src []byte, typeDeclaration *tree_sitter.Node) TypeEntry {
	typeSpec := typeDeclaration.NamedChild(0)
	entry := TypeEntry{Name: typeSpec.ChildByFieldName("name").Utf8Text(src)}
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
			entry.Fields = append(entry.Fields, Field{Name: name})
		}
	}
	return entry
}

func parseMethodDeclaration(src []byte, methodDeclaration *tree_sitter.Node) MethodEntry {
	receiver := methodDeclaration.ChildByFieldName("receiver").NamedChild(0).ChildByFieldName("type")
	name := methodDeclaration.ChildByFieldName("name")
	params := methodDeclaration.ChildByFieldName("parameters")
	returns := methodDeclaration.ChildByFieldName("result")
	return MethodEntry{
		FunctionEntry: FunctionEntry{
			Name:       name.Utf8Text(src),
			Parameters: parseParameters(src, params),
			ReturnArgs: parseReturnArgs(src, returns),
		},
		Type: &TypeEntry{Name: receiver.Utf8Text(src)},
	}
}

func parseFunctionDeclaration(src []byte, functionDeclaration *tree_sitter.Node) FunctionEntry {
	name := functionDeclaration.ChildByFieldName("name")
	params := functionDeclaration.ChildByFieldName("parameters")
	returns := functionDeclaration.ChildByFieldName("result")
	return FunctionEntry{
		Name:       name.Utf8Text(src),
		Parameters: parseParameters(src, params),
		ReturnArgs: parseReturnArgs(src, returns),
	}
}

func parseReturnArgs(src []byte, result *tree_sitter.Node) []Parameter {
	if result == nil {
		return nil
	}
	if result.Kind() == "parameter_list" {
		return parseParameters(src, result)
	}
	return []Parameter{{Type: result.Utf8Text(src)}}
}

func parseParameters(src []byte, list *tree_sitter.Node) []Parameter {
	if list == nil {
		return nil
	}
	var params []Parameter
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
				params = append(params, Parameter{Type: typeText})
				continue
			}
			for _, name := range names {
				params = append(params, Parameter{Name: name, Type: typeText})
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
