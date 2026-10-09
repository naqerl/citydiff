package golang

import (
	"bytes"
	"sort"
	"strings"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// importUse is one import spec. The alias is kept for resolution and is not
// stored on the import entity.
type importUse struct {
	path  string
	alias string
	dot   bool
	blank bool
}

// binding is a name in scope at a call. A blocked name is a value, so the
// call is left unresolved. Otherwise the name is a receiver whose type is
// recv, in the same package or in the imported package pkgQual.
type binding struct {
	block   bool
	recv    string
	pkgQual string
}

// pending is one call before refs are filled.
type pending struct {
	expr    string
	name    string
	qual    string
	recv    string
	method  bool
	resolve bool
	start   uint
	end     uint
}

type draft struct {
	path       string
	dir        string
	pkg        string
	module     string
	importPath string
	imports    []importUse
	entities   []lib.Entity
	calls      [][]pending
}

type pkgID struct {
	importPath string
	dir        string
	name       string
}

func (p pkgID) key() string {
	if p.importPath != "" {
		return "i\x00" + p.importPath + "\x00" + p.name
	}
	return "d\x00" + p.dir + "\x00" + p.name
}

func (d *draft) id() pkgID {
	return pkgID{importPath: d.importPath, dir: d.dir, name: d.pkg}
}

type moduleRoot struct {
	dir  string
	path string
}

type decl struct {
	path string
	name string
	recv string
}

type index struct {
	funcs   map[string]map[string][]decl
	methods map[string]map[string]map[string][]decl
}

func modulePath(src []byte) string {
	for _, line := range bytes.Split(src, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if i := strings.Index(text, "//"); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if !strings.HasPrefix(text, "module ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(text, "module "))
		rest = strings.Trim(rest, `"`)
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			rest = rest[:i]
		}
		return rest
	}
	return ""
}

func assignImportPaths(drafts []*draft, mods []moduleRoot) {
	for _, d := range drafts {
		mod, ok := bestModule(mods, d.dir)
		if !ok {
			continue
		}
		d.module = mod.path
		rel, ok := relDir(mod.dir, d.dir)
		if !ok {
			continue
		}
		if rel == "." {
			d.importPath = mod.path
			continue
		}
		d.importPath = mod.path + "/" + rel
	}
}

func relDir(root, dir string) (string, bool) {
	if root == "." {
		return dir, true
	}
	if dir == root {
		return ".", true
	}
	prefix := root + "/"
	if strings.HasPrefix(dir, prefix) {
		return strings.TrimPrefix(dir, prefix), true
	}
	return "", false
}

func bestModule(mods []moduleRoot, dir string) (moduleRoot, bool) {
	var best moduleRoot
	found := false
	bestRank := -1
	for _, mod := range mods {
		if !dirUnder(dir, mod.dir) {
			continue
		}
		rank := 0
		if mod.dir != "." {
			rank = strings.Count(mod.dir, "/") + 1
		}
		if !found || rank > bestRank {
			best = mod
			bestRank = rank
			found = true
		}
	}
	return best, found
}

func dirUnder(dir, root string) bool {
	if root == "." {
		return true
	}
	return dir == root || strings.HasPrefix(dir, root+"/")
}

func resolve(drafts []*draft) {
	byImport := importIndex(drafts)
	idx := buildIndex(drafts)
	for _, d := range drafts {
		locals, dots := d.localImports(byImport)
		for i, pendingCalls := range d.calls {
			if len(pendingCalls) == 0 {
				continue
			}
			calls := make([]lib.Call, len(pendingCalls))
			for j, call := range pendingCalls {
				calls[j] = lib.Call{Expr: call.expr}
				ref, ok := idx.lookup(d.id(), call, locals, dots)
				if ok {
					calls[j].Ref = ref
				}
			}
			setCalls(d, i, calls)
		}
	}
}

func importIndex(drafts []*draft) map[string]pkgID {
	seen := map[string]pkgID{}
	conflict := map[string]bool{}
	for _, d := range drafts {
		if d.importPath == "" || d.pkg == "" || strings.HasSuffix(d.pkg, "_test") {
			continue
		}
		id := d.id()
		if prev, ok := seen[d.importPath]; ok && prev != id {
			conflict[d.importPath] = true
			continue
		}
		seen[d.importPath] = id
	}
	for importPath := range conflict {
		delete(seen, importPath)
	}
	return seen
}

func (d *draft) localImports(byImport map[string]pkgID) (map[string]pkgID, []pkgID) {
	locals := map[string]pkgID{}
	var dots []pkgID
	for _, spec := range d.imports {
		if spec.blank {
			continue
		}
		id, ok := byImport[spec.path]
		if !ok {
			continue
		}
		if spec.dot {
			dots = append(dots, id)
			continue
		}
		name := spec.alias
		if name == "" {
			name = id.name
		}
		if _, exists := locals[name]; exists {
			continue
		}
		locals[name] = id
	}
	return locals, dots
}

func buildIndex(drafts []*draft) *index {
	idx := &index{
		funcs:   map[string]map[string][]decl{},
		methods: map[string]map[string]map[string][]decl{},
	}
	for _, d := range drafts {
		key := d.id().key()
		for _, entity := range d.entities {
			switch entity := entity.(type) {
			case lib.FunctionEntry:
				addFunc(idx.funcs, key, decl{path: d.path, name: entity.Name})
			case lib.MethodEntry:
				if entity.Type == nil {
					continue
				}
				recv := normRecv(entity.Type.Name)
				addMethod(idx.methods, key, recv, decl{path: d.path, name: entity.Name, recv: recv})
			}
		}
	}
	return idx
}

func addFunc(funcs map[string]map[string][]decl, key string, d decl) {
	if funcs[key] == nil {
		funcs[key] = map[string][]decl{}
	}
	funcs[key][d.name] = mergeDecl(funcs[key][d.name], d)
}

func addMethod(methods map[string]map[string]map[string][]decl, key, recv string, d decl) {
	if methods[key] == nil {
		methods[key] = map[string]map[string][]decl{}
	}
	if methods[key][recv] == nil {
		methods[key][recv] = map[string][]decl{}
	}
	methods[key][recv][d.name] = mergeDecl(methods[key][recv][d.name], d)
}

// mergeDecl keeps the first declaration when paths are the same stem with
// different GOOS/GOARCH suffixes. Those files are alternatives, not two
// declarations in one build. An unsuffixed file is kept as a second entry
// so lookup stays unresolved: it is built together with the suffixed file.
func mergeDecl(cur []decl, d decl) []decl {
	if len(cur) == 0 {
		return []decl{d}
	}
	stem, ok := osArchStem(d.path)
	if !ok {
		return append(cur, d)
	}
	for _, prev := range cur {
		prevStem, prevOK := osArchStem(prev.path)
		if !prevOK || prevStem != stem {
			return append(cur, d)
		}
	}
	return cur
}

func osArchStem(p string) (string, bool) {
	slash := strings.LastIndex(p, "/")
	dir, base := "", p
	if slash >= 0 {
		dir, base = p[:slash+1], p[slash+1:]
	}
	if !strings.HasSuffix(base, ".go") {
		return "", false
	}
	stem, ok := trimOSArch(strings.TrimSuffix(base, ".go"))
	if !ok || stem == "" {
		return "", false
	}
	return dir + stem, true
}

func trimOSArch(name string) (string, bool) {
	i := strings.LastIndex(name, "_")
	if i <= 0 {
		return "", false
	}
	last := name[i+1:]
	rest := name[:i]
	if j := strings.LastIndex(rest, "_"); j > 0 && goos[rest[j+1:]] && goarch[last] {
		return rest[:j], true
	}
	if goos[last] || goarch[last] {
		return rest, true
	}
	return "", false
}

// Filename suffixes from Go's known GOOS and GOARCH lists. This is not build-tag evaluation.
var goos = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "js": true, "linux": true,
	"nacl": true, "netbsd": true, "openbsd": true, "plan9": true, "solaris": true,
	"wasip1": true, "windows": true, "zos": true,
}

var goarch = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true,
	"arm64": true, "arm64be": true, "loong64": true, "mips": true, "mipsle": true,
	"mips64": true, "mips64le": true, "mips64p32": true, "mips64p32le": true,
	"ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true,
	"s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

func (idx *index) lookup(file pkgID, call pending, locals map[string]pkgID, dots []pkgID) (*lib.CallRef, bool) {
	if !call.resolve || call.name == "" {
		return nil, false
	}
	if call.method {
		if call.qual != "" {
			id, ok := locals[call.qual]
			if !ok {
				return nil, false
			}
			found, ok := one(idx.methods[id.key()][call.recv][call.name])
			if !ok {
				return nil, false
			}
			return found.ref(), true
		}
		found := idx.methods[file.key()][call.recv][call.name]
		if len(found) == 1 {
			return found[0].ref(), true
		}
		if len(found) > 1 {
			return nil, false
		}
		// Dot-imported packages share the unqualified name. Use the method
		// only when exactly one of them declares that receiver.
		var hits []decl
		for _, id := range dots {
			if d, ok := one(idx.methods[id.key()][call.recv][call.name]); ok {
				hits = append(hits, d)
			}
		}
		if d, ok := one(hits); ok {
			return d.ref(), true
		}
		return nil, false
	}
	if call.qual != "" {
		id, ok := locals[call.qual]
		if !ok {
			return nil, false
		}
		found, ok := one(idx.funcs[id.key()][call.name])
		if !ok {
			return nil, false
		}
		return found.ref(), true
	}
	if found, ok := one(idx.funcs[file.key()][call.name]); ok {
		return found.ref(), true
	}
	var found []decl
	for _, id := range dots {
		if d, ok := one(idx.funcs[id.key()][call.name]); ok {
			found = append(found, d)
		}
	}
	if d, ok := one(found); ok {
		return d.ref(), true
	}
	return nil, false
}

func one(decls []decl) (decl, bool) {
	if len(decls) != 1 {
		return decl{}, false
	}
	return decls[0], true
}

func (d decl) ref() *lib.CallRef {
	return &lib.CallRef{Path: d.path, Name: d.name, Recv: d.recv}
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

func normRecv(raw string) string {
	_, name, ok := namedType(raw)
	if !ok {
		return strings.TrimSpace(raw)
	}
	return name
}

// namedType reports the package qualifier and type name of a single named type.
// Pointers, variadic markers, and type parameters are stripped.
// Composite types are not named types.
func namedType(raw string) (qual, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "...")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "*")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(raw, "[]"), strings.HasPrefix(raw, "map["),
		strings.HasPrefix(raw, "chan "), strings.HasPrefix(raw, "<-chan "),
		strings.HasPrefix(raw, "func("), strings.HasPrefix(raw, "struct{"),
		strings.HasPrefix(raw, "interface{"):
		return "", "", false
	}
	if i := strings.IndexByte(raw, '['); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" || strings.ContainsAny(raw, " \t{}") {
		return "", "", false
	}
	if i := strings.LastIndex(raw, "."); i >= 0 {
		return raw[:i], raw[i+1:], raw[i+1:] != ""
	}
	return "", raw, true
}

func funcScope(recvIdent, recvType string, params []lib.Parameter) map[string]binding {
	scope := map[string]binding{}
	if recvIdent != "" {
		qual, name, ok := namedType(recvType)
		if !ok {
			scope[recvIdent] = binding{block: true}
		} else {
			scope[recvIdent] = binding{recv: name, pkgQual: qual}
		}
	}
	for _, param := range params {
		if param.Name == "" {
			continue
		}
		qual, name, ok := namedType(param.Type)
		if !ok {
			scope[param.Name] = binding{block: true}
			continue
		}
		scope[param.Name] = binding{recv: name, pkgQual: qual}
	}
	return scope
}

func receiverInfo(src []byte, method *tree_sitter.Node) (ident, typeText string) {
	recv := method.ChildByFieldName("receiver")
	if recv == nil {
		return "", ""
	}
	decl := recv.NamedChild(0)
	if decl == nil {
		return "", ""
	}
	names := fieldTexts(src, decl, "name")
	if len(names) > 0 {
		ident = names[0]
	}
	if typ := decl.ChildByFieldName("type"); typ != nil {
		typeText = typ.Utf8Text(src)
	}
	return ident, typeText
}

func collectCalls(src []byte, body *tree_sitter.Node, outer map[string]binding) []pending {
	if body == nil {
		return nil
	}
	if outer == nil {
		outer = map[string]binding{}
	}
	var out []pending
	scopes := []map[string]binding{outer}
	var walk func(*tree_sitter.Node)
	walk = func(n *tree_sitter.Node) {
		if n == nil {
			return
		}
		if scope, ok := pushedScope(src, n); ok {
			scopes = append(scopes, scope)
			defer func() { scopes = scopes[:len(scopes)-1] }()
		}
		// ID[T](x) is a type conversion; ID[T]() is a call through an index.
		// Unwrap the type arguments and resolve the name under them.
		// A conversion such as string(x) or T(x), where T is only a type,
		// is an ordinary call and stays unresolved.
		if n.Kind() == "call_expression" || genericConversion(n) {
			if call, ok := pendingFrom(src, n, scopes); ok {
				call.start = n.StartByte()
				call.end = n.EndByte()
				out = append(out, call)
			}
		}
		if n.Kind() == "type_switch_statement" {
			walkTypeSwitch(src, n, walk, scopes)
			return
		}
		cursor := n.Walk()
		defer cursor.Close()
		for _, child := range n.NamedChildren(cursor) {
			walk(&child)
		}
		// Declarations bind after their initializer, so a call in the
		// initializer and any call earlier in the block still see the outer name.
		switch n.Kind() {
		case "short_var_declaration":
			bindShort(src, n, scopes)
		case "var_spec":
			bindVar(src, n, scopes)
		case "range_clause":
			bindRange(src, n, scopes)
		}
	}
	walk(body)
	// Start byte is source order. Calls that start at the same byte are a
	// chain (s.queries(ctx).Save): the inner call ends first.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].start != out[j].start {
			return out[i].start < out[j].start
		}
		return out[i].end < out[j].end
	})
	return out
}

func genericConversion(n *tree_sitter.Node) bool {
	if n.Kind() != "type_conversion_expression" {
		return false
	}
	typ := n.ChildByFieldName("type")
	return typ != nil && typ.Kind() == "generic_type"
}

func pushedScope(src []byte, n *tree_sitter.Node) (map[string]binding, bool) {
	switch n.Kind() {
	case "func_literal":
		params := parseParameters(src, n.ChildByFieldName("parameters"))
		params = append(append([]lib.Parameter{}, params...), namedResults(src, n.ChildByFieldName("result"))...)
		return funcScope("", "", params), true
	case "block", "if_statement", "for_statement", "expression_switch_statement",
		"type_switch_statement", "expression_case", "default_case", "type_case",
		"communication_case":
		return map[string]binding{}, true
	default:
		return nil, false
	}
}

func walkTypeSwitch(src []byte, n *tree_sitter.Node, walk func(*tree_sitter.Node), scopes []map[string]binding) {
	cursor := n.Walk()
	defer cursor.Close()
	var cases []tree_sitter.Node
	for _, child := range n.NamedChildren(cursor) {
		switch child.Kind() {
		case "type_case", "default_case":
			cases = append(cases, child)
		default:
			walk(&child)
		}
	}
	// The alias comes into scope at the end of the guard, before the cases.
	bindNames(scopes, identNamesFrom(src, n.ChildByFieldName("alias")), binding{block: true})
	for i := range cases {
		walk(&cases[i])
	}
}

func pendingFrom(src []byte, call *tree_sitter.Node, scopes []map[string]binding) (pending, bool) {
	var fn *tree_sitter.Node
	switch call.Kind() {
	case "call_expression":
		fn = call.ChildByFieldName("function")
	case "type_conversion_expression":
		fn = call.ChildByFieldName("type")
	}
	if fn == nil {
		return pending{}, false
	}
	expr := fn.Utf8Text(src)
	base := unwrapCallee(fn)
	if base == nil {
		return pending{expr: expr}, true
	}
	switch base.Kind() {
	case "identifier", "type_identifier":
		name := base.Utf8Text(src)
		if _, bound := findBinding(scopes, name); bound {
			return pending{expr: expr}, true
		}
		return pending{expr: expr, name: name, resolve: true}, true
	case "selector_expression", "qualified_type":
		field, operand := selectorParts(base)
		if field == nil || operand == nil || !isName(field.Kind()) {
			return pending{expr: expr}, true
		}
		name := field.Utf8Text(src)
		if !isName(operand.Kind()) {
			return pending{expr: expr}, true
		}
		qual := operand.Utf8Text(src)
		bound, ok := findBinding(scopes, qual)
		if !ok {
			return pending{expr: expr, name: name, qual: qual, resolve: true}, true
		}
		if bound.block {
			return pending{expr: expr}, true
		}
		return pending{
			expr:    expr,
			name:    name,
			qual:    bound.pkgQual,
			recv:    bound.recv,
			method:  true,
			resolve: true,
		}, true
	case "func_literal":
		return pending{expr: "func"}, true
	default:
		return pending{expr: expr}, true
	}
}

func selectorParts(n *tree_sitter.Node) (field, operand *tree_sitter.Node) {
	if n.Kind() == "qualified_type" {
		return n.ChildByFieldName("name"), n.ChildByFieldName("package")
	}
	return n.ChildByFieldName("field"), n.ChildByFieldName("operand")
}

func unwrapCallee(fn *tree_sitter.Node) *tree_sitter.Node {
	for fn != nil {
		switch fn.Kind() {
		case "parenthesized_expression":
			next := fn.NamedChild(0)
			if next == nil {
				return fn
			}
			fn = next
		case "index_expression":
			next := fn.ChildByFieldName("operand")
			if next == nil {
				return fn
			}
			fn = next
		case "generic_type":
			next := fn.ChildByFieldName("type")
			if next == nil {
				return fn
			}
			fn = next
		default:
			return fn
		}
	}
	return fn
}

func isName(kind string) bool {
	switch kind {
	case "identifier", "type_identifier", "field_identifier", "package_identifier":
		return true
	default:
		return false
	}
}

func findBinding(scopes []map[string]binding, name string) (binding, bool) {
	for i := len(scopes) - 1; i >= 0; i-- {
		if bound, ok := scopes[i][name]; ok {
			return bound, true
		}
	}
	return binding{}, false
}

func bindShort(src []byte, n *tree_sitter.Node, scopes []map[string]binding) {
	names := identNamesFrom(src, n.ChildByFieldName("left"))
	vals := namedNodes(n.ChildByFieldName("right"))
	if len(names) > 1 && len(vals) == 1 {
		bindNames(scopes, names, binding{block: true})
		return
	}
	for i, name := range names {
		b := binding{block: true}
		if i < len(vals) {
			if bound, ok := compositeBinding(src, &vals[i]); ok {
				b = bound
			}
		}
		bindNames(scopes, []string{name}, b)
	}
}

func bindVar(src []byte, n *tree_sitter.Node, scopes []map[string]binding) {
	names := fieldTexts(src, n, "name")
	if typ := n.ChildByFieldName("type"); typ != nil {
		bindNames(scopes, names, bindingFromType(src, typ))
		return
	}
	vals := namedNodes(n.ChildByFieldName("value"))
	if len(names) > 1 && len(vals) == 1 {
		bindNames(scopes, names, binding{block: true})
		return
	}
	for i, name := range names {
		b := binding{block: true}
		if i < len(vals) {
			if bound, ok := compositeBinding(src, &vals[i]); ok {
				b = bound
			}
		}
		bindNames(scopes, []string{name}, b)
	}
}

func bindRange(src []byte, n *tree_sitter.Node, scopes []map[string]binding) {
	if !rangeDeclares(src, n) {
		return
	}
	bindNames(scopes, identNamesFrom(src, n.ChildByFieldName("left")), binding{block: true})
}

func rangeDeclares(src []byte, n *tree_sitter.Node) bool {
	left := n.ChildByFieldName("left")
	if left == nil {
		return false
	}
	start, end := int(left.EndByte()), int(n.EndByte())
	if start < 0 || end > len(src) || start > end {
		return false
	}
	chunk := src[start:end]
	if rel := bytes.Index(chunk, []byte("range")); rel >= 0 {
		chunk = chunk[:rel]
	}
	return bytes.Contains(chunk, []byte(":="))
}

func bindNames(scopes []map[string]binding, names []string, b binding) {
	if len(scopes) == 0 {
		return
	}
	scope := scopes[len(scopes)-1]
	for _, name := range names {
		if name == "" || name == "_" {
			continue
		}
		scope[name] = b
	}
}

func identNamesFrom(src []byte, list *tree_sitter.Node) []string {
	if list == nil {
		return nil
	}
	var names []string
	cursor := list.Walk()
	defer cursor.Close()
	for _, child := range list.NamedChildren(cursor) {
		if child.Kind() == "identifier" {
			names = append(names, child.Utf8Text(src))
		}
	}
	return names
}

func namedNodes(list *tree_sitter.Node) []tree_sitter.Node {
	if list == nil {
		return nil
	}
	cursor := list.Walk()
	defer cursor.Close()
	return list.NamedChildren(cursor)
}

func compositeBinding(src []byte, n *tree_sitter.Node) (binding, bool) {
	n = skipParen(n)
	if n == nil {
		return binding{}, false
	}
	if n.Kind() == "unary_expression" {
		if !isAddress(src, n) {
			return binding{}, false
		}
		n = skipParen(n.ChildByFieldName("operand"))
	}
	if n == nil || n.Kind() != "composite_literal" {
		return binding{}, false
	}
	typ := n.ChildByFieldName("type")
	if typ == nil {
		return binding{}, false
	}
	b := bindingFromType(src, typ)
	if b.block {
		return binding{}, false
	}
	return b, true
}

func isAddress(src []byte, n *tree_sitter.Node) bool {
	if op := n.ChildByFieldName("operator"); op != nil && op.Utf8Text(src) == "&" {
		return true
	}
	text := strings.TrimSpace(n.Utf8Text(src))
	return strings.HasPrefix(text, "&")
}

func bindingFromType(src []byte, typ *tree_sitter.Node) binding {
	qual, name, ok := namedType(typ.Utf8Text(src))
	if !ok {
		return binding{block: true}
	}
	return binding{recv: name, pkgQual: qual}
}

func skipParen(n *tree_sitter.Node) *tree_sitter.Node {
	for n != nil && n.Kind() == "parenthesized_expression" {
		next := n.NamedChild(0)
		if next == nil {
			return n
		}
		n = next
	}
	return n
}
