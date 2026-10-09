package rust

import (
	"bytes"
	"sort"
	"strings"
	"unicode"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// binding is a local name. A blocked name has no known type, so a method call
// on it stays unresolved.
type binding struct {
	t tval
}

type found struct {
	call  lib.Call
	start int
	end   int
}

func resolve(drafts []*draft, parser *tree_sitter.Parser) {
	idx := buildIndex(drafts)
	for _, d := range drafts {
		for i, entity := range d.entities {
			m := d.metas[i]
			cur := fullMod(d, m)
			switch entity := entity.(type) {
			case lib.ImportEntry:
				d.entities[i] = lib.ImportEntry{Path: idx.importPath(d, cur, entity.Path)}
			case lib.FunctionEntry:
				entity.Calls = collect(idx, parser, d, m, scope{d: d, mod: cur})
				d.entities[i] = entity
			case lib.MethodEntry:
				entity.Calls = collect(idx, parser, d, m, scope{d: d, mod: cur, self: m.recv})
				d.entities[i] = entity
			}
		}
	}
}

type collector struct {
	idx    *index
	parser *tree_sitter.Parser
	src    []byte
	delta  int
	at     scope
	scopes []map[string]binding
	out    []found
}

func collect(idx *index, parser *tree_sitter.Parser, d *draft, m meta, at scope) []lib.Call {
	if m.node == nil {
		return nil
	}
	body := m.node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	c := &collector{idx: idx, parser: parser, src: d.src, at: at, scopes: []map[string]binding{{}}}
	if at.self != "" {
		c.scopes[0]["self"] = binding{t: tval{id: at.self}}
	}
	if params := m.node.ChildByFieldName("parameters"); params != nil {
		cursor := params.Walk()
		for _, p := range params.NamedChildren(cursor) {
			if p.Kind() == "parameter" {
				c.bind(p.ChildByFieldName("pattern"), idx.convert(at, c.text(p.ChildByFieldName("type"))))
			}
		}
		cursor.Close()
	}
	c.walk(body)
	if len(c.out) == 0 {
		return nil
	}
	sort.SliceStable(c.out, func(i, j int) bool {
		if c.out[i].start != c.out[j].start {
			return c.out[i].start < c.out[j].start
		}
		return c.out[i].end < c.out[j].end
	})
	calls := make([]lib.Call, len(c.out))
	for i, f := range c.out {
		calls[i] = f.call
	}
	return calls
}

func (c *collector) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(c.src)
}

func (c *collector) push() func() {
	c.scopes = append(c.scopes, map[string]binding{})
	return func() { c.scopes = c.scopes[:len(c.scopes)-1] }
}

// bind binds a pattern. A plain name, with or without mut, takes t. Every
// name in a destructuring pattern is bound with no type.
func (c *collector) bind(pattern *tree_sitter.Node, t tval) {
	if pattern == nil {
		return
	}
	scope := c.scopes[len(c.scopes)-1]
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.text(pattern)), "mut "))
	if isIdent(text) {
		scope[text] = binding{t: t}
		return
	}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !isIdentRune(r) }) {
		if isIdent(word) && !unicode.IsUpper([]rune(word)[0]) {
			scope[word] = binding{}
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

func (c *collector) find(name string) (binding, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if b, ok := c.scopes[i][name]; ok {
			return b, true
		}
	}
	return binding{}, false
}

func (c *collector) record(n *tree_sitter.Node, call lib.Call) {
	c.out = append(c.out, found{call: call, start: int(n.StartByte()) + c.delta, end: int(n.EndByte()) + c.delta})
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
					c.bind(p.ChildByFieldName("pattern"), c.idx.convert(c.at, c.text(p.ChildByFieldName("type"))))
				} else {
					c.bind(&p, tval{})
				}
			}
			cursor.Close()
		}
		c.walk(n.ChildByFieldName("body"))
		return
	case "let_declaration":
		value := n.ChildByFieldName("value")
		c.walk(value)
		c.walk(n.ChildByFieldName("alternative"))
		var t tval
		if typ := n.ChildByFieldName("type"); typ != nil {
			t = c.idx.convert(c.at, c.text(typ))
		} else if value != nil {
			t = c.typeOf(value)
		}
		c.bind(n.ChildByFieldName("pattern"), t)
		return
	case "let_condition":
		c.walk(n.ChildByFieldName("value"))
		c.bind(n.ChildByFieldName("pattern"), tval{})
		return
	case "for_expression":
		c.walk(n.ChildByFieldName("value"))
		defer c.push()()
		c.bind(n.ChildByFieldName("pattern"), tval{})
		c.walk(n.ChildByFieldName("body"))
		return
	case "call_expression":
		fn := n.ChildByFieldName("function")
		if c.text(fn) == macroWrapper {
			break
		}
		call, _ := c.callee(fn)
		c.record(n, call)
	case "macro_invocation":
		c.macro(n)
		return
	}
	if n.Kind() == "match_arm" {
		c.bind(n.ChildByFieldName("pattern"), tval{})
	}
	cursor := n.Walk()
	defer cursor.Close()
	for _, child := range n.NamedChildren(cursor) {
		c.walk(&child)
	}
}

// callee resolves the function part of a call and returns the call and the
// type it returns.
func (c *collector) callee(fn *tree_sitter.Node) (lib.Call, tval) {
	call := lib.Call{Expr: c.text(fn)}
	for fn != nil && (fn.Kind() == "generic_function" || fn.Kind() == "parenthesized_expression") {
		if fn.Kind() == "generic_function" {
			fn = fn.ChildByFieldName("function")
		} else {
			fn = fn.NamedChild(0)
		}
	}
	if fn == nil {
		return call, tval{}
	}
	var decl fdecl
	ok := false
	switch fn.Kind() {
	case "identifier":
		name := c.text(fn)
		if _, bound := c.find(name); bound {
			return call, tval{}
		}
		decl, ok = c.idx.function(c.at.d, c.at.mod, []string{name})
	case "scoped_identifier":
		segs := splitPath(c.text(fn))
		if len(segs) == 0 {
			return call, tval{}
		}
		if len(segs) == 2 && segs[0] == "Self" {
			decl, ok = c.idx.method(c.at.self, segs[1])
			break
		}
		decl, ok = c.idx.function(c.at.d, c.at.mod, segs)
		if !ok && len(segs) >= 2 {
			id := c.idx.resolveType(c.at, strings.Join(segs[:len(segs)-1], "::"))
			decl, ok = c.idx.method(id, segs[len(segs)-1])
		}
	case "field_expression":
		field := fn.ChildByFieldName("field")
		if field == nil || field.Kind() != "field_identifier" {
			return call, tval{}
		}
		recv := c.typeOf(fn.ChildByFieldName("value"))
		name := c.text(field)
		if recv.wrap && (name == "unwrap" || name == "expect") {
			return call, tval{id: recv.id}
		}
		if recv.wrap {
			return call, tval{}
		}
		decl, ok = c.idx.method(recv.id, name)
	case "closure_expression":
		return lib.Call{Expr: "closure"}, tval{}
	}
	if !ok {
		return call, tval{}
	}
	ref := decl.ref
	call.Ref = &ref
	return call, c.idx.returns(decl)
}

// typeOf infers the type of an expression from bindings, struct literals,
// field types and return types. Anything else is unknown.
func (c *collector) typeOf(n *tree_sitter.Node) tval {
	if n == nil {
		return tval{}
	}
	switch n.Kind() {
	case "identifier":
		name := c.text(n)
		if b, ok := c.find(name); ok {
			return b.t
		}
		if unicode.IsUpper([]rune(name)[0]) {
			return tval{id: c.idx.resolveType(c.at, name)}
		}
		return tval{}
	case "self":
		return tval{id: c.at.self}
	case "parenthesized_expression":
		return c.typeOf(n.NamedChild(0))
	case "reference_expression":
		return c.typeOf(n.ChildByFieldName("value"))
	case "struct_expression":
		return tval{id: c.idx.resolveType(c.at, c.text(n.ChildByFieldName("name")))}
	case "try_expression":
		if t := c.typeOf(n.NamedChild(0)); t.wrap {
			return tval{id: t.id}
		}
	case "call_expression":
		_, t := c.callee(n.ChildByFieldName("function"))
		return t
	case "field_expression":
		field := n.ChildByFieldName("field")
		if field != nil && field.Kind() == "field_identifier" {
			return c.idx.field(c.typeOf(n.ChildByFieldName("value")), c.text(field))
		}
	}
	return tval{}
}

const macroWrapper = "__citydiff_macro"

// exprMacros take expressions as arguments, so their token tree is parsed as
// the arguments of a call. vec! is parsed as an array, for vec![x; n].
var exprMacros = map[string]bool{
	"println": true, "print": true, "eprintln": true, "eprint": true, "format": true,
	"write": true, "writeln": true, "assert": true, "assert_eq": true, "assert_ne": true,
	"debug_assert": true, "debug_assert_eq": true, "debug_assert_ne": true,
	"vec": true, "panic": true, "matches": true, "dbg": true, "format_args": true,
}

func (c *collector) macro(n *tree_sitter.Node) {
	name := bareName(c.text(n.ChildByFieldName("macro")))
	c.record(n, lib.Call{Expr: name + "!"})
	var tt *tree_sitter.Node
	cursor := n.Walk()
	for _, child := range n.NamedChildren(cursor) {
		if child.Kind() == "token_tree" {
			tt = &child
		}
	}
	cursor.Close()
	if tt == nil || tt.EndByte()-tt.StartByte() < 2 {
		return
	}
	if !exprMacros[name] {
		c.tokenCalls(tt)
		return
	}
	inner := c.src[tt.StartByte()+1 : tt.EndByte()-1]
	prefix, suffix := "fn f() { "+macroWrapper+"(", "); }"
	if name == "vec" {
		prefix, suffix = "fn f() { [", "]; }"
	}
	src := append(append([]byte(prefix), inner...), suffix...)
	tree := c.parser.Parse(src, nil)
	if tree == nil {
		return
	}
	defer tree.Close()
	fn := tree.RootNode().NamedChild(0)
	if fn == nil || fn.Kind() != "function_item" {
		return
	}
	savedSrc, savedDelta := c.src, c.delta
	c.delta = savedDelta + int(tt.StartByte()) + 1 - len(prefix)
	c.src = src
	c.walk(fn.ChildByFieldName("body"))
	c.src, c.delta = savedSrc, savedDelta
}

// tokenCalls records name(...) inside the arguments of other macros. Their
// token tree is not parsed, so only a bare name directly followed by a
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
		call := lib.Call{Expr: name}
		if _, bound := c.find(name); !bound {
			if decl, ok := c.idx.function(c.at.d, c.at.mod, []string{name}); ok {
				ref := decl.ref
				call.Ref = &ref
			}
		}
		c.out = append(c.out, found{call: call, start: int(child.StartByte()) + c.delta, end: int(next.EndByte()) + c.delta})
	}
}
