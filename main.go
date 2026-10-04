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
		switch n.Kind() {
		case "package_clause":
			slog.Info("package", "text", n.Utf8Text(src))
		case "import_declaration":
			parseImportDeclaration(src, &n)
		case "var_declaration":
			parseVarDeclaration(src, &n)
		case "type_declaration":
			parseTypeDeclaration(src, &n)
		case "method_declaration":
			parseMethodDeclaration(src, &n)
		case "function_declaration":
			parseFunctionDeclaration(src, &n)
		default:
			for _, c := range n.NamedChildren(cursor) {
				q = append(q, c)
			}
		}
	}

	slog.Info("walked")
}

// FIXME: Process aliased imports that currently are trimmed
func parseImportDeclaration(src []byte, importDeclaration *tree_sitter.Node) {
	importSpecList := importDeclaration.NamedChild(0)
	for i := uint(0); i < importSpecList.NamedChildCount(); i++ {
		importSpec := importSpecList.NamedChild(i)
		path := importSpec.NamedChild(0).Utf8Text(src)
		// trim double quotes
		path = path[1 : len(path)-2]
		slog.Info("import", "path", path)
	}
}

func parseVarDeclaration(src []byte, varDeclaration *tree_sitter.Node) {
	varSpecList := varDeclaration.NamedChild(0)
	varSpec := varSpecList.NamedChild(0)
	name := varSpec.ChildByFieldName("name")
	slog.Info("var", "name", name.Utf8Text(src))
}

func parseTypeDeclaration(src []byte, typeDeclaration *tree_sitter.Node) {
	typeSpec := typeDeclaration.NamedChild(0)
	name := typeSpec.ChildByFieldName("name")
	slog.Info("type", "name", name.Utf8Text(src))
}

func parseMethodDeclaration(src []byte, methodDeclaration *tree_sitter.Node) {
	receiver := methodDeclaration.ChildByFieldName("receiver").NamedChild(0).ChildByFieldName("type")
	name := methodDeclaration.ChildByFieldName("name")
	slog.Info("method", "receiver", receiver.Utf8Text(src), "name", name.Utf8Text(src))
}

func parseFunctionDeclaration(src []byte, functionDeclaration *tree_sitter.Node) {
	name := functionDeclaration.ChildByFieldName("name")
	slog.Info("function", "name", name.Utf8Text(src))
}
