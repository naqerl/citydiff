// Package golang parses Go source into language-agnostic entities.
// The directory is parser/go; the package cannot be named go because that is a keyword.
package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"betterdiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// Parser parses Go source.
type Parser struct{}

// New returns a Go parser.
func New() Parser { return Parser{} }

// Parse implements lib.Parser.
// Every file is collected before calls are resolved, so a declaration in the
// last file fills a call in the first file. Calls that do not resolve are kept.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		return nil, err
	}

	var mods []moduleRoot
	var drafts []*draft
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		file.Path = path.Clean(filepath.ToSlash(file.Path))
		base := path.Base(file.Path)
		if base == "go.mod" {
			if module := modulePath(file.Src); module != "" {
				mods = append(mods, moduleRoot{dir: path.Dir(file.Path), path: module})
			}
			continue
		}
		if !strings.HasSuffix(base, ".go") {
			return nil, fmt.Errorf("%s is not a Go file", file.Path)
		}
		tree := parser.Parse(file.Src, nil)
		if tree == nil {
			return nil, fmt.Errorf("parse %s returned no tree", file.Path)
		}
		pkg, imports, entries, calls := walk(file.Src, tree.RootNode())
		tree.Close()
		assignMethodHashes(entries)
		drafts = append(drafts, &draft{
			path:     file.Path,
			dir:      path.Dir(file.Path),
			pkg:      pkg,
			imports:  imports,
			entities: entries,
			calls:    calls,
		})
	}
	assignImportPaths(drafts, mods)
	resolve(drafts)

	out := make([]lib.ParsedFile, len(drafts))
	for i, d := range drafts {
		out[i] = lib.ParsedFile{Path: d.path, Entities: d.entities}
	}
	return out, nil
}

var _ lib.Parser = Parser{}

func walk(src []byte, root *tree_sitter.Node) (pkg string, imports []importUse, entries []lib.Entity, calls [][]pending) {
	cursor := root.Walk()
	defer cursor.Close()

	q := []tree_sitter.Node{*root}
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		switch n.Kind() {
		case "package_clause":
			if id := n.NamedChild(0); id != nil {
				pkg = id.Utf8Text(src)
			}
		case "import_declaration":
			specs := parseImports(src, &n)
			imports = append(imports, specs...)
			for _, spec := range specs {
				entries, calls = grow(entries, calls, lib.ImportEntry{Path: spec.path}, nil)
			}
		case "var_declaration":
			for _, entry := range parseVarDeclaration(src, &n) {
				entries, calls = grow(entries, calls, entry, nil)
			}
		case "type_declaration":
			entries, calls = grow(entries, calls, parseTypeDeclaration(src, &n), nil)
		case "method_declaration":
			entry, body := parseMethodDeclaration(src, &n)
			entries, calls = grow(entries, calls, entry, body)
		case "function_declaration":
			entry, body := parseFunctionDeclaration(src, &n)
			entries, calls = grow(entries, calls, entry, body)
		default:
			for _, c := range n.NamedChildren(cursor) {
				q = append(q, c)
			}
		}
	}
	return pkg, imports, entries, calls
}

func grow(entries []lib.Entity, calls [][]pending, entry lib.Entity, body []pending) ([]lib.Entity, [][]pending) {
	if len(body) == 0 {
		body = nil
	}
	return append(entries, entry), append(calls, body)
}

// parseImports reads one import declaration. Aliases are kept for call
// resolution. The import entity still stores only the path.
func parseImports(src []byte, importDeclaration *tree_sitter.Node) []importUse {
	node := importDeclaration.NamedChild(0)
	if node == nil {
		return nil
	}
	var specs []*tree_sitter.Node
	if node.Kind() == "import_spec" {
		specs = []*tree_sitter.Node{node}
	} else {
		specs = make([]*tree_sitter.Node, 0, node.NamedChildCount())
		for i := uint(0); i < node.NamedChildCount(); i++ {
			specs = append(specs, node.NamedChild(i))
		}
	}
	out := make([]importUse, 0, len(specs))
	for _, spec := range specs {
		pathNode := spec.ChildByFieldName("path")
		if pathNode == nil {
			continue
		}
		use := importUse{path: unquoteImport(pathNode.Utf8Text(src))}
		if name := spec.ChildByFieldName("name"); name != nil {
			switch name.Kind() {
			case "dot":
				use.dot = true
			case "blank_identifier":
				use.blank = true
			default:
				use.alias = name.Utf8Text(src)
			}
		}
		out = append(out, use)
	}
	return out
}

func unquoteImport(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
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

func parseMethodDeclaration(src []byte, methodDeclaration *tree_sitter.Node) (lib.MethodEntry, []pending) {
	ident, typeText := receiverInfo(src, methodDeclaration)
	name := methodDeclaration.ChildByFieldName("name")
	params := methodDeclaration.ChildByFieldName("parameters")
	returns := methodDeclaration.ChildByFieldName("result")
	parameters := parseParameters(src, params)
	entry := lib.MethodEntry{
		FunctionEntry: lib.FunctionEntry{
			Name:       name.Utf8Text(src),
			Parameters: parameters,
			ReturnArgs: parseReturnArgs(src, returns),
			BodyHash:   hashBody(src, methodDeclaration),
		},
		Type: &lib.TypeEntry{Name: typeText},
	}
	return entry, collectCalls(src, methodDeclaration.ChildByFieldName("body"), funcScope(ident, typeText, joinParams(parameters, namedResults(src, returns))))
}

func parseFunctionDeclaration(src []byte, functionDeclaration *tree_sitter.Node) (lib.FunctionEntry, []pending) {
	name := functionDeclaration.ChildByFieldName("name")
	params := functionDeclaration.ChildByFieldName("parameters")
	returns := functionDeclaration.ChildByFieldName("result")
	parameters := parseParameters(src, params)
	entry := lib.FunctionEntry{
		Name:       name.Utf8Text(src),
		Parameters: parameters,
		ReturnArgs: parseReturnArgs(src, returns),
		BodyHash:   hashBody(src, functionDeclaration),
	}
	return entry, collectCalls(src, functionDeclaration.ChildByFieldName("body"), funcScope("", "", joinParams(parameters, namedResults(src, returns))))
}

// hashBody is the hex SHA-256 of the function or method body source.
// A declaration with no body has an empty hash.
func hashBody(src []byte, decl *tree_sitter.Node) string {
	body := decl.ChildByFieldName("body")
	if body == nil {
		return ""
	}
	start, end := body.StartByte(), body.EndByte()
	if end < start || int(end) > len(src) {
		return ""
	}
	sum := sha256.Sum256(src[start:end])
	return hex.EncodeToString(sum[:])
}

// assignMethodHashes sets each type's MethodsHash from its method body hashes.
// Pointer and value receivers share the declared type name. Order follows source order.
func assignMethodHashes(entries []lib.Entity) {
	byType := map[string][]string{}
	for _, entry := range entries {
		method, ok := entry.(lib.MethodEntry)
		if !ok || method.Type == nil || method.BodyHash == "" {
			continue
		}
		name := receiverTypeName(method.Type.Name)
		byType[name] = append(byType[name], method.BodyHash)
	}
	for i, entry := range entries {
		typ, ok := entry.(lib.TypeEntry)
		if !ok {
			continue
		}
		parts := byType[typ.Name]
		if len(parts) == 0 {
			continue
		}
		typ.MethodsHash = cumulativeHash(parts)
		entries[i] = typ
	}
}

func receiverTypeName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "*"))
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return name
}

// cumulativeHash is the hex SHA-256 of the method body hashes in source order.
func cumulativeHash(hexHashes []string) string {
	h := sha256.New()
	for _, hexHash := range hexHashes {
		raw, err := hex.DecodeString(hexHash)
		if err != nil {
			continue
		}
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
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

// namedResults is the named result variables. A bare result type has none.
func namedResults(src []byte, result *tree_sitter.Node) []lib.Parameter {
	if result == nil || result.Kind() != "parameter_list" {
		return nil
	}
	return parseParameters(src, result)
}

func joinParams(params, results []lib.Parameter) []lib.Parameter {
	if len(results) == 0 {
		return params
	}
	out := make([]lib.Parameter, 0, len(params)+len(results))
	return append(append(out, params...), results...)
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
