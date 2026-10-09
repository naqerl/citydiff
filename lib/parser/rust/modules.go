package rust

import (
	"bytes"
	"path"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// use is one leaf of a use tree. path is as written; alias is the name it
// binds, empty for a glob. mod is the module it sits in.
type use struct {
	path  []string
	alias string
	glob  bool
	mod   []string
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
// src/lib.rs and src/main.rs are the crate root of the Cargo package. A file
// directly under src/bin, tests, examples or benches is its own crate root.
// A file in a subdirectory there, such as tests/common/mod.rs, is a module
// shared by the crates of that directory.
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
		var target string
		switch {
		case ok && strings.HasPrefix(rel, "src/bin/"):
			target = "src/bin"
		case ok && strings.HasPrefix(rel, "src/"):
			d.module = moduleSegs(strings.TrimPrefix(stem, "src/"))
		case ok && strings.Contains(rel, "/"):
			target = rel[:strings.IndexByte(rel, '/')]
		case ok:
			target = "."
		default:
			d.module = moduleSegs(strings.TrimPrefix(stem, "src/"))
		}
		var targetPath []string
		if target != "" {
			d.ns += "\x00" + target
			rest := strings.TrimPrefix(stem, target+"/")
			segs := strings.Split(rest, "/")
			if len(segs) == 1 || (target == "src/bin" && len(segs) == 2 && segs[1] == "main") {
				targetPath = append(strings.Split(target, "/"), segs[0])
			} else {
				d.module = moduleSegs(rest)
				targetPath = strings.Split(target, "/")
			}
			if target == "." {
				targetPath = targetPath[1:]
			}
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
		parts := append(append([]string{d.crate}, targetPath...), d.module...)
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
