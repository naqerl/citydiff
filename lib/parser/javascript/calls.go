package javascript

import (
	"regexp"
	"strings"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// resolve links the imports, then fills every function's and method's calls.
func resolve(drafts []*draft) {
	linkImports(drafts)
	for _, d := range drafts {
		for i, e := range d.entities {
			m := d.metas[i]
			if m.node == nil {
				continue
			}
			switch e := e.(type) {
			case lib.FunctionEntry:
				e.Calls = collect(d, m)
				d.entities[i] = e
			case lib.MethodEntry:
				e.Calls = collect(d, m)
				d.entities[i] = e
			}
		}
	}
}

// collector walks one body. locals are every name the function declares
// anywhere inside it (parameters included); a call through one of them
// does not reach a top-level declaration. typed are the locals bound once
// to new C() or documented as {C}, and never reassigned.
type collector struct {
	d      *draft
	m      meta
	locals map[string]int
	typed  map[string]string
	out    []lib.Call
}

func collect(d *draft, m meta) []lib.Call {
	c := &collector{d: d, m: m, locals: map[string]int{}, typed: map[string]string{}}
	fn := m.node
	if p := fn.ChildByFieldName("parameter"); p != nil {
		c.locals[c.text(p)]++
	}
	for _, p := range named(fn.ChildByFieldName("parameters")) {
		for _, id := range patternNames(p) {
			c.locals[c.text(id)]++
		}
		if p.Kind() == "rest_pattern" {
			for _, id := range patternNames(p) {
				c.locals[c.text(id)]++
			}
		}
	}
	for name, typ := range c.jsdoc(fn) {
		if c.locals[name] == 1 {
			c.typed[name] = typ
		}
	}
	body := fn.ChildByFieldName("body")
	c.declared(body)
	c.walk(body, false)
	if len(c.out) == 0 {
		return nil
	}
	return c.out
}

func (c *collector) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(c.d.src)
}

// declared gathers the names declared anywhere in n, and the ones bound
// once to new C().
func (c *collector) declared(n *tree_sitter.Node) {
	if n == nil {
		return
	}
	news := map[string]string{}
	assigned := map[string]bool{}
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		switch n.Kind() {
		case "variable_declarator":
			names := patternNames(n.ChildByFieldName("name"))
			for _, id := range names {
				c.locals[c.text(id)]++
			}
			if v := unparen(n.ChildByFieldName("value")); len(names) == 1 && n.ChildByFieldName("name").Kind() == "identifier" && v != nil && v.Kind() == "new_expression" {
				if ctor := v.ChildByFieldName("constructor"); ctor != nil && ctor.Kind() == "identifier" {
					news[c.text(names[0])] = c.text(ctor)
				}
			}
		case "function_declaration", "generator_function_declaration", "class_declaration":
			c.locals[c.text(n.ChildByFieldName("name"))]++
		case "catch_clause":
			for _, id := range patternNames(n.ChildByFieldName("parameter")) {
				c.locals[c.text(id)]++
			}
		case "assignment_expression", "augmented_assignment_expression":
			if l := unparen(n.ChildByFieldName("left")); l != nil && l.Kind() == "identifier" {
				assigned[c.text(l)] = true
			}
		case "arrow_function", "function_expression", "function", "generator_function", "method_definition":
			if p := n.ChildByFieldName("parameter"); p != nil {
				c.locals[c.text(p)]++
			}
			for _, p := range named(n.ChildByFieldName("parameters")) {
				for _, id := range patternNames(p) {
					c.locals[c.text(id)]++
				}
			}
			if name := n.ChildByFieldName("name"); name != nil && n.Kind() != "method_definition" {
				c.locals[c.text(name)]++
			}
		}
		for _, ch := range named(n) {
			visit(ch)
		}
	}
	visit(n)
	for name, typ := range news {
		if c.locals[name] == 1 && !assigned[name] {
			c.typed[name] = typ
		}
	}
	for name := range assigned {
		if _, ok := c.typed[name]; ok && news[name] == "" {
			delete(c.typed, name)
		}
	}
}

var jsdocParam = regexp.MustCompile(`@param\s+\{([A-Za-z_$][\w$]*)\}\s+\[?([A-Za-z_$][\w$]*)`)

// jsdoc reads @param {C} x from the comment right above the function.
func (c *collector) jsdoc(fn *tree_sitter.Node) map[string]string {
	n := fn
	for p := n.Parent(); p != nil; p = n.Parent() {
		k := p.Kind()
		if k == "program" || k == "class_body" || k == "statement_block" || k == "object" {
			break
		}
		n = p
	}
	out := map[string]string{}
	prev := n.PrevSibling()
	if prev == nil || prev.Kind() != "comment" || !strings.HasPrefix(c.text(prev), "/**") {
		return out
	}
	for _, m := range jsdocParam.FindAllStringSubmatch(c.text(prev), -1) {
		out[m[2]] = m[1]
	}
	return out
}

// walk records calls in source order: a call before the calls in its
// callee and arguments. inner is true inside a nested function that is not
// an arrow, where this is another object.
func (c *collector) walk(n *tree_sitter.Node, inner bool) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "call_expression":
		fn := unparen(n.ChildByFieldName("function"))
		if !(fn != nil && fn.Kind() == "identifier" && c.text(fn) == "require") && fn != nil && fn.Kind() != "import" {
			c.out = append(c.out, lib.Call{Expr: oneLine(c.text(fn)), Ref: c.call(fn, inner)})
		}
	case "new_expression":
		ctor := unparen(n.ChildByFieldName("constructor"))
		var ref *lib.CallRef
		if ctor != nil && ctor.Kind() == "identifier" && c.locals[c.text(ctor)] == 0 {
			ref = construct(local(c.d, c.text(ctor), 0), 0)
		}
		c.out = append(c.out, lib.Call{Expr: "new " + oneLine(c.text(ctor)), Ref: ref})
	case "function_expression", "function", "generator_function":
		inner = true
	case "class", "class_declaration":
		return
	}
	for _, ch := range named(n) {
		c.walk(ch, inner)
	}
}

var spaces = regexp.MustCompile(`\s*\n\s*`)

func oneLine(s string) string {
	s = spaces.ReplaceAllString(s, "")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// call resolves a callee: f, this.m, super.m, x.m, C.m and ns.f.
func (c *collector) call(fn *tree_sitter.Node, inner bool) *lib.CallRef {
	switch fn.Kind() {
	case "identifier":
		name := c.text(fn)
		if c.locals[name] > 0 {
			return nil
		}
		s := local(c.d, name, 0)
		switch s.kind {
		case symFunc:
			return &lib.CallRef{Path: s.d.path, Name: s.name}
		case symClass:
			// A constructor function that also owns methods.
			for _, e := range s.d.entities {
				if f, ok := e.(lib.FunctionEntry); ok && f.Name == s.name {
					return &lib.CallRef{Path: s.d.path, Name: s.name}
				}
			}
		}
		return nil
	case "member_expression":
		obj := unparen(fn.ChildByFieldName("object"))
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil || prop.Kind() != "property_identifier" {
			return nil
		}
		m := c.text(prop)
		switch obj.Kind() {
		case "this":
			if inner || c.m.owner == "" {
				return nil
			}
			return member(symbol{d: c.d, name: c.m.owner, kind: symClass}, m, c.m.static, 0)
		case "super":
			if inner || c.m.owner == "" {
				return nil
			}
			own := symbol{d: c.d, name: c.m.owner, kind: symClass}
			for i, e := range c.d.entities {
				if t, ok := e.(lib.TypeEntry); ok && t.Name == c.m.owner && c.d.metas[i].extends != "" {
					if parent := local(c.d, c.d.metas[i].extends, 0); parent.kind == symClass {
						return member(parent, m, c.m.static, 0)
					}
				}
			}
			_ = own
			return nil
		case "identifier":
			name := c.text(obj)
			if typ, ok := c.typed[name]; ok {
				if s := local(c.d, typ, 0); s.kind == symClass {
					return member(s, m, false, 0)
				}
				return nil
			}
			if c.locals[name] > 0 {
				return nil
			}
			s := local(c.d, name, 0)
			if s.kind == symClass || s.kind == symModule {
				return member(s, m, true, 0)
			}
		}
	}
	return nil
}
