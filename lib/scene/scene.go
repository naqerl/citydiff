// Package scene builds the document the 3D view draws.
//
// A snapshot with a nil left side is the project on its own: every package
// is unchanged. A non-nil left side, even an empty one, is a diff. The view
// draws the right side as the city and lays the diff on top of it.
package scene

import (
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"citydiff/lib"
	"citydiff/lib/diff"
)

const (
	same     = "same"
	added    = "added"
	removed  = "removed"
	modified = "modified"
)

// Scene is one city. Root is the module package the others sit inside.
// Diff is false when the scene is a single snapshot.
type Scene struct {
	Module   string    `json:"module,omitempty"`
	Root     string    `json:"root,omitempty"`
	Diff     bool      `json:"diff"`
	Packages []Package `json:"packages"`
}

// Package is one Go package, or a synthetic module root that only holds
// other packages, or an external import target with no source in the snapshot.
type Package struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Package   string   `json:"package,omitempty"`
	Dir       string   `json:"dir,omitempty"`
	Parent    string   `json:"parent,omitempty"`
	Change    string   `json:"change"`
	External  bool     `json:"external,omitempty"`
	Synthetic bool     `json:"synthetic,omitempty"`
	Entities  []Entity `json:"entities,omitempty"`
	Deps      []Dep    `json:"deps,omitempty"`
}

// Dep is one import edge from this package.
// Files is how many of its files import that path on the side that still has the edge.
type Dep struct {
	To       string `json:"to"`
	Change   string `json:"change"`
	External bool   `json:"external,omitempty"`
	Files    int    `json:"files,omitempty"`
}

// Entity is one declaration inside a package.
// Parent is the type a method belongs to, when that type is in the same package.
// BodyBytes is the right-hand body length. BodyBytesBefore is the left-hand
// length for a function or method that exists on the left.
type Entity struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Name            string     `json:"name"`
	Recv            string     `json:"recv,omitempty"`
	File            string     `json:"file"`
	Parent          string     `json:"parent,omitempty"`
	Change          string     `json:"change"`
	Fields          []string   `json:"fields,omitempty"`
	BodyBytes       int        `json:"bodyBytes,omitempty"`
	BodyBytesBefore *int       `json:"bodyBytesBefore,omitempty"`
	Calls           []CallStep `json:"calls,omitempty"`
}

// CallStep is one call in source order after the two sides are aligned.
type CallStep struct {
	Change   string `json:"change"`
	Expr     string `json:"expr"`
	Path     string `json:"path,omitempty"`
	Name     string `json:"name,omitempty"`
	Recv     string `json:"recv,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
}

type group struct {
	id        string
	pkgName   string
	dir       string
	module    string
	synthetic bool
	left      []lib.ParsedFile
	right     []lib.ParsedFile
}

// Build assembles a scene from the two sides of a range.
// A nil left side is one snapshot. An empty left slice is a diff against nothing.
func Build(left, right []lib.ParsedFile) Scene {
	diffing := left != nil
	groups := map[string]*group{}
	addFiles(groups, left, true)
	addFiles(groups, right, false)
	ensureModuleRoots(groups)

	ids := make(map[string]bool, len(groups))
	for id := range groups {
		ids[id] = true
	}

	out := Scene{Diff: diffing, Packages: []Package{}}
	modules := map[string]bool{}
	for _, g := range groups {
		if g.module != "" {
			modules[g.module] = true
		}
	}
	if len(modules) == 1 {
		for mod := range modules {
			out.Module = mod
			out.Root = mod
		}
	}

	var pkgs []Package
	var externalUses []depUse
	for _, g := range groups {
		pkg, uses := buildPackage(g, ids, diffing)
		pkgs = append(pkgs, pkg)
		externalUses = append(externalUses, uses...)
	}
	pkgs = append(pkgs, externalPackages(externalUses, diffing)...)
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ID < pkgs[j].ID })
	out.Packages = pkgs
	return out
}

func addFiles(groups map[string]*group, files []lib.ParsedFile, left bool) {
	for _, file := range files {
		id := packageID(file)
		g := groups[id]
		if g == nil {
			g = &group{id: id, dir: path.Dir(file.Path)}
			groups[id] = g
		}
		g.pkgName = preferName(g.pkgName, file.Package)
		if file.Module != "" {
			g.module = file.Module
		}
		if dir := fileDir(file.Path); g.dir == "" || g.dir == "." {
			g.dir = dir
		}
		if left {
			g.left = append(g.left, file)
		} else {
			g.right = append(g.right, file)
		}
	}
}

// fileDir is the snapshot directory of a file. The module root is ".".
func fileDir(p string) string {
	d := path.Dir(p)
	if d == "" {
		return "."
	}
	return d
}

func packageID(file lib.ParsedFile) string {
	if file.ImportPath != "" {
		return file.ImportPath
	}
	dir := fileDir(file.Path)
	if dir != "." {
		return dir
	}
	if file.Package != "" {
		return file.Package
	}
	return "."
}

func preferName(cur, next string) string {
	if cur == "" || (strings.HasSuffix(cur, "_test") && !strings.HasSuffix(next, "_test") && next != "") {
		return next
	}
	return cur
}

func ensureModuleRoots(groups map[string]*group) {
	need := map[string]string{}
	for _, g := range groups {
		if g.module != "" {
			need[g.module] = g.module
		}
	}
	for mod := range need {
		if _, ok := groups[mod]; ok {
			continue
		}
		groups[mod] = &group{
			id:        mod,
			dir:       ".",
			module:    mod,
			synthetic: true,
		}
	}
}

type depUse struct {
	to     string
	change string
}

func buildPackage(g *group, ids map[string]bool, diffing bool) (Package, []depUse) {
	name := g.pkgName
	if name == "" {
		name = path.Base(g.id)
	}
	pkg := Package{
		ID:        g.id,
		Name:      name,
		Package:   g.pkgName,
		Dir:       g.dir,
		Parent:    parentOf(g.id, ids),
		Change:    same,
		Synthetic: g.synthetic,
	}
	if g.synthetic {
		return pkg, nil
	}
	leftImports := importCounts(g.left)
	rightImports := importCounts(g.right)
	pkg.Entities = packageEntities(g, diffing)
	pkg.Deps = packageDeps(g.id, leftImports, rightImports, ids, diffing)
	pkg.Change = packageChange(g, diffing)
	var uses []depUse
	for _, dep := range pkg.Deps {
		if dep.External {
			uses = append(uses, depUse{to: dep.To, change: dep.Change})
		}
	}
	return pkg, uses
}

func parentOf(id string, ids map[string]bool) string {
	rest := id
	for {
		i := strings.LastIndex(rest, "/")
		if i <= 0 {
			return ""
		}
		rest = rest[:i]
		if ids[rest] {
			return rest
		}
	}
}

func packageChange(g *group, diffing bool) string {
	if !diffing {
		return same
	}
	if len(g.left) == 0 {
		return added
	}
	if len(g.right) == 0 {
		return removed
	}
	if len(diff.Files(g.left, g.right)) > 0 {
		return modified
	}
	return same
}

func importCounts(files []lib.ParsedFile) map[string]int {
	out := map[string]int{}
	for _, file := range files {
		seen := map[string]bool{}
		for _, entry := range file.Entities {
			imp, ok := entry.(lib.ImportEntry)
			if !ok || imp.Path == "" || seen[imp.Path] {
				continue
			}
			seen[imp.Path] = true
			out[imp.Path]++
		}
	}
	return out
}

func packageDeps(from string, left, right map[string]int, ids map[string]bool, diffing bool) []Dep {
	keys := map[string]bool{}
	for k := range left {
		keys[k] = true
	}
	for k := range right {
		keys[k] = true
	}
	var deps []Dep
	for to := range keys {
		if to == from {
			continue
		}
		change := same
		files := right[to]
		if diffing {
			switch {
			case left[to] == 0:
				change = added
			case right[to] == 0:
				change = removed
				files = left[to]
			}
		}
		deps = append(deps, Dep{
			To:       to,
			Change:   change,
			External: !ids[to],
			Files:    files,
		})
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].To < deps[j].To })
	return deps
}

func externalPackages(uses []depUse, diffing bool) []Package {
	byID := map[string][]string{}
	for _, use := range uses {
		byID[use.to] = append(byID[use.to], use.change)
	}
	var out []Package
	for id, changes := range byID {
		out = append(out, Package{
			ID:       id,
			Name:     path.Base(id),
			Change:   externalChange(changes, diffing),
			External: true,
		})
	}
	return out
}

func externalChange(changes []string, diffing bool) string {
	if !diffing || len(changes) == 0 {
		return same
	}
	addedN, removedN, sameN := 0, 0, 0
	for _, change := range changes {
		switch change {
		case added:
			addedN++
		case removed:
			removedN++
		default:
			sameN++
		}
	}
	switch {
	case addedN > 0 && removedN == 0 && sameN == 0:
		return added
	case removedN > 0 && addedN == 0 && sameN == 0:
		return removed
	case addedN == 0 && removedN == 0:
		return same
	default:
		return modified
	}
}

type paired struct {
	left, right lib.Entity
	change      string
	file        string
	nth         int
}

func packageEntities(g *group, diffing bool) []Entity {
	leftFiles := map[string]lib.ParsedFile{}
	rightFiles := map[string]lib.ParsedFile{}
	var order []string
	seen := map[string]bool{}
	for _, file := range g.right {
		if !seen[file.Path] {
			order = append(order, file.Path)
			seen[file.Path] = true
		}
		rightFiles[file.Path] = file
	}
	for _, file := range g.left {
		if !seen[file.Path] {
			order = append(order, file.Path)
			seen[file.Path] = true
		}
		leftFiles[file.Path] = file
	}
	var entities []Entity
	for _, filePath := range order {
		_, onLeft := leftFiles[filePath]
		_, onRight := rightFiles[filePath]
		mode := "compare"
		switch {
		case !diffing:
			mode = same
		case onLeft && !onRight:
			mode = removed
		case onRight && !onLeft:
			mode = added
		}
		for _, pair := range pairEntities(leftFiles[filePath].Entities, rightFiles[filePath].Entities, mode) {
			pair.file = filePath
			if entity, ok := sceneEntity(pair); ok {
				entities = append(entities, entity)
			}
		}
	}
	linkMethods(entities)
	return entities
}

type queued struct {
	entry lib.Entity
	nth   int
}

// pairEntities pairs declarations in one file.
// mode is added or removed when the file exists on only one side, same when
// the scene is a snapshot, and modified when the two sides of the file differ
// or not. modified here means "compare", including pairs that are unchanged.
func pairEntities(left, right []lib.Entity, mode string) []paired {
	switch mode {
	case removed:
		return stamp(left, removed)
	case added, same:
		return stamp(right, mode)
	}
	pending := map[string][]queued{}
	var leftOrder []string
	seen := map[string]int{}
	for _, entry := range left {
		key := diff.Identity(entry)
		if _, ok := pending[key]; !ok {
			leftOrder = append(leftOrder, key)
		}
		pending[key] = append(pending[key], queued{entry: entry, nth: seen[key]})
		seen[key]++
	}
	var out []paired
	for _, entry := range right {
		key := diff.Identity(entry)
		queue := pending[key]
		if len(queue) == 0 {
			out = append(out, paired{right: entry, change: added, nth: seen[key]})
			seen[key]++
			continue
		}
		old := queue[0]
		pending[key] = queue[1:]
		change := same
		if !reflect.DeepEqual(old.entry, entry) {
			change = modified
		}
		out = append(out, paired{left: old.entry, right: entry, change: change, nth: old.nth})
	}
	for _, key := range leftOrder {
		for _, old := range pending[key] {
			out = append(out, paired{left: old.entry, change: removed, nth: old.nth})
		}
	}
	return out
}

func stamp(entries []lib.Entity, change string) []paired {
	seen := map[string]int{}
	out := make([]paired, 0, len(entries))
	for _, entry := range entries {
		key := diff.Identity(entry)
		nth := seen[key]
		seen[key]++
		pair := paired{change: change, nth: nth}
		if change == removed {
			pair.left = entry
		} else {
			pair.right = entry
		}
		out = append(out, pair)
	}
	return out
}

func sceneEntity(pair paired) (Entity, bool) {
	entry := pair.right
	if pair.change == removed || entry == nil {
		entry = pair.left
	}
	if _, ok := entry.(lib.ImportEntry); ok || entry == nil {
		return Entity{}, false
	}
	id := entityID(pair.file, diff.Identity(entry), pair.nth)
	out := Entity{
		ID:     id,
		Kind:   entry.Kind().String(),
		Name:   entityName(entry),
		Recv:   entityRecv(entry),
		File:   pair.file,
		Change: pair.change,
		Fields: entityFields(entry),
	}
	if n, ok := bodyBytes(pair.right); ok && pair.change != removed {
		out.BodyBytes = n
	}
	if n, ok := bodyBytes(pair.left); ok && pair.change != added {
		out.BodyBytesBefore = &n
	}
	if out.Kind == "function" || out.Kind == "method" {
		out.Calls = alignCalls(callsOf(pair.left), callsOf(pair.right), pair.change)
	}
	return out, true
}

func entityID(file, ident string, nth int) string {
	id := file + "#" + strings.ReplaceAll(ident, "\x00", "#")
	if nth > 0 {
		id += "#" + strconv.Itoa(nth+1)
	}
	return id
}

func entityName(entry lib.Entity) string {
	switch entry := entry.(type) {
	case lib.VariableEntry:
		return entry.Name
	case lib.TypeEntry:
		return entry.Name
	case lib.FunctionEntry:
		return entry.Name
	case lib.MethodEntry:
		return entry.Name
	default:
		return ""
	}
}

func entityRecv(entry lib.Entity) string {
	method, ok := entry.(lib.MethodEntry)
	if !ok || method.Type == nil {
		return ""
	}
	return normRecv(method.Type.Name)
}

func entityFields(entry lib.Entity) []string {
	typ, ok := entry.(lib.TypeEntry)
	if !ok || len(typ.Fields) == 0 {
		return nil
	}
	names := make([]string, len(typ.Fields))
	for i, field := range typ.Fields {
		names[i] = field.Name
	}
	return names
}

func bodyBytes(entry lib.Entity) (int, bool) {
	switch entry := entry.(type) {
	case lib.FunctionEntry:
		return entry.BodyBytes, true
	case lib.MethodEntry:
		return entry.BodyBytes, true
	default:
		return 0, false
	}
}

func callsOf(entry lib.Entity) []lib.Call {
	switch entry := entry.(type) {
	case lib.FunctionEntry:
		return entry.Calls
	case lib.MethodEntry:
		return entry.Calls
	default:
		return nil
	}
}

// normRecv is the type a method belongs to. A Rust trait impl owner
// <T as Trait> belongs to T.
func normRecv(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "*"))
	if strings.HasPrefix(name, "<") {
		if i := strings.Index(name, " as "); i > 0 {
			name = name[1:i]
		}
	}
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return name
}

func linkMethods(entities []Entity) {
	byFile := map[string]string{}
	byName := map[string]string{}
	for _, entity := range entities {
		if entity.Kind != "type" {
			continue
		}
		if _, ok := byName[entity.Name]; !ok {
			byName[entity.Name] = entity.ID
		}
		byFile[entity.File+"\x00"+entity.Name] = entity.ID
	}
	for i, entity := range entities {
		if entity.Kind != "method" || entity.Recv == "" {
			continue
		}
		if id, ok := byFile[entity.File+"\x00"+entity.Recv]; ok {
			entities[i].Parent = id
			continue
		}
		if id, ok := byName[entity.Recv]; ok {
			entities[i].Parent = id
		}
	}
}
