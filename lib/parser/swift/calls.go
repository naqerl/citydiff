package swift

import (
	"sort"
	"strings"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// binding is a local name. A name with no known type keeps a method call on
// it unresolved, and shadows a member or function of the same name.
type binding struct {
	t tval
}

type found struct {
	call  lib.Call
	start int
	end   int
}

func resolve(drafts []*draft) {
	idx := buildIndex(drafts)
	for _, d := range drafts {
		for i, entity := range d.entities {
			m := d.metas[i]
			switch entity := entity.(type) {
			case lib.ImportEntry:
				// A package module keeps its name, which is its ImportPath.
				if ns := modNS(rootOf(d.ns), entity.Path); idx.modules[ns] {
					if root := rootOf(d.ns); root != "." {
						d.entities[i] = lib.ImportEntry{Path: root + "/" + entity.Path}
					}
				}
			case lib.FunctionEntry:
				entity.Calls = collect(idx, d, m, scope{d: d})
				d.entities[i] = entity
			case lib.MethodEntry:
				entity.Calls = collect(idx, d, m, scope{d: d, self: m.owner, static: m.static})
				d.entities[i] = entity
			}
		}
	}
}

type collector struct {
	idx    *index
	src    []byte
	at     scope
	scopes []map[string]binding
	out    []found
}

func bodyOf(n *tree_sitter.Node) *tree_sitter.Node {
	switch n.Kind() {
	case "property_declaration":
		return n.ChildByFieldName("computed_value")
	case "subscript_declaration":
		return childOfKind(n, "computed_property")
	}
	return n.ChildByFieldName("body")
}

func collect(idx *index, d *draft, m meta, at scope) []lib.Call {
	if m.node == nil {
		return nil
	}
	body := bodyOf(m.node)
	if body == nil {
		return nil
	}
	c := &collector{idx: idx, src: d.src, at: at, scopes: []map[string]binding{{}}}
	for _, p := range m.params {
		if p.name != "" {
			c.scopes[0][p.name] = binding{t: idx.convert(at, p.typ)}
		}
	}
	if m.node.Kind() == "property_declaration" || m.node.Kind() == "subscript_declaration" {
		c.scopes[0]["newValue"] = binding{}
		c.scopes[0]["oldValue"] = binding{}
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

func (c *collector) bindName(name string, t tval) {
	if name != "" && name != "_" {
		c.scopes[len(c.scopes)-1][name] = binding{t: t}
	}
}

// bindAll binds every identifier a pattern introduces, with no type.
func (c *collector) bindAll(n *tree_sitter.Node) {
	if n == nil {
		return
	}
	if n.Kind() == "simple_identifier" {
		c.bindName(c.text(n), tval{})
		return
	}
	cursor := n.Walk()
	defer cursor.Close()
	for _, ch := range n.NamedChildren(cursor) {
		switch ch.Kind() {
		case "simple_identifier", "pattern", "value_binding_pattern", "tuple_pattern", "tuple_pattern_item":
			c.bindAll(&ch)
		}
	}
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
	c.out = append(c.out, found{call: call, start: int(n.StartByte()), end: int(n.EndByte())})
}

func (c *collector) children(n *tree_sitter.Node) {
	cursor := n.Walk()
	defer cursor.Close()
	for _, child := range n.NamedChildren(cursor) {
		c.walk(&child)
	}
}

func (c *collector) walk(n *tree_sitter.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "class_declaration", "protocol_declaration", "import_declaration", "typealias_declaration":
		return
	case "statements", "switch_entry", "catch_block", "do_statement":
		defer c.push()()
		if n.Kind() == "switch_entry" {
			c.bindPatterns(n)
		}
		if n.Kind() == "catch_block" {
			c.bindName("error", tval{})
		}
	case "function_declaration":
		// A local function: its name shadows, and its calls count here.
		c.bindName(c.text(n.ChildByFieldName("name")), tval{})
		defer c.push()()
		w := walker{src: c.src}
		for _, p := range w.params(n) {
			c.bindName(p.name, c.idx.convert(c.at, p.typ))
		}
		c.walk(n.ChildByFieldName("body"))
		return
	case "lambda_literal":
		defer c.push()()
		c.bindName("$0", tval{})
		if typ := n.ChildByFieldName("type"); typ != nil {
			c.bindAll(childOfKind(typ, "lambda_function_type_parameters"))
			if params := childOfKind(typ, "lambda_function_type_parameters"); params != nil {
				cursor := params.Walk()
				for _, p := range params.NamedChildren(cursor) {
					name := p.ChildByFieldName("name")
					t := tval{}
					if ty := p.ChildByFieldName("type"); ty != nil {
						t = c.idx.convert(c.at, c.text(ty))
					}
					c.bindName(c.text(name), t)
				}
				cursor.Close()
			}
		}
		c.children(n)
		return
	case "property_declaration":
		value := n.ChildByFieldName("value")
		c.walk(value)
		c.walk(n.ChildByFieldName("computed_value"))
		w := walker{src: c.src}
		names := w.bound(n)
		var t tval
		if len(names) == 1 {
			if ann := w.annotation(n); ann != "" {
				t = c.idx.convert(c.at, ann)
			} else {
				t = c.typeOf(value)
			}
		}
		for _, name := range names {
			c.bindName(name, t)
		}
		cursor := n.Walk()
		for _, p := range n.ChildrenByFieldName("name", cursor) {
			if p.Kind() == "pattern" && p.ChildByFieldName("bound_identifier") == nil {
				c.bindAll(&p)
			}
		}
		cursor.Close()
		return
	case "if_statement", "guard_statement", "while_statement":
		if n.Kind() != "guard_statement" {
			defer c.push()()
		}
		c.conditions(n)
		return
	case "for_statement":
		c.walk(n.ChildByFieldName("collection"))
		defer c.push()()
		c.bindAll(n.ChildByFieldName("item"))
		cursor := n.Walk()
		for _, ch := range n.NamedChildren(cursor) {
			if ch.Kind() == "statements" || ch.Kind() == "where_clause" {
				c.walk(&ch)
			}
		}
		cursor.Close()
		return
	case "call_expression":
		c.call(n)
		return
	}
	c.children(n)
}

// bindPatterns binds the names a switch case introduces.
func (c *collector) bindPatterns(n *tree_sitter.Node) {
	cursor := n.Walk()
	defer cursor.Close()
	for _, ch := range n.NamedChildren(cursor) {
		if ch.Kind() == "switch_pattern" {
			c.patternNames(&ch)
		}
	}
}

func (c *collector) patternNames(n *tree_sitter.Node) {
	if id := n.ChildByFieldName("bound_identifier"); id != nil {
		c.bindName(c.text(id), tval{})
	}
	cursor := n.Walk()
	defer cursor.Close()
	for _, ch := range n.NamedChildren(cursor) {
		c.patternNames(&ch)
	}
}

// conditions walks if/guard/while conditions in order. if let x = value
// binds x to value's type without the Optional; if let x is shorthand for
// if let x = x. The body is walked after the bindings.
func (c *collector) conditions(n *tree_sitter.Node) {
	pending := ""
	flush := func(t tval) {
		if pending != "" {
			t.opt, t.chain = false, false
			c.bindName(pending, t)
			pending = ""
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if !ch.IsNamed() {
			continue
		}
		switch n.FieldNameForChild(uint32(i)) {
		case "bound_identifier":
			if pending != "" {
				flush(c.nameType(pending))
			}
			pending = c.text(ch)
			continue
		case "condition":
			if ch.Kind() == "value_binding_pattern" {
				continue
			}
			c.walk(ch)
			if pending != "" {
				flush(c.typeOf(ch))
			}
			continue
		}
		if pending != "" {
			flush(c.nameType(pending))
		}
		c.walk(ch)
	}
	if pending != "" {
		flush(c.nameType(pending))
	}
}

// args reads the labels of a call: its value arguments, then its trailing
// closures, the first unlabeled and later ones labeled.
func (c *collector) args(suffix *tree_sitter.Node) []arg {
	var out []arg
	if suffix == nil {
		return out
	}
	label := ""
	for i := uint(0); i < suffix.ChildCount(); i++ {
		ch := suffix.Child(i)
		switch ch.Kind() {
		case "value_arguments":
			cursor := ch.Walk()
			for _, v := range ch.NamedChildren(cursor) {
				if v.Kind() != "value_argument" {
					continue
				}
				a := arg{}
				if l := v.ChildByFieldName("name"); l != nil {
					a.label = strings.TrimSpace(c.text(l))
				}
				out = append(out, a)
			}
			cursor.Close()
		case "simple_identifier":
			label = c.text(ch)
		case "lambda_literal":
			out = append(out, arg{label: label, trailing: true})
			label = ""
		}
	}
	return out
}

func (c *collector) call(n *tree_sitter.Node) {
	fn := n.NamedChild(0)
	suffix := childOfKind(n, "call_suffix")
	call, _ := c.callee(fn, c.args(suffix))
	c.record(n, call)
	c.walk(fn)
	c.walk(suffix)
}

// oneLine is a callee as written on one line: a line break and the
// indentation around it are dropped, other spaces kept single.
func oneLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.Join(lines, "")
}

// callee resolves the function part of a call and returns the call and the
// type it returns.
func (c *collector) callee(fn *tree_sitter.Node, args []arg) (lib.Call, tval) {
	call := lib.Call{Expr: oneLine(c.text(fn))}
	if fn == nil {
		return call, tval{}
	}
	var decl fdecl
	ok := false
	var ret tval
	switch fn.Kind() {
	case "simple_identifier":
		name := c.text(fn)
		if _, bound := c.find(name); bound {
			return call, tval{}
		}
		if c.at.self != "" {
			if decl, ok = c.idx.method(c.at.self, name, args, c.at.static); ok {
				break
			}
			if c.idx.hasField(c.at.self, name) {
				return call, tval{}
			}
		}
		if isUpper(name) {
			if id := c.idx.resolveType(c.at, name); id != "" {
				ret = tval{id: id}
				decl, ok = c.idx.method(id, "init", args, false)
				break
			}
		}
		decl, ok = c.idx.function(c.at.d, name, args)
	case "navigation_expression":
		target := fn.ChildByFieldName("target")
		name := c.suffixName(fn)
		if target == nil || name == "" {
			return call, tval{}
		}
		switch target.Kind() {
		case "self_expression":
			if name == "init" {
				decl, ok = c.idx.method(c.at.self, "init", args, false)
				break
			}
			decl, ok = c.idx.method(c.at.self, name, args, c.at.static)
		case "super_expression":
			for _, sup := range c.supers() {
				if decl, ok = c.idx.method(sup, name, args, c.at.static); ok {
					break
				}
			}
		default:
			if id, isType := c.typeRef(fn); isType && id != "" {
				ret = tval{id: id}
				decl, ok = c.idx.method(id, "init", args, false)
				break
			}
			if id, isType := c.typeRef(target); isType {
				if id == "" {
					if module := c.text(target); c.idx.modules[modNS(rootOf(c.at.d.ns), module)] {
						decl, ok = c.idx.moduleFunction(c.at.d, module, name, args)
					}
					break
				}
				if name == "init" {
					ret = tval{id: id}
					decl, ok = c.idx.method(id, "init", args, false)
					break
				}
				decl, ok = c.idx.method(id, name, args, true)
				break
			}
			recv := c.typeOf(target)
			if recv.opt && c.chained(fn) {
				recv.opt, recv.chain = false, true
			}
			if recv.opt || recv.id == "" {
				return call, tval{}
			}
			decl, ok = c.idx.method(recv.id, name, args, false)
			if ok {
				ref := decl.ref
				call.Ref = &ref
				t := c.idx.returns(decl)
				t.chain = t.chain || recv.chain
				return call, t
			}
		}
	case "lambda_literal":
		return lib.Call{Expr: "closure"}, tval{}
	}
	if !ok {
		return call, ret
	}
	ref := decl.ref
	call.Ref = &ref
	if ret.id != "" {
		return call, ret
	}
	return call, c.idx.returns(decl)
}

// supers are the resolved supertypes of the enclosing type.
func (c *collector) supers() []string {
	t := c.idx.types[c.at.self]
	if t == nil {
		return nil
	}
	var out []string
	for _, text := range t.inherit {
		if id := c.idx.resolveType(t.at, text); id != "" && c.idx.types[id].kind == "class" {
			out = append(out, id)
		}
	}
	return out
}

func (c *collector) suffixName(nav *tree_sitter.Node) string {
	s := nav.ChildByFieldName("suffix")
	if s == nil {
		return ""
	}
	if id := s.ChildByFieldName("suffix"); id != nil && id.Kind() == "simple_identifier" {
		return c.text(id)
	}
	return ""
}

// chained reports whether a member access is optional chaining: a ? right
// after the target, as in x?.m().
func (c *collector) chained(nav *tree_sitter.Node) bool {
	target := nav.ChildByFieldName("target")
	s := nav.ChildByFieldName("suffix")
	if target == nil || s == nil {
		return false
	}
	return strings.Contains(string(c.src[target.EndByte():s.StartByte()+1]), "?") ||
		strings.HasPrefix(strings.TrimSpace(c.text(s)), "?")
}

// typeRef reports whether an expression names a type, or a module (id empty).
// It is a type only when no local, member or property shadows the name.
func (c *collector) typeRef(n *tree_sitter.Node) (string, bool) {
	text := strings.Join(strings.Fields(c.text(n)), "")
	if text == "" || !isUpper(text) {
		return "", false
	}
	for _, seg := range strings.Split(text, ".") {
		if !isIdentWord(seg) {
			return "", false
		}
	}
	first := strings.SplitN(text, ".", 2)[0]
	if _, bound := c.find(first); bound {
		return "", false
	}
	if c.at.self != "" && c.idx.hasField(c.at.self, first) {
		return "", false
	}
	if id := c.idx.resolveType(c.at, text); id != "" {
		return id, true
	}
	if !strings.Contains(text, ".") && c.idx.modules[modNS(rootOf(c.at.d.ns), text)] {
		return "", true
	}
	return "", false
}

func isIdentWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// nameType is the type of a bare name: a local, or a property of self.
func (c *collector) nameType(name string) tval {
	if b, ok := c.find(name); ok {
		return b.t
	}
	if c.at.self != "" && !c.at.static {
		return c.idx.field(tval{id: c.at.self}, name)
	}
	return tval{}
}

// typeOf infers the type of an expression from bindings, initializer calls,
// annotations, property types and return types. Anything else is unknown.
func (c *collector) typeOf(n *tree_sitter.Node) tval {
	if n == nil {
		return tval{}
	}
	switch n.Kind() {
	case "simple_identifier":
		return c.nameType(c.text(n))
	case "self_expression":
		if c.at.static {
			return tval{}
		}
		return tval{id: c.at.self}
	case "call_expression":
		_, t := c.callee(n.NamedChild(0), c.args(childOfKind(n, "call_suffix")))
		return t
	case "navigation_expression":
		target := n.ChildByFieldName("target")
		name := c.suffixName(n)
		if target == nil || name == "" {
			return tval{}
		}
		if id, isType := c.typeRef(target); isType {
			if id == "" {
				return tval{}
			}
			return c.idx.field(tval{id: id}, name)
		}
		recv := c.typeOf(target)
		if recv.opt && c.chained(n) {
			recv.opt, recv.chain = false, true
		}
		t := c.idx.field(recv, name)
		t.chain = t.id != "" && (t.chain || recv.chain)
		return t
	case "postfix_expression":
		if op := n.ChildByFieldName("operation"); op != nil && c.text(op) == "!" {
			t := c.typeOf(n.ChildByFieldName("target"))
			t.opt = false
			return t
		}
	case "try_expression":
		t := c.typeOf(n.ChildByFieldName("expr"))
		if op := n.ChildByFieldName("try_operator"); op == nil {
			cursor := n.Walk()
			for _, ch := range n.NamedChildren(cursor) {
				if ch.Kind() == "try_operator" && c.text(&ch) == "try?" {
					t.opt = t.id != ""
				}
			}
			cursor.Close()
		} else if c.text(op) == "try?" {
			t.opt = t.id != ""
		}
		return t
	case "await_expression":
		return c.typeOf(n.ChildByFieldName("expr"))
	case "tuple_expression", "parenthesized_expression":
		if n.NamedChildCount() == 1 {
			return c.typeOf(n.NamedChild(0))
		}
	case "as_expression":
		// x as T and x as! T are a T; x as? T is an Optional T.
		cursor := n.Walk()
		defer cursor.Close()
		kids := n.NamedChildren(cursor)
		if len(kids) < 2 {
			return tval{}
		}
		t := c.idx.convert(c.at, c.text(&kids[len(kids)-1]))
		if t.id != "" && strings.Contains(c.text(n), " as? ") {
			t.opt = true
		}
		return t
	}
	return tval{}
}
