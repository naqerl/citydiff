// Package javascript parses JavaScript source into language-agnostic entities.
package javascript

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
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// Parser parses JavaScript source: .js, .mjs, .cjs and .jsx.
type Parser struct{}

// New returns a JavaScript parser.
func New() Parser { return Parser{} }

// Extensions are the file suffixes the parser reads.
var Extensions = []string{".js", ".mjs", ".cjs", ".jsx"}

// IsSource reports whether base is a JavaScript file name.
func IsSource(base string) bool {
	for _, ext := range Extensions {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// binding is a name a file imports: from spec, the exported name ("" for the
// whole module: a namespace import or a plain require).
type binding struct {
	spec    string
	export  string
	require bool
}

// draft is one file between reading it and filling its calls.
type draft struct {
	path       string
	src        []byte
	tree       *tree_sitter.Tree
	importPath string
	pkg        string
	module     string
	entities   []lib.Entity
	metas      []meta
	// imports are the import entities' specifiers, in order, before they are
	// resolved to modules.
	specs    []string
	bindings map[string]binding
	// exports maps an exported name to the local name behind it; "default"
	// is the default export and module.exports. stars are export * sources.
	exports map[string]string
	reexp   map[string]binding
	stars   []string
	// targets are the resolved files of the specifiers.
	targets map[string]*draft
}

// meta is what call resolution needs about one entity. node is the function
// node (for the body and the parameters); owner is the class or object a
// method belongs to.
type meta struct {
	node   *tree_sitter.Node
	owner  string
	static bool
	// For a class: what it extends, as written.
	extends string
}

// Parse implements lib.Parser.
// Files are collected first; calls resolve once every file's exports are
// known. A package.json names the package its directory roots; a file's
// module is its directory under that root, as Go's are.
//
// Parsing splits into three phases and only two of them are independent: read
// each file into a draft, build the index, fill the calls. The first and the
// third run across par.Workers, the index build stays between them because
// every collect below reads it. See resolve for the barrier and for why the
// result is applied after the workers join.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}

	// Read the stream first. A file is the unit of the first phase, and reading
	// in stream order is what fixes the order of the drafts, and so of the
	// output. A package.json yields a manifest and no file of its own.
	var manifests []manifest
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
		if base == "package.json" {
			manifests = append(manifests, readManifest(path.Dir(file.Path), file.Src))
			continue
		}
		if !IsSource(base) {
			return nil, fmt.Errorf("%s is not a JavaScript file", file.Path)
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
	// own tree. A draft keeps its tree: the walker's meta nodes point into it and
	// collecting the calls is a second walk of it.
	drafts := make([]*draft, len(files))
	defer closeTrees(drafts)
	errs := make([]error, len(files))
	par.Do(len(files), parsers, func(parser *tree_sitter.Parser, i int) {
		file := files[i]
		tree := parser.Parse(file.Src, nil)
		if tree == nil {
			errs[i] = fmt.Errorf("parse %s returned no tree", file.Path)
			return
		}
		d := &draft{path: file.Path, src: file.Src, tree: tree, bindings: map[string]binding{}, exports: map[string]string{}, reexp: map[string]binding{}}
		w := walker{src: file.Src, d: d, owners: map[string]bool{}, objects: map[string]*tree_sitter.Node{}}
		w.program(tree.RootNode())
		assignMethodHashes(d.entities)
		drafts[i] = d
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	assignModules(drafts, manifests)
	resolve(drafts)

	out := make([]lib.ParsedFile, len(drafts))
	for i, d := range drafts {
		out[i] = lib.ParsedFile{Path: d.path, Package: d.pkg, ImportPath: d.importPath, Module: d.module, Entities: d.entities}
	}
	return out, nil
}

var _ lib.Parser = Parser{}

// newParsers makes n parsers with the JavaScript grammar, one per worker.
func newParsers(n int) ([]*tree_sitter.Parser, error) {
	out := make([]*tree_sitter.Parser, n)
	for i := range out {
		p := tree_sitter.NewParser()
		if err := p.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
			p.Close()
			par.CloseAll(out[:i])
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

// closeTrees closes the tree of every draft that still holds one. Phase 3 frees
// a tree as soon as its file's entities are collected; this catches the trees
// left behind when a parse fails.
func closeTrees(drafts []*draft) {
	for _, d := range drafts {
		if d != nil && d.tree != nil {
			d.tree.Close()
			d.tree = nil
		}
	}
}

type walker struct {
	src []byte
	d   *draft
	// owners are the names that got methods by assignment (X.m = function,
	// X.prototype.m = function); each becomes a type if it is not one.
	owners map[string]bool
	// objects are top-level object literals by name, for exported objects'
	// methods.
	objects map[string]*tree_sitter.Node
	// names are the top-level declarations, by name, with their position.
	names map[string]*tree_sitter.Node
	types map[string]bool
}

func (w *walker) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(w.src)
}

func (w *walker) add(entry lib.Entity, at *tree_sitter.Node, m meta) {
	if at != nil {
		p := at.StartPosition()
		entry = lib.At(entry, int(p.Row)+1, int(p.Column)+1)
	}
	w.d.entities = append(w.d.entities, entry)
	w.d.metas = append(w.d.metas, m)
}

func named(n *tree_sitter.Node) []*tree_sitter.Node {
	if n == nil {
		return nil
	}
	out := make([]*tree_sitter.Node, 0, n.NamedChildCount())
	for i := uint(0); i < n.NamedChildCount(); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

func isFunction(n *tree_sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "arrow_function", "function_expression", "function", "generator_function":
		return true
	}
	return false
}

func unparen(n *tree_sitter.Node) *tree_sitter.Node {
	for n != nil && n.Kind() == "parenthesized_expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	return n
}

// stringValue is the text of a string literal without its quotes.
func (w *walker) stringValue(n *tree_sitter.Node) (string, bool) {
	if n == nil || (n.Kind() != "string" && n.Kind() != "template_string") {
		return "", false
	}
	t := w.text(n)
	if len(t) < 2 || strings.Contains(t, "${") {
		return "", false
	}
	return t[1 : len(t)-1], true
}

// requireSpec is the specifier of require("x"), or false.
func (w *walker) requireSpec(n *tree_sitter.Node) (string, bool) {
	n = unparen(n)
	if n == nil || n.Kind() != "call_expression" {
		return "", false
	}
	fn := n.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "identifier" || w.text(fn) != "require" {
		return "", false
	}
	args := named(n.ChildByFieldName("arguments"))
	if len(args) != 1 {
		return "", false
	}
	return w.stringValue(args[0])
}

func (w *walker) addSpec(spec string) {
	for _, s := range w.d.specs {
		if s == spec {
			return
		}
	}
	w.d.specs = append(w.d.specs, spec)
	w.d.entities = append(w.d.entities, lib.ImportEntry{Path: spec})
	w.d.metas = append(w.d.metas, meta{})
}

func (w *walker) program(root *tree_sitter.Node) {
	w.names = map[string]*tree_sitter.Node{}
	w.types = map[string]bool{}
	for _, n := range named(root) {
		w.statement(n, false)
	}
	// A require anywhere (in a function, or called at once as
	// require("debug")("x")) still imports its module.
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if spec, ok := w.requireSpec(n); ok {
			w.addSpec(spec)
		}
		for _, c := range named(n) {
			visit(c)
		}
	}
	visit(root)
	// An object that got methods by assignment, or an exported object
	// literal with methods, is drawn as a type that owns them.
	for local := range w.objects {
		if w.exported(local) {
			w.objectMethods(local, w.objects[local])
		}
	}
	for owner := range w.owners {
		if w.types[owner] {
			continue
		}
		w.types[owner] = true
		w.add(lib.TypeEntry{Name: owner}, w.names[owner], meta{})
	}
}

func (w *walker) exported(local string) bool {
	for _, l := range w.d.exports {
		if l == local {
			return true
		}
	}
	return false
}

func (w *walker) statement(n *tree_sitter.Node, exported bool) {
	switch n.Kind() {
	case "import_statement":
		w.importStatement(n)
	case "export_statement":
		w.exportStatement(n)
	case "function_declaration", "generator_function_declaration":
		name := n.ChildByFieldName("name")
		w.function(w.text(name), name, n)
		w.declare(w.text(name), name, exported)
	case "class_declaration":
		name := n.ChildByFieldName("name")
		w.class(w.text(name), name, n)
		w.declare(w.text(name), name, exported)
	case "lexical_declaration", "variable_declaration":
		for _, decl := range named(n) {
			if decl.Kind() == "variable_declarator" {
				w.declarator(decl, exported)
			}
		}
	case "expression_statement":
		if e := unparen(n.NamedChild(0)); e != nil && e.Kind() == "assignment_expression" {
			w.assignment(e)
		}
	}
}

func (w *walker) declare(name string, at *tree_sitter.Node, exported bool) {
	if name == "" {
		return
	}
	w.names[name] = at
	if exported {
		w.d.exports[name] = name
	}
}

func (w *walker) importStatement(n *tree_sitter.Node) {
	spec, ok := w.stringValue(n.ChildByFieldName("source"))
	if !ok {
		return
	}
	w.addSpec(spec)
	for _, c := range named(n) {
		if c.Kind() != "import_clause" {
			continue
		}
		for _, part := range named(c) {
			switch part.Kind() {
			case "identifier":
				w.d.bindings[w.text(part)] = binding{spec: spec, export: "default"}
			case "namespace_import":
				if id := part.NamedChild(0); id != nil {
					w.d.bindings[w.text(id)] = binding{spec: spec}
				}
			case "named_imports":
				for _, s := range named(part) {
					if s.Kind() != "import_specifier" {
						continue
					}
					name := w.text(s.ChildByFieldName("name"))
					local := name
					if alias := s.ChildByFieldName("alias"); alias != nil {
						local = w.text(alias)
					}
					w.d.bindings[local] = binding{spec: spec, export: strings.Trim(name, `"'`)}
				}
			}
		}
	}
}

func (w *walker) exportStatement(n *tree_sitter.Node) {
	source := n.ChildByFieldName("source")
	spec, fromOther := w.stringValue(source)
	if fromOther {
		w.addSpec(spec)
	}
	isDefault := false
	for i := uint(0); i < n.ChildCount(); i++ {
		if c := n.Child(i); c != nil && c.Kind() == "default" {
			isDefault = true
		}
	}
	if decl := n.ChildByFieldName("declaration"); decl != nil {
		before := len(w.names)
		_ = before
		w.statement(decl, true)
		if isDefault {
			if name := decl.ChildByFieldName("name"); name != nil {
				w.d.exports["default"] = w.text(name)
			}
		}
		return
	}
	if value := unparen(n.ChildByFieldName("value")); value != nil && isDefault {
		switch {
		case value.Kind() == "identifier":
			w.d.exports["default"] = w.text(value)
		case isFunction(value):
			name := "default"
			if id := value.ChildByFieldName("name"); id != nil {
				name = w.text(id)
			}
			w.function(name, value, value)
			w.declare(name, value, false)
			w.d.exports["default"] = name
		case value.Kind() == "class":
			name := "default"
			if id := value.ChildByFieldName("name"); id != nil {
				name = w.text(id)
			}
			w.class(name, value, value)
			w.declare(name, value, false)
			w.d.exports["default"] = name
		}
		return
	}
	for _, c := range named(n) {
		switch c.Kind() {
		case "export_clause":
			for _, s := range named(c) {
				if s.Kind() != "export_specifier" {
					continue
				}
				name := strings.Trim(w.text(s.ChildByFieldName("name")), `"'`)
				alias := name
				if a := s.ChildByFieldName("alias"); a != nil {
					alias = strings.Trim(w.text(a), `"'`)
				}
				if fromOther {
					w.d.reexp[alias] = binding{spec: spec, export: name}
				} else {
					w.d.exports[alias] = name
				}
			}
		case "namespace_export":
			if fromOther {
				if id := c.NamedChild(0); id != nil {
					w.d.reexp[strings.Trim(w.text(id), `"'`)] = binding{spec: spec}
				}
			}
		}
	}
	if fromOther && !hasKind(n, "export_clause") && !hasKind(n, "namespace_export") {
		w.d.stars = append(w.d.stars, spec)
	}
}

func hasKind(n *tree_sitter.Node, kind string) bool {
	for _, c := range named(n) {
		if c.Kind() == kind {
			return true
		}
	}
	return false
}

// moduleExports is true for module.exports and exports.
func (w *walker) moduleExports(n *tree_sitter.Node) bool {
	n = unparen(n)
	if n == nil {
		return false
	}
	t := w.text(n)
	return t == "module.exports" || t == "exports"
}

// assignTarget unwinds a = b = c into the innermost value and whether
// module.exports (or exports) was one of the left sides.
func (w *walker) assignTarget(value *tree_sitter.Node) (*tree_sitter.Node, bool) {
	toExports := false
	value = unparen(value)
	for value != nil && value.Kind() == "assignment_expression" {
		if w.moduleExports(value.ChildByFieldName("left")) {
			toExports = true
		}
		value = unparen(value.ChildByFieldName("right"))
	}
	return value, toExports
}

func (w *walker) declarator(decl *tree_sitter.Node, exported bool) {
	nameNode := decl.ChildByFieldName("name")
	value, toExports := w.assignTarget(decl.ChildByFieldName("value"))
	// var f = require("./x").f is a named import.
	if value != nil && value.Kind() == "member_expression" && nameNode.Kind() == "identifier" {
		if spec, ok := w.requireSpec(value.ChildByFieldName("object")); ok {
			w.addSpec(spec)
			w.d.bindings[w.text(nameNode)] = binding{spec: spec, export: w.text(value.ChildByFieldName("property"))}
			return
		}
	}
	if spec, ok := w.requireSpec(value); ok {
		w.addSpec(spec)
		switch nameNode.Kind() {
		case "identifier":
			w.d.bindings[w.text(nameNode)] = binding{spec: spec, require: true}
		case "object_pattern":
			for _, p := range named(nameNode) {
				switch p.Kind() {
				case "shorthand_property_identifier_pattern":
					w.d.bindings[w.text(p)] = binding{spec: spec, export: w.text(p)}
				case "pair_pattern":
					key, val := p.ChildByFieldName("key"), p.ChildByFieldName("value")
					if val != nil && val.Kind() == "identifier" {
						w.d.bindings[w.text(val)] = binding{spec: spec, export: w.text(key)}
					}
				}
			}
		}
		return
	}
	if nameNode == nil || nameNode.Kind() != "identifier" {
		for _, id := range patternNames(nameNode) {
			w.add(lib.VariableEntry{Name: w.text(id)}, id, meta{})
			w.declare(w.text(id), id, exported)
		}
		return
	}
	name := w.text(nameNode)
	w.declare(name, nameNode, exported)
	if toExports {
		w.d.exports["default"] = name
	}
	switch {
	case isFunction(value):
		w.function(name, nameNode, value)
	case value != nil && value.Kind() == "class":
		w.class(name, nameNode, value)
	default:
		w.add(lib.VariableEntry{Name: name}, nameNode, meta{})
		if value != nil && value.Kind() == "object" {
			w.objects[name] = value
		}
	}
}

func patternNames(n *tree_sitter.Node) []*tree_sitter.Node {
	if n == nil {
		return nil
	}
	switch n.Kind() {
	case "identifier", "shorthand_property_identifier_pattern":
		return []*tree_sitter.Node{n}
	case "pair_pattern":
		return patternNames(n.ChildByFieldName("value"))
	case "assignment_pattern":
		return patternNames(n.ChildByFieldName("left"))
	}
	var out []*tree_sitter.Node
	for _, c := range named(n) {
		out = append(out, patternNames(c)...)
	}
	return out
}

// assignment reads a top-level a = b: module.exports, exports.x, X.m and
// X.prototype.m.
func (w *walker) assignment(e *tree_sitter.Node) {
	left := unparen(e.ChildByFieldName("left"))
	value, chainExports := w.assignTarget(e.ChildByFieldName("right"))
	// res.set = res.header = function () {} names one function twice: each
	// inner member name gets the method too.
	for r := unparen(e.ChildByFieldName("right")); r != nil && r.Kind() == "assignment_expression"; r = unparen(r.ChildByFieldName("right")) {
		if l := unparen(r.ChildByFieldName("left")); l != nil && l.Kind() == "member_expression" && !w.moduleExports(l) {
			w.memberAssign(l, value)
		}
	}
	if w.moduleExports(left) || chainExports {
		w.exportValue(value)
		if !w.moduleExports(left) && left != nil && left.Kind() == "identifier" {
			w.d.exports["default"] = w.text(left)
		}
		if w.moduleExports(left) {
			return
		}
	}
	if left == nil || left.Kind() != "member_expression" {
		return
	}
	w.memberAssign(left, value)
}

// memberAssign reads left = value for a member left: exports.x, X.m and
// X.prototype.m.
func (w *walker) memberAssign(left, value *tree_sitter.Node) {
	object := unparen(left.ChildByFieldName("object"))
	prop := left.ChildByFieldName("property")
	name := w.text(prop)
	if w.moduleExports(object) {
		switch {
		case isFunction(value):
			w.function(name, prop, value)
			w.d.exports[name] = name
		case value != nil && value.Kind() == "identifier":
			w.d.exports[name] = w.text(value)
		case value != nil && value.Kind() == "class":
			w.class(name, prop, value)
			w.d.exports[name] = name
		default:
			w.add(lib.VariableEntry{Name: name}, prop, meta{})
			w.d.exports[name] = name
		}
		return
	}
	// X.prototype.m = function, X.m = function, X.prototype = { ... }.
	static := true
	if object != nil && object.Kind() == "member_expression" && w.text(object.ChildByFieldName("property")) == "prototype" {
		object = unparen(object.ChildByFieldName("object"))
		static = false
	}
	if object == nil || object.Kind() != "identifier" {
		return
	}
	owner := w.text(object)
	if _, ok := w.names[owner]; !ok {
		return
	}
	if name == "prototype" && value != nil && value.Kind() == "object" {
		w.objectMethodsOf(owner, value, false)
		return
	}
	if isFunction(value) {
		w.method(owner, name, prop, value, static)
		w.owners[owner] = true
	}
}

// exportValue reads module.exports = value.
func (w *walker) exportValue(value *tree_sitter.Node) {
	if spec, ok := w.requireSpec(value); ok {
		// module.exports = require("./x") passes x's exports on.
		w.addSpec(spec)
		w.d.reexp["default"] = binding{spec: spec, require: true}
		return
	}
	switch {
	case value == nil:
	case value.Kind() == "identifier":
		w.d.exports["default"] = w.text(value)
	case isFunction(value):
		name := "default"
		if id := value.ChildByFieldName("name"); id != nil {
			name = w.text(id)
		}
		w.function(name, value, value)
		w.declare(name, value, false)
		w.d.exports["default"] = name
	case value.Kind() == "class":
		name := "default"
		if id := value.ChildByFieldName("name"); id != nil {
			name = w.text(id)
		}
		w.class(name, value, value)
		w.declare(name, value, false)
		w.d.exports["default"] = name
	case value.Kind() == "object":
		for _, p := range named(value) {
			switch p.Kind() {
			case "shorthand_property_identifier":
				w.d.exports[w.text(p)] = w.text(p)
			case "pair":
				key := strings.Trim(w.text(p.ChildByFieldName("key")), `"'`)
				val := unparen(p.ChildByFieldName("value"))
				switch {
				case val != nil && val.Kind() == "identifier":
					w.d.exports[key] = w.text(val)
				case isFunction(val):
					w.function(key, p.ChildByFieldName("key"), val)
					w.declare(key, p.ChildByFieldName("key"), false)
					w.d.exports[key] = key
				}
			case "method_definition":
				key := w.text(p.ChildByFieldName("name"))
				w.function(key, p.ChildByFieldName("name"), p)
				w.declare(key, p.ChildByFieldName("name"), false)
				w.d.exports[key] = key
			}
		}
	}
}

func (w *walker) objectMethods(owner string, obj *tree_sitter.Node) {
	if w.objectMethodsOf(owner, obj, true) {
		w.owners[owner] = true
	}
}

func (w *walker) objectMethodsOf(owner string, obj *tree_sitter.Node, static bool) bool {
	any := false
	for _, p := range named(obj) {
		switch p.Kind() {
		case "pair":
			val := unparen(p.ChildByFieldName("value"))
			if isFunction(val) {
				key := p.ChildByFieldName("key")
				w.method(owner, strings.Trim(w.text(key), `"'`), key, val, static)
				any = true
			}
		case "method_definition":
			key := p.ChildByFieldName("name")
			w.method(owner, w.text(key), key, p, static)
			any = true
		}
	}
	if any {
		w.owners[owner] = true
	}
	return any
}

func (w *walker) function(name string, at, fn *tree_sitter.Node) {
	w.add(w.entry(name, fn), at, meta{node: fn})
}

func (w *walker) method(owner, name string, at, fn *tree_sitter.Node, static bool) {
	w.add(lib.MethodEntry{FunctionEntry: w.entry(name, fn), Type: &lib.TypeEntry{Name: owner}}, at, meta{node: fn, owner: owner, static: static})
}

func (w *walker) entry(name string, fn *tree_sitter.Node) lib.FunctionEntry {
	body := fn.ChildByFieldName("body")
	hash, size := hashBody(w.src, body)
	return lib.FunctionEntry{Name: name, Parameters: w.params(fn), BodyHash: hash, BodyBytes: size}
}

func (w *walker) params(fn *tree_sitter.Node) []lib.Parameter {
	if p := fn.ChildByFieldName("parameter"); p != nil {
		return []lib.Parameter{{Name: w.text(p)}}
	}
	var out []lib.Parameter
	for _, p := range named(fn.ChildByFieldName("parameters")) {
		for _, id := range patternNames(p) {
			out = append(out, lib.Parameter{Name: w.text(id)})
		}
		if p.Kind() == "rest_pattern" && p.NamedChildCount() > 0 {
			out = append(out, lib.Parameter{Name: "..." + w.text(p.NamedChild(0))})
		}
	}
	return out
}

func (w *walker) class(name string, at, n *tree_sitter.Node) {
	if name == "" {
		return
	}
	extends := ""
	for _, c := range named(n) {
		if c.Kind() == "class_heritage" && c.NamedChildCount() > 0 {
			extends = w.text(c.NamedChild(0))
		}
	}
	var fields []lib.Field
	type m struct {
		name   string
		at, fn *tree_sitter.Node
		static bool
	}
	var methods []m
	for _, member := range named(n.ChildByFieldName("body")) {
		static := false
		for i := uint(0); i < member.ChildCount(); i++ {
			if c := member.Child(i); c != nil && c.Kind() == "static" {
				static = true
			}
		}
		switch member.Kind() {
		case "field_definition":
			prop := member.ChildByFieldName("property")
			if v := unparen(member.ChildByFieldName("value")); isFunction(v) {
				methods = append(methods, m{w.text(prop), prop, v, static})
				continue
			}
			fields = append(fields, lib.Field{Name: w.text(prop)})
		case "method_definition":
			key := member.ChildByFieldName("name")
			methods = append(methods, m{w.text(key), key, member, static})
		}
	}
	w.types[name] = true
	w.add(lib.TypeEntry{Name: name, Fields: fields}, at, meta{extends: extends})
	for _, mm := range methods {
		w.method(name, mm.name, mm.at, mm.fn, mm.static)
	}
}

func hashBody(src []byte, body *tree_sitter.Node) (string, int) {
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

// assignMethodHashes sets each class's MethodsHash from its methods' body
// hashes in this file, in source order.
func assignMethodHashes(entries []lib.Entity) {
	byType := map[string][]string{}
	for _, entry := range entries {
		method, ok := entry.(lib.MethodEntry)
		if !ok || method.Type == nil || method.BodyHash == "" {
			continue
		}
		byType[method.Type.Name] = append(byType[method.Type.Name], method.BodyHash)
	}
	for i, entry := range entries {
		typ, ok := entry.(lib.TypeEntry)
		if !ok || len(byType[typ.Name]) == 0 {
			continue
		}
		h := sha256.New()
		for _, part := range byType[typ.Name] {
			raw, _ := hex.DecodeString(part)
			h.Write(raw)
			h.Write([]byte{0})
		}
		typ.MethodsHash = hex.EncodeToString(h.Sum(nil))
		entries[i] = typ
	}
}
