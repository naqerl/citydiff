package rust

import (
	"bytes"
	"path"
	"sort"
	"strings"
	"unicode"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// use is one leaf of a use tree. path is as written; alias is the name it
// binds, empty for a glob. mod is the inline module it sits in.
type use struct {
	path  []string
	alias string
	glob  bool
	mod   []string
}

// binding is a local name at a call. A blocked name is a value of unknown
// type, so a method call on it stays unresolved. Otherwise recv is its type.
type binding struct {
	block bool
	recv  string
}

// pending is one call before refs are filled. A path call names a function
// or an associated function by path. A method call names recv and name.
type pending struct {
	expr    string
	segs    []string
	recv    string
	name    string
	method  bool
	resolve bool
	start   uint
	end     uint
}

type draft struct {
	path       string
	crate      string
	ns         string
	module     []string
	pkg        string
	importPath string
	uses       []use
	entities   []lib.Entity
	calls      [][]pending
	mods       [][]string
	recvs      []string
}

type crateRoot struct {
	dir  string
	name string
}

func crateName(src []byte) string {
	inPackage := false
	for _, line := range bytes.Split(src, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if strings.HasPrefix(text, "[") {
			inPackage = text == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok || strings.TrimSpace(key) != "name" {
			continue
		}
		return strings.ReplaceAll(strings.Trim(strings.TrimSpace(value), `"'`), "-", "_")
	}
	return ""
}

func useTree(src []byte, n *tree_sitter.Node, prefix []string) []use {
	if n == nil {
		return nil
	}
	join := func(rest ...string) []string {
		return append(append([]string{}, prefix...), rest...)
	}
	switch n.Kind() {
	case "use_as_clause":
		segs := join(splitPath(n.ChildByFieldName("path").Utf8Text(src))...)
		return []use{{path: segs, alias: n.ChildByFieldName("alias").Utf8Text(src)}}
	case "use_wildcard":
		var segs []string
		if inner := n.NamedChild(0); inner != nil {
			segs = join(splitPath(inner.Utf8Text(src))...)
		} else {
			segs = join()
		}
		return []use{{path: append(segs, "*"), glob: true}}
	case "scoped_use_list":
		next := prefix
		if p := n.ChildByFieldName("path"); p != nil {
			next = join(splitPath(p.Utf8Text(src))...)
		}
		return useTree(src, n.ChildByFieldName("list"), next)
	case "use_list":
		var out []use
		cursor := n.Walk()
		defer cursor.Close()
		for _, child := range n.NamedChildren(cursor) {
			out = append(out, useTree(src, &child, prefix)...)
		}
		return out
	default:
		segs := join(splitPath(n.Utf8Text(src))...)
		if len(segs) == 0 {
			return nil
		}
		alias := segs[len(segs)-1]
		if alias == "self" && len(segs) > 1 {
			segs = segs[:len(segs)-1]
			alias = segs[len(segs)-1]
		}
		return []use{{path: segs, alias: alias}}
	}
}

func splitPath(s string) []string {
	var out []string
	for _, seg := range strings.Split(s, "::") {
		seg = strings.TrimSpace(seg)
		if i := strings.IndexByte(seg, '<'); i >= 0 {
			seg = seg[:i]
		}
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// assignModules places each file in a crate namespace and module path.
// src/lib.rs and src/main.rs are the crate root of the Cargo package. Files
// under src/bin, tests, examples and benches are each their own root.
func assignModules(drafts []*draft, crates []crateRoot) {
	for _, d := range drafts {
		root, ok := bestCrate(crates, path.Dir(d.path))
		rel := d.path
		if ok {
			d.crate = root.name
			if root.dir != "." {
				rel = strings.TrimPrefix(d.path, root.dir+"/")
			}
		}
		d.ns = root.dir
		stem := strings.TrimSuffix(rel, ".rs")
		switch {
		case ok && strings.HasPrefix(rel, "src/bin/"):
			d.ns += "\x00" + stem
			d.module = nil
		case ok && strings.HasPrefix(rel, "src/"):
			d.module = moduleSegs(strings.TrimPrefix(stem, "src/"))
		case ok:
			d.ns += "\x00" + stem
			d.module = nil
		default:
			d.module = moduleSegs(strings.TrimPrefix(stem, "src/"))
		}
		for i := range d.uses {
			d.uses[i].mod = append(append([]string{}, d.module...), d.uses[i].mod...)
		}
		if !ok {
			if len(d.module) > 0 {
				d.pkg = d.module[len(d.module)-1]
			}
			continue
		}
		parts := []string{d.crate}
		if strings.Contains(d.ns, "\x00") {
			parts = append(parts, strings.Split(stem, "/")...)
		}
		parts = append(parts, d.module...)
		d.importPath = strings.Join(parts, "/")
		d.pkg = parts[len(parts)-1]
	}
}

func moduleSegs(stem string) []string {
	segs := strings.Split(stem, "/")
	switch segs[len(segs)-1] {
	case "mod":
		segs = segs[:len(segs)-1]
	case "lib", "main":
		if len(segs) == 1 {
			segs = nil
		}
	}
	return segs
}

func bestCrate(crates []crateRoot, dir string) (crateRoot, bool) {
	best := crateRoot{dir: "."}
	found := false
	for _, c := range crates {
		if c.dir != "." && dir != c.dir && !strings.HasPrefix(dir, c.dir+"/") {
			continue
		}
		if !found || len(c.dir) > len(best.dir) || best.dir == "." {
			best, found = c, true
		}
	}
	return best, found
}

type decl struct {
	path string
	name string
	recv string
}

type index struct {
	funcs   map[string][]decl
	methods map[string][]decl
	modules map[string]string
	libNS   map[string]string
}

func modKey(ns string, mod []string) string {
	return ns + "\x00\x00" + strings.Join(mod, "::")
}

func buildIndex(drafts []*draft) *index {
	idx := &index{
		funcs:   map[string][]decl{},
		methods: map[string][]decl{},
		modules: map[string]string{},
		libNS:   map[string]string{},
	}
	for _, d := range drafts {
		if d.crate != "" && !strings.Contains(d.ns, "\x00") {
			idx.libNS[d.crate] = d.ns
		}
		idx.modules[modKey(d.ns, d.module)] = d.importPath
		for i, entity := range d.entities {
			mod := append(append([]string{}, d.module...), d.mods[i]...)
			idx.modules[modKey(d.ns, mod)] = d.importPath
			switch entity := entity.(type) {
			case lib.FunctionEntry:
				key := modKey(d.ns, mod) + "\x00" + entity.Name
				idx.funcs[key] = append(idx.funcs[key], decl{path: d.path, name: entity.Name})
			case lib.MethodEntry:
				recv := d.recvs[i]
				key := d.ns + "\x00" + recv + "\x00" + entity.Name
				idx.methods[key] = append(idx.methods[key], decl{path: d.path, name: entity.Name, recv: recv})
			}
		}
	}
	return idx
}

func one(decls []decl) (*lib.CallRef, bool) {
	if len(decls) != 1 {
		return nil, false
	}
	return &lib.CallRef{Path: decls[0].path, Name: decls[0].name, Recv: decls[0].recv}, true
}

// absolute turns a path as written in module cur into a namespace and an
// absolute module path. ok is false when it leaves the snapshot.
func (idx *index) absolute(d *draft, cur []string, segs []string, local bool) (string, []string, bool) {
	if len(segs) == 0 {
		return "", nil, false
	}
	ns := d.ns
	switch segs[0] {
	case "crate":
		return ns, segs[1:], true
	case "self":
		return ns, append(append([]string{}, cur...), segs[1:]...), true
	case "super":
		mod := append([]string{}, cur...)
		for len(segs) > 0 && segs[0] == "super" {
			if len(mod) == 0 {
				return "", nil, false
			}
			mod = mod[:len(mod)-1]
			segs = segs[1:]
		}
		return ns, append(mod, segs...), true
	}
	if local {
		for _, u := range d.uses {
			if !u.glob && u.alias == segs[0] && sameMod(u.mod, cur) {
				uns, abs, ok := idx.absolute(d, u.mod, u.path, false)
				if !ok {
					return "", nil, false
				}
				return uns, append(abs, segs[1:]...), true
			}
		}
	}
	if libNS, ok := idx.libNS[segs[0]]; ok && segs[0] == d.crate {
		return libNS, segs[1:], true
	}
	return ns, append(append([]string{}, cur...), segs...), true
}

func sameMod(a, b []string) bool {
	return strings.Join(a, "::") == strings.Join(b, "::")
}

func (idx *index) lookupPath(d *draft, cur []string, segs []string) (*lib.CallRef, bool) {
	var tries [][]string
	if len(segs) == 1 {
		tries = append(tries, append(append([]string{}, cur...), segs[0]))
		if ns, abs, ok := idx.absolute(d, cur, segs, true); ok && ns == d.ns {
			tries = append(tries, abs)
		} else if ok {
			if ref, found := idx.lookupAbs(ns, abs); found {
				return ref, true
			}
		}
		for _, u := range d.uses {
			if u.glob && sameMod(u.mod, cur) {
				if ns, abs, ok := idx.absolute(d, u.mod, u.path[:len(u.path)-1], false); ok {
					if ref, found := idx.lookupAbs(ns, append(abs, segs[0])); found {
						return ref, true
					}
				}
			}
		}
	} else if ns, abs, ok := idx.absolute(d, cur, segs, true); ok {
		if ref, found := idx.lookupAbs(ns, abs); found {
			return ref, true
		}
	}
	for _, abs := range tries {
		if ref, found := idx.lookupAbs(d.ns, abs); found {
			return ref, true
		}
	}
	return nil, false
}

func (idx *index) lookupAbs(ns string, abs []string) (*lib.CallRef, bool) {
	if len(abs) == 0 {
		return nil, false
	}
	name := abs[len(abs)-1]
	mod := abs[:len(abs)-1]
	if ref, ok := one(idx.funcs[modKey(ns, mod)+"\x00"+name]); ok {
		return ref, true
	}
	if len(mod) > 0 {
		return one(idx.methods[ns+"\x00"+mod[len(mod)-1]+"\x00"+name])
	}
	return nil, false
}

func resolve(drafts []*draft) {
	idx := buildIndex(drafts)
	for _, d := range drafts {
		for i, entity := range d.entities {
			if imp, ok := entity.(lib.ImportEntry); ok {
				cur := append(append([]string{}, d.module...), d.mods[i]...)
				d.entities[i] = lib.ImportEntry{Path: idx.importPath(d, cur, imp.Path)}
			}
		}
		for i, pendingCalls := range d.calls {
			if len(pendingCalls) == 0 {
				continue
			}
			cur := append(append([]string{}, d.module...), d.mods[i]...)
			calls := make([]lib.Call, len(pendingCalls))
			for j, call := range pendingCalls {
				calls[j] = lib.Call{Expr: call.expr}
				if !call.resolve {
					continue
				}
				var ref *lib.CallRef
				var ok bool
				if call.method {
					ref, ok = one(idx.methods[d.ns+"\x00"+call.recv+"\x00"+call.name])
				} else {
					ref, ok = idx.lookupPath(d, cur, call.segs)
				}
				if ok {
					calls[j].Ref = ref
				}
			}
			setCalls(d, i, calls)
		}
	}
}

// importPath is the ImportPath of the snapshot module a use names, or the
// path as written when it names nothing in the snapshot.
func (idx *index) importPath(d *draft, cur []string, written string) string {
	segs := strings.Split(written, "::")
	if segs[len(segs)-1] == "*" {
		segs = segs[:len(segs)-1]
	}
	ns, abs, ok := idx.absolute(d, cur, segs, false)
	if !ok {
		return written
	}
	if segs[0] != "crate" && segs[0] != "self" && segs[0] != "super" && segs[0] != d.crate {
		if _, known := idx.modules[modKey(ns, abs[:min(len(abs), len(cur)+1)])]; !known {
			return written
		}
	}
	for n := len(abs); n >= 0; n-- {
		if p, known := idx.modules[modKey(ns, abs[:n])]; known && p != "" {
			return p
		}
	}
	return written
}

func setCalls(d *draft, i int, calls []lib.Call) {
	switch entity := d.entities[i].(type) {
	case lib.FunctionEntry:
		entity.Calls = calls
		d.entities[i] = entity
	case lib.MethodEntry:
		entity.Calls = calls
		d.entities[i] = entity
	}
}

func typeBinding(t string) binding {
	name := normType(t)
	if name == "" || name == "Self" || !unicode.IsUpper([]rune(name)[0]) {
		return binding{block: true}
	}
	return binding{recv: name}
}

// bindPattern binds a parameter pattern. A plain name takes b; every name in
// a destructuring pattern is blocked.
func bindPattern(scope map[string]binding, pattern string, b binding) {
	pattern = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(pattern), "mut "))
	if isIdent(pattern) {
		scope[pattern] = b
		return
	}
	for _, word := range strings.FieldsFunc(pattern, func(r rune) bool { return !isIdentRune(r) }) {
		if isIdent(word) && !unicode.IsUpper([]rune(word)[0]) {
			scope[word] = binding{block: true}
		}
	}
}

func isIdentRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func isIdent(s string) bool {
	if s == "" || unicode.IsDigit([]rune(s)[0]) {
		return false
	}
	for _, r := range s {
		if !isIdentRune(r) {
			return false
		}
	}
	return true
}

type collector struct {
	src    []byte
	self   string
	scopes []map[string]binding
	out    []pending
}

func collectCalls(src []byte, body *tree_sitter.Node, outer map[string]binding, self string) []pending {
	if body == nil {
		return nil
	}
	c := &collector{src: src, self: self, scopes: []map[string]binding{outer}}
	c.walk(body)
	sort.SliceStable(c.out, func(i, j int) bool {
		if c.out[i].start != c.out[j].start {
			return c.out[i].start < c.out[j].start
		}
		return c.out[i].end < c.out[j].end
	})
	return c.out
}

func (c *collector) push() func() {
	c.scopes = append(c.scopes, map[string]binding{})
	return func() { c.scopes = c.scopes[:len(c.scopes)-1] }
}

func (c *collector) bind(pattern *tree_sitter.Node, b binding) {
	if pattern == nil {
		return
	}
	bindPattern(c.scopes[len(c.scopes)-1], pattern.Utf8Text(c.src), b)
}

func (c *collector) find(name string) (binding, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if b, ok := c.scopes[i][name]; ok {
			return b, true
		}
	}
	return binding{}, false
}

func (c *collector) walk(n *tree_sitter.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "function_item", "impl_item", "trait_item", "mod_item", "struct_item", "enum_item", "use_declaration":
		return
	case "block", "if_expression", "match_arm", "while_expression", "loop_expression":
		defer c.push()()
	case "closure_expression":
		defer c.push()()
		if params := n.ChildByFieldName("parameters"); params != nil {
			cursor := params.Walk()
			for _, p := range params.NamedChildren(cursor) {
				if p.Kind() == "parameter" {
					b := binding{block: true}
					if t := p.ChildByFieldName("type"); t != nil {
						b = typeBinding(t.Utf8Text(c.src))
					}
					c.bind(p.ChildByFieldName("pattern"), b)
				} else {
					c.bind(&p, binding{block: true})
				}
			}
			cursor.Close()
		}
		c.walk(n.ChildByFieldName("body"))
		return
	case "let_declaration":
		c.walk(n.ChildByFieldName("value"))
		c.walk(n.ChildByFieldName("alternative"))
		b := binding{block: true}
		if t := n.ChildByFieldName("type"); t != nil {
			b = typeBinding(t.Utf8Text(c.src))
		} else if v := n.ChildByFieldName("value"); v != nil {
			b = c.valueBinding(v)
		}
		c.bind(n.ChildByFieldName("pattern"), b)
		return
	case "let_condition":
		c.walk(n.ChildByFieldName("value"))
		c.bind(n.ChildByFieldName("pattern"), binding{block: true})
		return
	case "for_expression":
		c.walk(n.ChildByFieldName("value"))
		defer c.push()()
		c.bind(n.ChildByFieldName("pattern"), binding{block: true})
		c.walk(n.ChildByFieldName("body"))
		return
	case "call_expression":
		if call, ok := c.pendingFrom(n.ChildByFieldName("function")); ok {
			call.start, call.end = n.StartByte(), n.EndByte()
			c.out = append(c.out, call)
		}
	case "macro_invocation":
		name := n.ChildByFieldName("macro").Utf8Text(c.src)
		c.out = append(c.out, pending{expr: name + "!", start: n.StartByte(), end: n.EndByte()})
		c.tokenCalls(n)
		return
	}
	if n.Kind() == "match_arm" {
		c.bind(n.ChildByFieldName("pattern"), binding{block: true})
	}
	cursor := n.Walk()
	defer cursor.Close()
	for _, child := range n.NamedChildren(cursor) {
		c.walk(&child)
	}
}

// tokenCalls records name(...) inside macro arguments. A token tree is not
// parsed as expressions, so only a bare name directly followed by a
// parenthesized group counts.
func (c *collector) tokenCalls(n *tree_sitter.Node) {
	cursor := n.Walk()
	defer cursor.Close()
	children := n.Children(cursor)
	for i := range children {
		child := &children[i]
		if child.Kind() == "token_tree" {
			c.tokenCalls(child)
			continue
		}
		if child.Kind() != "identifier" || i+1 >= len(children) {
			continue
		}
		next := &children[i+1]
		if next.Kind() != "token_tree" || !bytes.HasPrefix(c.src[next.StartByte():], []byte("(")) {
			continue
		}
		if i > 0 && (children[i-1].Kind() == "." || children[i-1].Kind() == "::") {
			continue
		}
		name := child.Utf8Text(c.src)
		call := pending{expr: name, start: child.StartByte(), end: next.EndByte()}
		if _, bound := c.find(name); !bound {
			call.segs, call.resolve = []string{name}, true
		}
		c.out = append(c.out, call)
	}
}

func (c *collector) valueBinding(v *tree_sitter.Node) binding {
	for v != nil && (v.Kind() == "reference_expression" || v.Kind() == "parenthesized_expression") {
		if v.Kind() == "reference_expression" {
			v = v.ChildByFieldName("value")
		} else {
			v = v.NamedChild(0)
		}
	}
	if v != nil && v.Kind() == "struct_expression" {
		name := normType(v.ChildByFieldName("name").Utf8Text(c.src))
		if name == "Self" {
			name = c.self
		}
		if name != "" {
			return binding{recv: name}
		}
	}
	return binding{block: true}
}

func (c *collector) pendingFrom(fn *tree_sitter.Node) (pending, bool) {
	if fn == nil {
		return pending{}, false
	}
	expr := fn.Utf8Text(c.src)
	for fn.Kind() == "generic_function" || fn.Kind() == "parenthesized_expression" {
		next := fn.ChildByFieldName("function")
		if fn.Kind() == "parenthesized_expression" {
			next = fn.NamedChild(0)
		}
		if next == nil {
			break
		}
		fn = next
	}
	switch fn.Kind() {
	case "identifier":
		name := fn.Utf8Text(c.src)
		if _, bound := c.find(name); bound {
			return pending{expr: expr}, true
		}
		return pending{expr: expr, segs: []string{name}, resolve: true}, true
	case "scoped_identifier":
		segs := splitPath(fn.Utf8Text(c.src))
		if len(segs) == 2 && segs[0] == "Self" && c.self != "" {
			return pending{expr: expr, recv: c.self, name: segs[1], method: true, resolve: true}, true
		}
		return pending{expr: expr, segs: segs, resolve: len(segs) > 0}, true
	case "field_expression":
		value := fn.ChildByFieldName("value")
		field := fn.ChildByFieldName("field")
		if value == nil || field == nil || field.Kind() != "field_identifier" {
			return pending{expr: expr}, true
		}
		name := field.Utf8Text(c.src)
		if value.Kind() != "self" && value.Kind() != "identifier" {
			return pending{expr: expr}, true
		}
		b, ok := c.find(value.Utf8Text(c.src))
		if !ok || b.block {
			return pending{expr: expr}, true
		}
		return pending{expr: expr, recv: b.recv, name: name, method: true, resolve: true}, true
	case "closure_expression":
		return pending{expr: "closure"}, true
	default:
		return pending{expr: expr}, true
	}
}
