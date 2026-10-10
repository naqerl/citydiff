package swift

import (
	"strings"
	"unicode"

	"citydiff/lib"
)

// tval is the inferred type of an expression. id names a type in the
// snapshot; empty is unknown. opt marks an Optional around it.
type tval struct {
	id  string
	opt bool
	// chain marks a value inside an optional chain (a?.b): members of id
	// apply, and the chain's result is optional.
	chain bool
}

// scope is where a type or signature was written. self is the id of the
// enclosing type; static is set inside a static member.
type scope struct {
	d      *draft
	self   string
	static bool
}

type fdecl struct {
	ref    lib.CallRef
	ret    string
	params []param
	at     scope
	where  int
	static bool
	// implicit marks a struct's memberwise initializer, which has no
	// declaration to point at: a call it may take stays unresolved.
	implicit bool
}

type typeDecl struct {
	kind    string
	at      scope
	fields  map[string]string
	inherit []string
	alias   string
}

type conformance struct {
	text string
	at   scope
}

type index struct {
	modules  map[string]bool
	types    map[string]*typeDecl
	funcs    map[string][]fdecl
	methods  map[string]map[string][]fdecl
	conforms map[string][]conformance
}

func modNS(root, module string) string { return root + "\x00" + module }

func rootOf(ns string) string {
	if i := strings.IndexByte(ns, 0); i >= 0 {
		return ns[:i]
	}
	return ns
}

func typeID(ns, full string) string { return ns + "\x00" + full }

// fullName is the type path of a type id: Outer.Inner.
func fullName(id string) string {
	if i := strings.LastIndexByte(id, 0); i >= 0 {
		return id[i+1:]
	}
	return id
}

func nsOf(id string) string {
	if i := strings.LastIndexByte(id, 0); i >= 0 {
		return id[:i]
	}
	return ""
}

func buildIndex(drafts []*draft) *index {
	idx := &index{
		modules:  map[string]bool{},
		types:    map[string]*typeDecl{},
		funcs:    map[string][]fdecl{},
		methods:  map[string]map[string][]fdecl{},
		conforms: map[string][]conformance{},
	}
	for _, d := range drafts {
		idx.modules[d.ns] = true
		for i, entity := range d.entities {
			typ, ok := entity.(lib.TypeEntry)
			if !ok {
				continue
			}
			m := d.metas[i]
			id := typeID(d.ns, typ.Name)
			owner := ""
			if m.owner != "" {
				owner = typeID(d.ns, m.owner)
			}
			idx.types[id] = &typeDecl{kind: m.kind, at: scope{d: d, self: owner}, fields: m.fields, inherit: m.inherit, alias: m.alias}
			if m.kind == "struct" && !declaresInit(d, typ.Name) {
				if idx.methods[id] == nil {
					idx.methods[id] = map[string][]fdecl{}
				}
				idx.methods[id]["init"] = append(idx.methods[id]["init"], fdecl{params: m.memberwise, implicit: true})
			}
		}
	}
	for _, d := range drafts {
		for i, entity := range d.entities {
			m := d.metas[i]
			switch entity := entity.(type) {
			case lib.FunctionEntry:
				key := d.ns + "\x00" + entity.Name
				idx.funcs[key] = append(idx.funcs[key], fdecl{
					ref:    lib.CallRef{Path: d.path, Name: entity.Name},
					ret:    m.ret,
					params: m.params,
					at:     scope{d: d},
				})
			case lib.MethodEntry:
				self := idx.selfType(d, m.owner)
				d.metas[i].owner = self
				if m.where == inConformance {
					for _, p := range conformsOf(entity.Type.Name) {
						idx.conforms[self] = append(idx.conforms[self], conformance{text: p, at: scope{d: d}})
					}
				}
				if idx.methods[self] == nil {
					idx.methods[self] = map[string][]fdecl{}
				}
				idx.methods[self][entity.Name] = append(idx.methods[self][entity.Name], fdecl{
					ref:    lib.CallRef{Path: d.path, Name: entity.Name, Recv: entity.Type.Name},
					ret:    m.ret,
					params: m.params,
					at:     scope{d: d, self: self, static: m.static},
					where:  m.where,
					static: m.static,
				})
			}
		}
	}
	return idx
}

// declaresInit reports whether a struct declares an initializer in its own
// body, which takes away the memberwise one. Extensions do not.
func declaresInit(d *draft, name string) bool {
	for i, e := range d.entities {
		if m, ok := e.(lib.MethodEntry); ok && m.Name == "init" && m.Type != nil && m.Type.Name == name && d.metas[i].where == inBody {
			return true
		}
	}
	return false
}

// conformsOf is the protocol list of a <T as P, Q> owner.
func conformsOf(owner string) []string {
	i := strings.Index(owner, " as ")
	if !strings.HasPrefix(owner, "<") || i < 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(owner[i+4:], ">"), ", ")
}

// selfType is the type id of a member's owner. A type outside the snapshot
// gets an id of its own so its members never match a snapshot type.
func (idx *index) selfType(d *draft, owner string) string {
	if id := idx.resolveType(scope{d: d}, owner); id != "" {
		return id
	}
	return "\x00ext\x00" + d.ns + "\x00" + owner
}

// cleanType drops attributes, inout, some/any and parentheses, and reports
// a trailing ? or ! as optional.
func cleanType(t string) (string, bool) {
	t = strings.TrimSpace(t)
	for {
		switch {
		case strings.HasPrefix(t, "@"):
			i := strings.IndexAny(t, " \t\n")
			if i < 0 {
				return "", false
			}
			t = strings.TrimSpace(t[i:])
		case strings.HasPrefix(t, "inout "), strings.HasPrefix(t, "some "), strings.HasPrefix(t, "any "):
			t = strings.TrimSpace(t[strings.IndexByte(t, ' '):])
		case strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") && !strings.Contains(t, ",") && !strings.Contains(t, "->"):
			t = strings.TrimSpace(t[1 : len(t)-1])
		default:
			opt := false
			for strings.HasSuffix(t, "?") || strings.HasSuffix(t, "!") {
				t = strings.TrimSpace(t[:len(t)-1])
				opt = true
			}
			if strings.HasPrefix(t, "Optional<") && strings.HasSuffix(t, ">") {
				t = t[len("Optional<") : len(t)-1]
				opt = true
			}
			return t, opt
		}
	}
}

// resolveType is the id of the snapshot type a written type names, or empty.
// A bare name is looked up in the enclosing types, then the module, then the
// package modules the file imports; several imported matches are ambiguous.
func (idx *index) resolveType(at scope, text string) string {
	t, _ := cleanType(text)
	if t == "" || strings.ContainsAny(t, "[]()-&:") {
		return ""
	}
	t = typeName(t)
	if t == "Self" {
		return at.self
	}
	segs := strings.Split(t, ".")
	if strings.HasPrefix(t, "Self.") && at.self != "" {
		return idx.alias(idx.typeIn(nsOf(at.self), fullName(at.self)+"."+strings.Join(segs[1:], ".")))
	}
	d := at.d
	if len(segs) > 1 {
		if ns := modNS(rootOf(d.ns), segs[0]); idx.modules[ns] && ns != d.ns {
			if id := idx.typeIn(ns, strings.Join(segs[1:], ".")); id != "" {
				return idx.alias(id)
			}
		}
	}
	if at.self != "" {
		ns := nsOf(at.self)
		chain := strings.Split(fullName(at.self), ".")
		for n := len(chain); n > 0; n-- {
			if id := idx.typeIn(ns, strings.Join(chain[:n], ".")+"."+t); id != "" {
				return idx.alias(id)
			}
			// A type's own name, and its supertypes' nested types.
		}
	}
	if id := idx.typeIn(d.ns, t); id != "" {
		return idx.alias(id)
	}
	found := ""
	for _, imp := range d.imports {
		ns := modNS(rootOf(d.ns), imp)
		if ns == d.ns || !idx.modules[ns] {
			continue
		}
		if id := idx.typeIn(ns, t); id != "" {
			if found != "" && found != id {
				return ""
			}
			found = id
		}
	}
	return idx.alias(found)
}

func (idx *index) typeIn(ns, full string) string {
	id := typeID(ns, full)
	if _, ok := idx.types[id]; ok {
		return id
	}
	return ""
}

// alias follows a typealias to the type it names. An alias of something
// outside the snapshot stays the alias.
func (idx *index) alias(id string) string {
	for range 4 {
		t := idx.types[id]
		if t == nil || t.kind != "typealias" || t.alias == "" {
			return id
		}
		next := idx.resolveType(t.at, t.alias)
		if next == "" || next == id {
			return id
		}
		id = next
	}
	return id
}

func (idx *index) convert(at scope, text string) tval {
	t, opt := cleanType(text)
	if t == "" {
		return tval{}
	}
	id := idx.resolveType(at, t)
	if id == "" {
		return tval{}
	}
	return tval{id: id, opt: opt}
}

func (idx *index) returns(f fdecl) tval {
	if f.ret == "" {
		return tval{}
	}
	return idx.convert(f.at, f.ret)
}

// arg is one call argument: its label and whether it is a trailing closure.
type arg struct {
	label    string
	trailing bool
}

// accepts reports whether a call with args can bind to params: labels in
// order, defaulted and variadic parameters may be left out, and a trailing
// closure fills the next parameter of function type.
func accepts(params []param, args []arg) bool {
	p := 0
	for i, a := range args {
		for {
			if p >= len(params) {
				if i > 0 && a.label == "" && !a.trailing && len(params) > 0 && params[len(params)-1].variadic {
					break
				}
				return false
			}
			cur := params[p]
			if a.trailing && (a.label == "" || a.label == cur.label) && (cur.fn || p == len(params)-1) {
				p++
				break
			}
			if !a.trailing && a.label == cur.label {
				if !cur.variadic {
					p++
				} else if i+1 >= len(args) || args[i+1].label != "" || args[i+1].trailing {
					p++
				}
				break
			}
			if !cur.optional {
				return false
			}
			p++
		}
	}
	for ; p < len(params); p++ {
		if !params[p].optional {
			return false
		}
	}
	return true
}

func pick(decls []fdecl, args []arg) []fdecl {
	var out []fdecl
	for _, d := range decls {
		if accepts(d.params, args) {
			out = append(out, d)
		}
	}
	return out
}

// function is the one top-level function a bare name with these arguments
// reaches: the file's own module first, then the imported package modules.
func (idx *index) function(d *draft, name string, args []arg) (fdecl, bool) {
	if hits := pick(idx.funcs[d.ns+"\x00"+name], args); len(hits) > 0 {
		if len(hits) == 1 {
			return hits[0], true
		}
		return fdecl{}, false
	}
	var hits []fdecl
	for _, imp := range d.imports {
		ns := modNS(rootOf(d.ns), imp)
		if ns == d.ns || !idx.modules[ns] {
			continue
		}
		hits = append(hits, pick(idx.funcs[ns+"\x00"+name], args)...)
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return fdecl{}, false
}

// moduleFunction is a function of a named module, as in Module.f().
func (idx *index) moduleFunction(d *draft, module, name string, args []arg) (fdecl, bool) {
	ns := modNS(rootOf(d.ns), module)
	if !idx.modules[ns] {
		return fdecl{}, false
	}
	if hits := pick(idx.funcs[ns+"\x00"+name], args); len(hits) == 1 {
		return hits[0], true
	}
	return fdecl{}, false
}

// method finds the member name of type id that a call with args reaches.
// static asks for a static member (a call on the type) or an instance one.
// The type's own body and plain extensions come first, then its conformance
// extensions, then its superclass, then the protocols it conforms to. At
// each step several matches are ambiguous and nothing resolves.
func (idx *index) method(id, name string, args []arg, static bool) (fdecl, bool) {
	return idx.methodIn(id, name, args, static, map[string]bool{})
}

func (idx *index) methodIn(id, name string, args []arg, static bool, seen map[string]bool) (fdecl, bool) {
	if id == "" || seen[id] {
		return fdecl{}, false
	}
	seen[id] = true
	var own, conf []fdecl
	for _, m := range pick(idx.methods[id][name], args) {
		if name != "init" && m.static != static {
			continue
		}
		if m.where == inConformance {
			conf = append(conf, m)
		} else {
			own = append(own, m)
		}
	}
	if f, ok, done := one(own); done {
		return f, ok
	}
	if f, ok, done := one(conf); done {
		return f, ok
	}
	if name == "init" {
		return fdecl{}, false
	}
	t := idx.types[id]
	var supers []string
	if t != nil {
		for _, text := range t.inherit {
			if sid := idx.resolveType(t.at, text); sid != "" {
				supers = append(supers, sid)
			}
		}
	}
	for _, c := range idx.conforms[id] {
		if sid := idx.resolveType(c.at, c.text); sid != "" {
			supers = append(supers, sid)
		}
	}
	var hits []fdecl
	for _, sid := range supers {
		if f, ok := idx.methodIn(sid, name, args, static, seen); ok {
			hits = append(hits, f)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return fdecl{}, false
}

// one is the single declaration in list. A protocol requirement wins over a
// default of the same name, as a call through the protocol dispatches to it.
func one(list []fdecl) (fdecl, bool, bool) {
	switch len(list) {
	case 0:
		return fdecl{}, false, false
	case 1:
		return list[0], !list[0].implicit, true
	}
	var reqs []fdecl
	for _, f := range list {
		if f.where == inRequirement {
			reqs = append(reqs, f)
		}
	}
	if len(reqs) == 1 {
		return reqs[0], true, true
	}
	return fdecl{}, false, true
}

// field is the type of property name of type t, its own or its supertypes'.
func (idx *index) field(t tval, name string) tval {
	if t.id == "" || t.opt {
		return tval{}
	}
	return idx.fieldIn(t.id, name, map[string]bool{})
}

func (idx *index) fieldIn(id, name string, seen map[string]bool) tval {
	td := idx.types[id]
	if td == nil || seen[id] {
		return tval{}
	}
	seen[id] = true
	if text, ok := td.fields[name]; ok {
		return idx.convert(scope{d: td.at.d, self: id}, text)
	}
	for _, m := range idx.methods[id][name] {
		if len(m.params) == 0 && m.ret != "" {
			return idx.convert(m.at, m.ret)
		}
	}
	for _, text := range td.inherit {
		if sid := idx.resolveType(td.at, text); sid != "" {
			if t := idx.fieldIn(sid, name, seen); t.id != "" {
				return t
			}
		}
	}
	return tval{}
}

// hasField reports whether type id or a supertype declares a stored
// property name.
func (idx *index) hasField(id, name string) bool {
	return idx.fieldIn(id, name, map[string]bool{}).id != "" || idx.declaresField(id, name, map[string]bool{})
}

func (idx *index) declaresField(id, name string, seen map[string]bool) bool {
	td := idx.types[id]
	if td == nil || seen[id] {
		return false
	}
	seen[id] = true
	if _, ok := td.fields[name]; ok {
		return true
	}
	for _, text := range td.inherit {
		if sid := idx.resolveType(td.at, text); sid != "" && idx.declaresField(sid, name, seen) {
			return true
		}
	}
	return false
}

func isUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}
