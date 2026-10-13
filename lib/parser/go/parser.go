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

	"citydiff/lib"
	"citydiff/lib/parser/internal/par"
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
//
// Parsing splits into three phases and only two of them are independent: read
// each file into a draft, build the index, fill the calls. The first and the
// third run across par.Workers, the index build stays between them because
// every lookup below reads it. See resolve for the barrier and for why the
// result is applied after the workers join.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}

	// Read the stream first. A file is the unit of the first phase, and reading
	// in stream order is what fixes the order of the drafts, and so of the
	// output. A go.mod yields a module root and no file of its own.
	var mods []moduleRoot
	var files []lib.File
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
		files = append(files, file)
	}

	// One tree-sitter parser per worker: a *tree_sitter.Parser is not safe for
	// concurrent use, and it is cheap to give each worker its own.
	parsers, err := newParsers(max(1, min(par.Workers(), len(files))))
	if err != nil {
		return nil, err
	}
	defer par.CloseAll(parsers)

	// Phase 1: parse and walk every file. The files share nothing but the
	// read-only source bytes, so each worker writes only its own draft and its
	// own tree. Nothing here keeps a tree: the walk reads all it needs and the
	// tree is closed at once, so peak memory does not hold the whole set.
	drafts := make([]*draft, len(files))
	errs := make([]error, len(files))
	par.Do(len(files), parsers, func(parser *tree_sitter.Parser, i int) {
		file := files[i]
		tree := parser.Parse(file.Src, nil)
		if tree == nil {
			errs[i] = fmt.Errorf("parse %s returned no tree", file.Path)
			return
		}
		pkg, imports, entries, calls := walk(file.Src, tree.RootNode())
		tree.Close()
		assignMethodHashes(entries)
		drafts[i] = &draft{
			path:     file.Path,
			dir:      path.Dir(file.Path),
			pkg:      pkg,
			imports:  imports,
			entities: entries,
			calls:    calls,
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	assignImportPaths(drafts, mods)
	resolve(drafts)

	out := make([]lib.ParsedFile, len(drafts))
	for i, d := range drafts {
		out[i] = lib.ParsedFile{
			Path:       d.path,
			Package:    d.pkg,
			ImportPath: d.importPath,
			Module:     d.module,
			Entities:   d.entities,
		}
	}
	return out, nil
}

var _ lib.Parser = Parser{}

// newParsers makes n parsers with the Go grammar, one per worker.
func newParsers(n int) ([]*tree_sitter.Parser, error) {
	out := make([]*tree_sitter.Parser, n)
	for i := range out {
		p := tree_sitter.NewParser()
		if err := p.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
			p.Close()
			par.CloseAll(out[:i])
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

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
				entries, calls = grow(entries, calls, at(entry, &n), nil)
			}
		case "type_declaration":
			spec := typeSpec(&n)
			if spec == nil {
				continue
			}
			entries, calls = grow(entries, calls, at(parseTypeDeclaration(src, spec), spec), nil)
		case "method_declaration":
			entry, body := parseMethodDeclaration(src, &n)
			entries, calls = grow(entries, calls, at(entry, &n), body)
		case "function_declaration":
			entry, body := parseFunctionDeclaration(src, &n)
			entries, calls = grow(entries, calls, at(entry, &n), body)
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

// typeSpec is the first spec of a type declaration that names a type. In a
// grouped declaration a comment can come before it, and a comment has no name.
func typeSpec(typeDeclaration *tree_sitter.Node) *tree_sitter.Node {
	for i := uint(0); i < typeDeclaration.NamedChildCount(); i++ {
		if spec := typeDeclaration.NamedChild(i); spec.ChildByFieldName("name") != nil {
			return spec
		}
	}
	return nil
}

func parseTypeDeclaration(src []byte, typeSpec *tree_sitter.Node) lib.TypeEntry {
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
	bodyHash, bodyBytes := hashBody(src, methodDeclaration)
	entry := lib.MethodEntry{
		FunctionEntry: lib.FunctionEntry{
			Name:       name.Utf8Text(src),
			Parameters: parameters,
			ReturnArgs: parseReturnArgs(src, returns),
			BodyHash:   bodyHash,
			BodyBytes:  bodyBytes,
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
	bodyHash, bodyBytes := hashBody(src, functionDeclaration)
	entry := lib.FunctionEntry{
		Name:       name.Utf8Text(src),
		Parameters: parameters,
		ReturnArgs: parseReturnArgs(src, returns),
		BodyHash:   bodyHash,
		BodyBytes:  bodyBytes,
	}
	return entry, collectCalls(src, functionDeclaration.ChildByFieldName("body"), funcScope("", "", joinParams(parameters, namedResults(src, returns))))
}

// hashBody is the hex SHA-256 of the function or method body source, and the
// length of those bytes. A declaration with no body has an empty hash and length 0.
func hashBody(src []byte, decl *tree_sitter.Node) (string, int) {
	body := decl.ChildByFieldName("body")
	if body == nil {
		return "", 0
	}
	start, end := body.StartByte(), body.EndByte()
	if end < start || int(end) > len(src) {
		return "", 0
	}
	sum := sha256.Sum256(src[start:end])
	return hex.EncodeToString(sum[:]), int(end - start)
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

// at stamps entry with the position of n's name, or of n.
func at(entry lib.Entity, n *tree_sitter.Node) lib.Entity {
	if name := n.ChildByFieldName("name"); name != nil {
		n = name
	}
	p := n.StartPosition()
	return lib.At(entry, int(p.Row)+1, int(p.Column)+1)
}
