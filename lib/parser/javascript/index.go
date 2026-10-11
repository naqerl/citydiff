package javascript

import (
	"citydiff/lib"
)

// symbol is what a name in a file stands for: a function, a class (or an
// object that owns methods), or a whole module.
type symbol struct {
	d    *draft
	name string
	kind int
}

const (
	symNone = iota
	symFunc
	symClass
	symModule
)

// local is the symbol a top-level name of d stands for, following imports.
func local(d *draft, name string, hops int) symbol {
	if hops > 8 || name == "" {
		return symbol{}
	}
	funcs, types := 0, 0
	for _, e := range d.entities {
		switch e := e.(type) {
		case lib.FunctionEntry:
			if e.Name == name {
				funcs++
			}
		case lib.TypeEntry:
			if e.Name == name {
				types++
			}
		}
	}
	switch {
	case types == 1:
		return symbol{d: d, name: name, kind: symClass}
	case types == 0 && funcs == 1:
		return symbol{d: d, name: name, kind: symFunc}
	case types+funcs > 1:
		return symbol{}
	}
	b, ok := d.bindings[name]
	if !ok {
		return symbol{}
	}
	t := d.targets[b.spec]
	if t == nil {
		return symbol{}
	}
	if b.export == "" {
		return whole(t, b.require, hops+1)
	}
	return exported(t, b.export, hops+1)
}

// exported is the symbol behind an exported name of d: its own export, a
// re-export, or the one export * source that has it.
func exported(d *draft, name string, hops int) symbol {
	if hops > 8 {
		return symbol{}
	}
	if l, ok := d.exports[name]; ok {
		return local(d, l, hops+1)
	}
	if b, ok := d.reexp[name]; ok {
		t := d.targets[b.spec]
		if t == nil {
			return symbol{}
		}
		if b.export == "" {
			return whole(t, b.require, hops+1)
		}
		return exported(t, b.export, hops+1)
	}
	if name == "default" {
		return symbol{}
	}
	var found []symbol
	for _, spec := range d.stars {
		if t := d.targets[spec]; t != nil {
			if s := exported(t, name, hops+1); s.kind != symNone {
				found = append(found, s)
			}
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return symbol{}
}

// member is m on the symbol: a module's export, or a class's method with
// the given staticness, looking through what the class extends.
func member(s symbol, m string, static bool, hops int) *lib.CallRef {
	if hops > 8 {
		return nil
	}
	switch s.kind {
	case symModule:
		t := exported(s.d, m, hops+1)
		if t.kind == symFunc {
			return &lib.CallRef{Path: t.d.path, Name: t.name}
		}
	case symClass:
		var hits []*lib.CallRef
		extends := ""
		for i, e := range s.d.entities {
			switch e := e.(type) {
			case lib.MethodEntry:
				if e.Type != nil && e.Type.Name == s.name && e.Name == m && s.d.metas[i].static == static {
					hits = append(hits, &lib.CallRef{Path: s.d.path, Name: m, Recv: s.name})
				}
			case lib.TypeEntry:
				if e.Name == s.name {
					extends = s.d.metas[i].extends
				}
			}
		}
		if len(hits) == 1 {
			return hits[0]
		}
		if len(hits) == 0 && extends != "" {
			if parent := local(s.d, extends, hops+1); parent.kind == symClass {
				return member(parent, m, static, hops+1)
			}
		}
	}
	return nil
}

// construct is what new C() runs: C's constructor (or the nearest one it
// inherits), or C itself when C is a constructor function.
func construct(s symbol, hops int) *lib.CallRef {
	switch s.kind {
	case symFunc:
		return &lib.CallRef{Path: s.d.path, Name: s.name}
	case symClass:
		if ref := member(s, "constructor", false, hops); ref != nil {
			return ref
		}
		// A function constructor with prototype methods is a type and a
		// function of the same name.
		for _, e := range s.d.entities {
			if f, ok := e.(lib.FunctionEntry); ok && f.Name == s.name {
				return &lib.CallRef{Path: s.d.path, Name: s.name}
			}
		}
	}
	return nil
}

// whole is what a module stands for as one value: for require, its
// module.exports when that was set (module.exports = X), else its exports
// as a namespace.
func whole(t *draft, require bool, hops int) symbol {
	if require {
		_, own := t.exports["default"]
		b, passed := t.reexp["default"]
		if passed && b.require && !own {
			if next := t.targets[b.spec]; next != nil && hops < 8 {
				return whole(next, true, hops+1)
			}
			return symbol{}
		}
		if own {
			return exported(t, "default", hops+1)
		}
	}
	return symbol{d: t, kind: symModule}
}
