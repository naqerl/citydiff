package javascript

import (
	"encoding/json"
	"path"
	"strings"

	"citydiff/lib"
)

// manifest is one package.json: its directory and package name.
type manifest struct {
	dir, name string
}

func readManifest(dir string, src []byte) manifest {
	var pkg struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(src, &pkg)
	return manifest{dir: dir, name: pkg.Name}
}

func under(p, dir string) bool {
	return dir == "." || p == dir || strings.HasPrefix(p, dir+"/")
}

// assignModules gives each file its module: the directory it is in. The
// root is the nearest package.json with a name, whose name prefixes the
// module path (express, express/lib/router); without one the module is the
// directory itself. Package is the directory's base name, or the package
// name at its root.
func assignModules(drafts []*draft, manifests []manifest) {
	for _, d := range drafts {
		dir := path.Dir(d.path)
		var best *manifest
		for i := range manifests {
			m := &manifests[i]
			if m.name == "" || !under(dir, m.dir) {
				continue
			}
			if best == nil || len(m.dir) > len(best.dir) {
				best = m
			}
		}
		if best == nil {
			d.importPath = dir
			if dir == "." {
				d.importPath = ""
				d.pkg = "root"
			} else {
				d.pkg = path.Base(dir)
			}
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(dir, best.dir), "/")
		if best.dir == "." {
			rel = dir
		}
		d.module = best.name
		d.importPath = best.name
		d.pkg = best.name
		if rel != "" && rel != "." {
			d.importPath = best.name + "/" + rel
			d.pkg = path.Base(rel)
		}
	}
}

// lookup resolves a relative specifier from a file to a snapshot file, the
// way Node does for a path: as written, with an extension, or as a
// directory's index file.
func lookup(byPath map[string]*draft, from, spec string) *draft {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && spec != "." && spec != ".." {
		return nil
	}
	base := path.Join(path.Dir(from), spec)
	if strings.HasPrefix(base, "../") || base == ".." {
		return nil
	}
	if base == "." {
		base = ""
	}
	cands := []string{base}
	for _, ext := range Extensions {
		cands = append(cands, base+ext)
	}
	for _, ext := range Extensions {
		cands = append(cands, strings.TrimPrefix(base+"/index"+ext, "/"))
	}
	for _, c := range cands {
		if d := byPath[c]; d != nil {
			return d
		}
	}
	return nil
}

// moduleID is what an import entity names: a project module, or the bare
// package (lodash/fp is lodash) and Node builtins as written.
func moduleID(d *draft) string {
	if d.importPath != "" {
		return d.importPath
	}
	return d.pkg
}

func externalID(spec string) string {
	if strings.HasPrefix(spec, ".") {
		return spec
	}
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// linkImports resolves every specifier and rewrites the import entities to
// module ids, dropping repeats and imports of the file's own module.
func linkImports(drafts []*draft) {
	byPath := map[string]*draft{}
	for _, d := range drafts {
		byPath[d.path] = d
	}
	for _, d := range drafts {
		d.targets = map[string]*draft{}
		seen := map[string]bool{}
		var ents []lib.Entity
		var metas []meta
		for i, e := range d.entities {
			imp, ok := e.(lib.ImportEntry)
			if !ok {
				ents = append(ents, e)
				metas = append(metas, d.metas[i])
				continue
			}
			id := externalID(imp.Path)
			if t := lookup(byPath, d.path, imp.Path); t != nil {
				d.targets[imp.Path] = t
				id = moduleID(t)
				if t.importPath == d.importPath {
					continue
				}
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ents = append(ents, lib.ImportEntry{Path: id})
			metas = append(metas, meta{})
		}
		d.entities, d.metas = ents, metas
	}
}
