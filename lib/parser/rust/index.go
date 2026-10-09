package rust

import (
	"strings"

	"citydiff/lib"
)

// tval is the inferred type of an expression. id names a type in the
// snapshot; empty is unknown. wrap marks an Option or Result around it.
type tval struct {
	id   string
	wrap bool
}

// scope is where a type or signature was written, for resolving names in it.
type scope struct {
	d    *draft
	mod  []string
	self string
}

type fdecl struct {
	ref      lib.CallRef
	ret      string
	at       scope
	inherent bool
}

type fieldDecl struct {
	text string
	at   scope
}

type index struct {
	modules map[string]string
	libNS   map[string]string
	funcs   map[string][]fdecl
	types   map[string]bool
	aliases map[string]fieldDecl
	fields  map[string]map[string]fieldDecl
	methods map[string]map[string][]fdecl
}

func modKey(ns string, mod []string) string {
	return ns + "\x00\x00" + strings.Join(mod, "::")
}

func typeID(ns string, mod []string, name string) string {
	return modKey(ns, mod) + "\x00" + name
}

func fullMod(d *draft, m meta) []string {
	return append(append([]string{}, d.module...), m.mod...)
}

func bareName(name string) string {
	if i := strings.LastIndex(name, "::"); i >= 0 {
		return name[i+2:]
	}
	return name
}

func buildIndex(drafts []*draft) *index {
	idx := &index{
		modules: map[string]string{},
		libNS:   map[string]string{},
		funcs:   map[string][]fdecl{},
		types:   map[string]bool{},
		aliases: map[string]fieldDecl{},
		fields:  map[string]map[string]fieldDecl{},
		methods: map[string]map[string][]fdecl{},
	}
	for _, d := range drafts {
		if d.crate != "" && !strings.Contains(d.ns, "\x00") {
			idx.libNS[d.crate] = d.ns
		}
		idx.modules[modKey(d.ns, d.module)] = d.importPath
		for i, entity := range d.entities {
			mod := fullMod(d, d.metas[i])
			idx.modules[modKey(d.ns, mod)] = d.importPath
			if typ, ok := entity.(lib.TypeEntry); ok {
				id := typeID(d.ns, mod, bareName(typ.Name))
				idx.types[id] = true
				at := scope{d: d, mod: mod, self: id}
				if d.metas[i].ret != "" {
					idx.aliases[id] = fieldDecl{text: d.metas[i].ret, at: at}
				}
				for name, text := range d.metas[i].fields {
					if idx.fields[id] == nil {
						idx.fields[id] = map[string]fieldDecl{}
					}
					idx.fields[id][name] = fieldDecl{text: text, at: at}
				}
			}
		}
	}
	for _, d := range drafts {
		for i, entity := range d.entities {
			m := d.metas[i]
			mod := fullMod(d, m)
			switch entity := entity.(type) {
			case lib.FunctionEntry:
				key := typeID(d.ns, mod, bareName(entity.Name))
				idx.funcs[key] = append(idx.funcs[key], fdecl{
					ref: lib.CallRef{Path: d.path, Name: entity.Name},
					ret: m.ret,
					at:  scope{d: d, mod: mod},
				})
			case lib.MethodEntry:
				self := idx.selfType(d, mod, m.recv)
				d.metas[i].recv = self
				if idx.methods[self] == nil {
					idx.methods[self] = map[string][]fdecl{}
				}
				idx.methods[self][entity.Name] = append(idx.methods[self][entity.Name], fdecl{
					ref:      lib.CallRef{Path: d.path, Name: entity.Name, Recv: entity.Type.Name},
					ret:      m.ret,
					at:       scope{d: d, mod: mod, self: self},
					inherent: m.trait == "" && !m.inTrait,
				})
			}
		}
	}
	return idx
}

// selfType is the type id of an impl or trait. A type outside the snapshot
// gets an id of its own so its methods never match a snapshot type.
func (idx *index) selfType(d *draft, mod []string, recv string) string {
	if id := idx.resolveType(scope{d: d, mod: mod}, recv); id != "" {
		return id
	}
	return "\x00ext\x00" + d.ns + "\x00" + normType(recv)
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

// candidates are the absolute paths a path written in module cur may name, in
// order of preference. A single name also tries each glob import.
func (idx *index) candidates(d *draft, cur []string, segs []string) (first [][2]string, globs [][2]string) {
	ns, abs, ok := idx.absolute(d, cur, segs, true)
	if !ok {
		return nil, nil
	}
	first = append(first, [2]string{ns, strings.Join(abs, "::")})
	if len(segs) != 1 {
		return first, nil
	}
	for _, u := range d.uses {
		if !u.glob || !sameMod(u.mod, cur) {
			continue
		}
		if gns, gabs, ok := idx.absolute(d, u.mod, u.path[:len(u.path)-1], false); ok {
			globs = append(globs, [2]string{gns, strings.Join(append(gabs, segs[0]), "::")})
		}
	}
	return first, globs
}

func idOf(c [2]string) string {
	mod, name := "", c[1]
	if i := strings.LastIndex(c[1], "::"); i >= 0 {
		mod, name = c[1][:i], c[1][i+2:]
	}
	return c[0] + "\x00\x00" + mod + "\x00" + name
}

// find returns the first candidate id that has, or the single glob match.
func (idx *index) find(d *draft, cur, segs []string, has func(string) bool) string {
	first, globs := idx.candidates(d, cur, segs)
	for _, c := range first {
		if id := idOf(c); has(id) {
			return id
		}
	}
	found := ""
	for _, c := range globs {
		if id := idOf(c); has(id) {
			if found != "" && found != id {
				return ""
			}
			found = id
		}
	}
	return found
}

// resolveType is the id of the snapshot type a type expression names, or
// empty. Self is the scope's own type. An alias resolves to its target.
func (idx *index) resolveType(at scope, text string) string {
	t := stripRef(text)
	if i := strings.IndexByte(t, '<'); i >= 0 {
		t = t[:i]
	}
	segs := splitPath(t)
	if len(segs) == 0 {
		return ""
	}
	if len(segs) == 1 && segs[0] == "Self" {
		return at.self
	}
	id := idx.find(at.d, at.mod, segs, func(id string) bool { return idx.types[id] })
	for range 4 {
		alias, ok := idx.aliases[id]
		if !ok {
			break
		}
		next := idx.resolveType(alias.at, alias.text)
		if next == "" {
			return id
		}
		id = next
	}
	return id
}

// convert is the tval of a written type. Option<T>, Result<T, E> and their
// path forms such as io::Result<T> wrap T.
func (idx *index) convert(at scope, text string) tval {
	t := stripRef(text)
	if i := strings.IndexByte(t, '<'); i >= 0 && strings.HasSuffix(t, ">") {
		switch normType(t[:i]) {
		case "Option", "Result":
			inner := firstArg(t[i+1 : len(t)-1])
			if id := idx.resolveType(at, inner); id != "" {
				return tval{id: id, wrap: true}
			}
			return tval{}
		}
	}
	return tval{id: idx.resolveType(at, t)}
}

func firstArg(args string) string {
	depth := 0
	for i, r := range args {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(args[:i])
			}
		}
	}
	return strings.TrimSpace(args)
}

// function is the single function a path names, if any.
func (idx *index) function(d *draft, cur, segs []string) (fdecl, bool) {
	id := idx.find(d, cur, segs, func(id string) bool { return len(idx.funcs[id]) > 0 })
	if decls := idx.funcs[id]; len(decls) == 1 {
		return decls[0], true
	}
	return fdecl{}, false
}

// method picks the method a call on type id reaches. An inherent method wins,
// as in Rust. A trait method counts only when it is the single candidate.
func (idx *index) method(id, name string) (fdecl, bool) {
	if id == "" {
		return fdecl{}, false
	}
	var inherent, traits []fdecl
	for _, m := range idx.methods[id][name] {
		if m.inherent {
			inherent = append(inherent, m)
		} else {
			traits = append(traits, m)
		}
	}
	if len(inherent) == 1 {
		return inherent[0], true
	}
	if len(inherent) == 0 && len(traits) == 1 {
		return traits[0], true
	}
	return fdecl{}, false
}

func (idx *index) field(t tval, name string) tval {
	if t.id == "" || t.wrap {
		return tval{}
	}
	f, ok := idx.fields[t.id][name]
	if !ok {
		return tval{}
	}
	return idx.convert(f.at, f.text)
}

func (idx *index) returns(f fdecl) tval {
	if f.ret == "" {
		return tval{}
	}
	return idx.convert(f.at, f.ret)
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
