// Package swift parses Swift source into language-agnostic entities.
package swift

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
	tree_sitter_swift "github.com/alex-pinkus/tree-sitter-swift/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parser parses Swift source.
type Parser struct{}

// New returns a Swift parser.
func New() Parser { return Parser{} }

// draft is one file between reading it and filling its calls.
type draft struct {
	path   string
	src    []byte
	tree   *tree_sitter.Tree
	ns     string
	module string
	// importPath is the module, under its package directory when that is
	// not the snapshot root. pkgName is the Package.swift package name.
	importPath string
	pkgName    string
	imports    []string
	entities   []lib.Entity
	metas      []meta
}

// Kinds of method, by where it is declared. A conformance extension
// (extension T: P) is kept apart from T's own members, the way a Rust trait
// impl is. A requirement is a protocol member with no body.
const (
	inBody = iota
	inExtension
	inConformance
	inRequirement
)

// meta is what call resolution needs about one entity. owner is the type
// path the declaration sits in, as written (Outer.Inner); for an extension it
// is the extended type.
type meta struct {
	node   *tree_sitter.Node
	owner  string
	where  int
	static bool
	ret    string
	params []param
	// For a type: its kind, stored property types and what it inherits from.
	kind    string
	fields  map[string]string
	inherit []string
	alias   string
	// memberwise is the implicit initializer of a struct: one parameter per
	// stored property.
	memberwise []param
}

// param is one parameter as a call sees it: its argument label ("" for _),
// whether it may be left out, and whether it takes several arguments.
type param struct {
	label    string
	optional bool
	variadic bool
	fn       bool
	name     string
	typ      string
}

// Parse implements lib.Parser.
// Every file is collected before calls are resolved, since a Swift module is
// one namespace across its files. Calls that do not resolve are kept.
//
// A Package.swift names the modules: each target's files form one module
// named after the target. Without one, a file's module is its top folder,
// the way an Xcode project groups sources. ImportPath and Package are the
// module name.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_swift.Language())); err != nil {
		return nil, err
	}
	var manifests []manifest
	var drafts []*draft
	defer func() {
		for _, d := range drafts {
			d.tree.Close()
		}
	}()
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
		if base == "Package.swift" {
			manifests = append(manifests, readManifest(path.Dir(file.Path), file.Src))
			continue
		}
		if !strings.HasSuffix(base, ".swift") {
			return nil, fmt.Errorf("%s is not a Swift file", file.Path)
		}
		tree := parser.Parse(file.Src, nil)
		if tree == nil {
			return nil, fmt.Errorf("parse %s returned no tree", file.Path)
		}
		d := &draft{path: file.Path, src: file.Src, tree: tree}
		w := walker{src: file.Src, d: d}
		w.items(tree.RootNode(), "", inBody, "")
		assignMethodHashes(d.entities)
		drafts = append(drafts, d)
	}
	assignModules(drafts, manifests)
	resolve(drafts)

	out := make([]lib.ParsedFile, len(drafts))
	for i, d := range drafts {
		out[i] = lib.ParsedFile{
			Path:       d.path,
			Package:    d.module,
			ImportPath: d.importPath,
			Module:     d.pkgName,
			Entities:   d.entities,
		}
	}
	return out, nil
}

var _ lib.Parser = Parser{}

type walker struct {
	src []byte
	d   *draft
	// cur is the declaration being read, for the position of what it adds.
	cur *tree_sitter.Node
}

func (w *walker) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(w.src)
}

func join(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

// declKind is class, struct, enum, actor or extension for a
// class_declaration, which the grammar uses for all five.
func (w *walker) declKind(n *tree_sitter.Node) string {
	if k := n.ChildByFieldName("declaration_kind"); k != nil {
		return w.text(k)
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c.IsNamed() {
			continue
		}
		switch t := w.text(c); t {
		case "class", "struct", "enum", "actor", "extension":
			return t
		}
	}
	return ""
}

func (w *walker) inherits(n *tree_sitter.Node) []string {
	var out []string
	cursor := n.Walk()
	defer cursor.Close()
	for _, c := range n.NamedChildren(cursor) {
		if c.Kind() == "inheritance_specifier" {
			out = append(out, strings.TrimSpace(w.text(c.ChildByFieldName("inherits_from"))))
		}
	}
	return out
}

func isStatic(w *walker, n *tree_sitter.Node) bool {
	cursor := n.Walk()
	defer cursor.Close()
	for _, c := range n.NamedChildren(cursor) {
		if c.Kind() == "modifiers" {
			t := " " + strings.Join(strings.Fields(w.text(&c)), " ") + " "
			return strings.Contains(t, " static ") || strings.Contains(t, " class ")
		}
	}
	return false
}

// methodOwner is the Type of a member: the type path, or <T as P, Q> for
// a member of a conformance extension.
func methodOwner(owner string, where int, conform []string) string {
	if where == inConformance && len(conform) > 0 {
		return "<" + owner + " as " + strings.Join(conform, ", ") + ">"
	}
	return owner
}

// items reads the declarations of a file or of a type body. owner is the
// type path the list belongs to, where how it was declared, and conform the
// protocols of a conformance extension.
func (w *walker) items(list *tree_sitter.Node, owner string, where int, conform string) {
	cursor := list.Walk()
	defer cursor.Close()
	var conformList []string
	if conform != "" {
		conformList = strings.Split(conform, ", ")
	}
	for _, n := range list.NamedChildren(cursor) {
		w.cur = &n
		switch n.Kind() {
		case "import_declaration":
			if owner == "" {
				name := ""
				c := n.Walk()
				for _, ch := range n.NamedChildren(c) {
					if ch.Kind() == "identifier" {
						name = strings.Join(strings.Fields(w.text(&ch)), "")
					}
				}
				c.Close()
				if name != "" {
					w.d.imports = append(w.d.imports, strings.SplitN(name, ".", 2)[0])
					w.add(lib.ImportEntry{Path: name}, meta{})
				}
			}
		case "class_declaration", "protocol_declaration":
			kind := "protocol"
			if n.Kind() == "class_declaration" {
				kind = w.declKind(&n)
			}
			name := typeName(w.text(n.ChildByFieldName("name")))
			body := n.ChildByFieldName("body")
			if kind == "extension" {
				ext := name
				if owner != "" {
					ext = join(owner, name)
				}
				inh := w.inherits(&n)
				if body != nil {
					if len(inh) > 0 {
						w.items(body, ext, inConformance, strings.Join(inh, ", "))
					} else {
						w.items(body, ext, inExtension, "")
					}
				}
				continue
			}
			full := join(owner, name)
			m := meta{kind: kind, owner: owner, inherit: w.inherits(&n), fields: map[string]string{}}
			fields := w.fields(body, kind, m.fields)
			if kind == "struct" {
				m.memberwise = w.memberwise(body)
			}
			w.add(lib.TypeEntry{Name: full, Fields: fields}, m)
			if body != nil {
				sub := inBody
				if kind == "protocol" {
					sub = inRequirement
				}
				w.items(body, full, sub, "")
			}
		case "typealias_declaration":
			name := w.text(n.ChildByFieldName("name"))
			alias := ""
			cn := n.Walk()
			named := n.ChildrenByFieldName("name", cn)
			cn.Close()
			if len(named) > 1 {
				alias = w.text(&named[1])
			}
			w.add(lib.TypeEntry{Name: join(owner, name)}, meta{kind: "typealias", owner: owner, alias: alias})
		case "property_declaration", "protocol_property_declaration":
			computed := n.ChildByFieldName("computed_value")
			if owner == "" {
				for _, name := range w.bound(&n) {
					w.add(lib.VariableEntry{Name: name}, meta{})
				}
				continue
			}
			if computed == nil {
				continue
			}
			names := w.bound(&n)
			if len(names) != 1 {
				continue
			}
			fn := w.function(&n, names[0], computed)
			m := meta{node: &n, owner: owner, where: where, static: isStatic(w, &n), ret: w.annotation(&n)}
			w.add(lib.MethodEntry{FunctionEntry: fn, Type: &lib.TypeEntry{Name: methodOwner(owner, where, conformList)}}, m)
		case "function_declaration", "protocol_function_declaration", "init_declaration", "deinit_declaration", "subscript_declaration":
			name := w.text(n.ChildByFieldName("name"))
			body := n.ChildByFieldName("body")
			switch n.Kind() {
			case "init_declaration":
				name = "init"
			case "deinit_declaration":
				name = "deinit"
			case "subscript_declaration":
				name = "subscript"
				body = childOfKind(&n, "computed_property")
			}
			fn := w.function(&n, name, body)
			m := meta{node: &n, owner: owner, where: where, static: isStatic(w, &n), ret: w.returnType(&n), params: w.params(&n)}
			if n.Kind() == "protocol_function_declaration" {
				m.where = inRequirement
			}
			if owner == "" {
				w.add(fn, m)
				continue
			}
			w.add(lib.MethodEntry{FunctionEntry: fn, Type: &lib.TypeEntry{Name: methodOwner(owner, where, conformList)}}, m)
		}
	}
}

func childOfKind(n *tree_sitter.Node, kind string) *tree_sitter.Node {
	cursor := n.Walk()
	defer cursor.Close()
	for _, c := range n.NamedChildren(cursor) {
		if c.Kind() == kind {
			return &c
		}
	}
	return nil
}

func (w *walker) add(entry lib.Entity, m meta) {
	if n := w.cur; n != nil {
		if name := n.ChildByFieldName("name"); name != nil {
			n = name
		}
		p := n.StartPosition()
		entry = lib.At(entry, int(p.Row)+1, int(p.Column)+1)
	}
	w.d.entities = append(w.d.entities, entry)
	w.d.metas = append(w.d.metas, m)
}

// bound is every name a property declaration binds.
func (w *walker) bound(n *tree_sitter.Node) []string {
	var out []string
	cursor := n.Walk()
	defer cursor.Close()
	for _, p := range n.ChildrenByFieldName("name", cursor) {
		if p.Kind() != "pattern" {
			continue
		}
		if id := p.ChildByFieldName("bound_identifier"); id != nil {
			out = append(out, w.text(id))
			continue
		}
		c := p.Walk()
		for _, ch := range p.NamedChildren(c) {
			if ch.Kind() == "simple_identifier" {
				out = append(out, w.text(&ch))
			}
		}
		c.Close()
	}
	return out
}

func (w *walker) annotation(n *tree_sitter.Node) string {
	if a := childOfKind(n, "type_annotation"); a != nil {
		return strings.TrimSpace(w.text(a.ChildByFieldName("name")))
	}
	return ""
}

// fields lists the stored properties of a type, or the cases of an enum,
// and records each stored property's type as written, or the type it is
// initialized with (let x = T(...)).
func (w *walker) fields(body *tree_sitter.Node, kind string, types map[string]string) []lib.Field {
	if body == nil {
		return nil
	}
	var out []lib.Field
	cursor := body.Walk()
	defer cursor.Close()
	for _, n := range body.NamedChildren(cursor) {
		switch n.Kind() {
		case "enum_entry":
			c := n.Walk()
			for _, name := range n.ChildrenByFieldName("name", c) {
				out = append(out, lib.Field{Name: w.text(&name)})
			}
			c.Close()
		case "property_declaration", "protocol_property_declaration":
			names := w.bound(&n)
			typ := w.annotation(&n)
			if typ == "" && len(names) == 1 {
				if v := n.ChildByFieldName("value"); v != nil && v.Kind() == "call_expression" {
					if callee := v.NamedChild(0); callee != nil && callee.Kind() == "simple_identifier" {
						typ = w.text(callee)
					}
				}
			}
			for _, name := range names {
				if typ != "" {
					types[name] = typ
				}
				if n.ChildByFieldName("computed_value") == nil && n.Kind() == "property_declaration" {
					out = append(out, lib.Field{Name: name})
				}
			}
		}
	}
	return out
}

// memberwise lists the parameters of a struct's implicit initializer: each
// stored instance property, which may be left out when it has a default.
// A let with a value is not a parameter.
func (w *walker) memberwise(body *tree_sitter.Node) []param {
	if body == nil {
		return nil
	}
	var out []param
	cursor := body.Walk()
	defer cursor.Close()
	for _, n := range body.NamedChildren(cursor) {
		if n.Kind() != "property_declaration" || n.ChildByFieldName("computed_value") != nil || isStatic(w, &n) {
			continue
		}
		isLet := false
		if b := childOfKind(&n, "value_binding_pattern"); b != nil {
			isLet = strings.TrimSpace(w.text(b)) == "let"
		}
		hasValue := n.ChildByFieldName("value") != nil
		if isLet && hasValue {
			continue
		}
		typ := w.annotation(&n)
		for _, name := range w.bound(&n) {
			out = append(out, param{label: name, name: name, typ: typ, optional: hasValue || (!isLet && strings.HasSuffix(typ, "?"))})
		}
	}
	return out
}

// returnType is the written return type: the name-field type that follows
// the parameter list.
func (w *walker) returnType(n *tree_sitter.Node) string {
	cursor := n.Walk()
	defer cursor.Close()
	names := n.ChildrenByFieldName("name", cursor)
	for _, c := range names {
		if c.Kind() != "simple_identifier" {
			return strings.TrimSpace(w.text(&c))
		}
	}
	return ""
}

func (w *walker) params(n *tree_sitter.Node) []param {
	var out []param
	cursor := n.Walk()
	defer cursor.Close()
	children := n.NamedChildren(cursor)
	for i, c := range children {
		if c.Kind() != "parameter" {
			continue
		}
		p := param{name: w.text(c.ChildByFieldName("name"))}
		if ext := c.ChildByFieldName("external_name"); ext != nil {
			p.label = w.text(ext)
		} else {
			p.label = p.name
		}
		if p.label == "_" {
			p.label = ""
		}
		cc := c.Walk()
		for _, t := range c.ChildrenByFieldName("name", cc) {
			if t.Kind() != "simple_identifier" {
				p.typ = strings.TrimSpace(w.text(&t))
				p.fn = t.Kind() == "function_type" || strings.Contains(p.typ, "->")
			}
		}
		cc.Close()
		p.variadic = strings.HasSuffix(strings.TrimSpace(w.text(&c)), "...")
		p.optional = p.variadic || (i+1 < len(children) && n.FieldNameForChild(uint32(childIndex(n, &children[i+1]))) == "default_value")
		out = append(out, p)
	}
	return out
}

func childIndex(parent, child *tree_sitter.Node) int {
	for i := uint(0); i < parent.ChildCount(); i++ {
		if c := parent.Child(i); c.StartByte() == child.StartByte() && c.EndByte() == child.EndByte() && c.Kind() == child.Kind() {
			return int(i)
		}
	}
	return -1
}

func (w *walker) function(n *tree_sitter.Node, name string, body *tree_sitter.Node) lib.FunctionEntry {
	var params []lib.Parameter
	for _, p := range w.params(n) {
		params = append(params, lib.Parameter{Name: p.name, Type: p.typ})
	}
	var returns []lib.Parameter
	ret := w.returnType(n)
	if n.Kind() == "property_declaration" || n.Kind() == "protocol_property_declaration" {
		ret = w.annotation(n)
	}
	if ret != "" {
		returns = []lib.Parameter{{Type: ret}}
	}
	bodyHash, bodyBytes := hashBody(w.src, body)
	return lib.FunctionEntry{Name: name, Parameters: params, ReturnArgs: returns, BodyHash: bodyHash, BodyBytes: bodyBytes}
}

// hashBody is the hex SHA-256 of the body source, and the length of those
// bytes. A declaration with no body has an empty hash and length 0.
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

// assignMethodHashes sets each type's MethodsHash from the body hashes of
// its members in this file, in its body, extensions and conformances alike.
func assignMethodHashes(entries []lib.Entity) {
	byType := map[string][]string{}
	for _, entry := range entries {
		method, ok := entry.(lib.MethodEntry)
		if !ok || method.Type == nil || method.BodyHash == "" {
			continue
		}
		name := ownerType(method.Type.Name)
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

// ownerType is the type of a member owner: T for both T and <T as P>.
func ownerType(name string) string {
	if strings.HasPrefix(name, "<") {
		if i := strings.Index(name, " as "); i > 0 {
			return name[1:i]
		}
	}
	return name
}

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

// typeName is a written type without generic arguments or spaces:
// Box<T> is Box, Swift.Array<Int> is Swift.Array.
func typeName(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.IndexByte(t, '<'); i >= 0 {
		t = t[:i]
	}
	return strings.Join(strings.Fields(t), "")
}
