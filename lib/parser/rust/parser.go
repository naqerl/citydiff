// Package rust parses Rust source into language-agnostic entities.
package rust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

	"citydiff/lib"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// Parser parses Rust source.
type Parser struct{}

// New returns a Rust parser.
func New() Parser { return Parser{} }

// Parse implements lib.Parser.
// Every file is collected before calls are resolved, so a declaration in the
// last file fills a call in the first file. Calls that do not resolve are kept.
//
// A Cargo.toml names the crate. ImportPath is the crate name followed by the
// module path, slash-separated, so src/lib.rs is "acme" and src/net/mod.rs or
// src/net.rs is "acme/net". Package is the last module segment.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_rust.Language())); err != nil {
		return nil, err
	}

	var crates []crateRoot
	var drafts []*draft
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		file.Path = path.Clean(filepath.ToSlash(file.Path))
		base := path.Base(file.Path)
		if base == "Cargo.toml" {
			if name := crateName(file.Src); name != "" {
				crates = append(crates, crateRoot{dir: path.Dir(file.Path), name: name})
			}
			continue
		}
		if !strings.HasSuffix(base, ".rs") {
			return nil, fmt.Errorf("%s is not a Rust file", file.Path)
		}
		tree := parser.Parse(file.Src, nil)
		if tree == nil {
			return nil, fmt.Errorf("parse %s returned no tree", file.Path)
		}
		d := &draft{path: file.Path}
		w := walker{src: file.Src, d: d}
		w.items(tree.RootNode(), nil, "")
		tree.Close()
		assignMethodHashes(d.entities)
		drafts = append(drafts, d)
	}
	assignModules(drafts, crates)
	resolve(drafts)

	out := make([]lib.ParsedFile, len(drafts))
	for i, d := range drafts {
		out[i] = lib.ParsedFile{
			Path:       d.path,
			Package:    d.pkg,
			ImportPath: d.importPath,
			Module:     d.crate,
			Entities:   d.entities,
		}
	}
	return out, nil
}

var _ lib.Parser = Parser{}

type walker struct {
	src []byte
	d   *draft
}

func (w *walker) text(n *tree_sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Utf8Text(w.src)
}

// items reads the items of a file, an inline module, a trait or an impl.
// mod is the inline module path below the file module. recv is the impl or
// trait type that functions in this list belong to.
func (w *walker) items(list *tree_sitter.Node, mod []string, recv string) {
	cursor := list.Walk()
	defer cursor.Close()
	for _, n := range list.NamedChildren(cursor) {
		switch n.Kind() {
		case "use_declaration":
			for _, use := range useTree(w.src, n.ChildByFieldName("argument"), nil) {
				use.mod = mod
				w.d.uses = append(w.d.uses, use)
				w.add(lib.ImportEntry{Path: strings.Join(use.path, "::")}, nil, mod, "")
			}
		case "extern_crate_declaration":
			name := w.text(n.ChildByFieldName("name"))
			w.add(lib.ImportEntry{Path: name}, nil, mod, "")
		case "mod_item":
			if body := n.ChildByFieldName("body"); body != nil {
				w.items(body, append(append([]string{}, mod...), w.text(n.ChildByFieldName("name"))), "")
			}
		case "struct_item", "union_item":
			w.add(lib.TypeEntry{Name: w.text(n.ChildByFieldName("name")), Fields: w.fields(n.ChildByFieldName("body"))}, nil, mod, "")
		case "enum_item":
			w.add(lib.TypeEntry{Name: w.text(n.ChildByFieldName("name")), Fields: w.variants(n.ChildByFieldName("body"))}, nil, mod, "")
		case "type_item":
			w.add(lib.TypeEntry{Name: w.text(n.ChildByFieldName("name"))}, nil, mod, "")
		case "trait_item":
			name := w.text(n.ChildByFieldName("name"))
			w.add(lib.TypeEntry{Name: name}, nil, mod, "")
			if body := n.ChildByFieldName("body"); body != nil {
				w.items(body, mod, name)
			}
		case "impl_item":
			if body := n.ChildByFieldName("body"); body != nil {
				w.items(body, mod, w.text(n.ChildByFieldName("type")))
			}
		case "const_item", "static_item":
			w.add(lib.VariableEntry{Name: w.text(n.ChildByFieldName("name"))}, nil, mod, "")
		case "function_item", "function_signature_item":
			fn, body := w.function(&n, mod, recv)
			if recv == "" {
				w.add(fn, body, mod, "")
				continue
			}
			w.add(lib.MethodEntry{FunctionEntry: fn, Type: &lib.TypeEntry{Name: recv}}, body, mod, normType(recv))
		}
	}
}

func (w *walker) add(entry lib.Entity, calls []pending, mod []string, recv string) {
	if len(calls) == 0 {
		calls = nil
	}
	w.d.entities = append(w.d.entities, entry)
	w.d.calls = append(w.d.calls, calls)
	w.d.mods = append(w.d.mods, mod)
	w.d.recvs = append(w.d.recvs, recv)
}

func (w *walker) fields(body *tree_sitter.Node) []lib.Field {
	if body == nil || body.Kind() != "field_declaration_list" {
		return nil
	}
	var out []lib.Field
	cursor := body.Walk()
	defer cursor.Close()
	for _, f := range body.NamedChildren(cursor) {
		if f.Kind() == "field_declaration" {
			out = append(out, lib.Field{Name: w.text(f.ChildByFieldName("name"))})
		}
	}
	return out
}

func (w *walker) variants(body *tree_sitter.Node) []lib.Field {
	if body == nil {
		return nil
	}
	var out []lib.Field
	cursor := body.Walk()
	defer cursor.Close()
	for _, v := range body.NamedChildren(cursor) {
		if v.Kind() == "enum_variant" {
			out = append(out, lib.Field{Name: w.text(v.ChildByFieldName("name"))})
		}
	}
	return out
}

func (w *walker) function(n *tree_sitter.Node, mod []string, recv string) (lib.FunctionEntry, []pending) {
	params := w.parameters(n.ChildByFieldName("parameters"))
	var returns []lib.Parameter
	if ret := n.ChildByFieldName("return_type"); ret != nil {
		returns = []lib.Parameter{{Type: w.text(ret)}}
	}
	bodyHash, bodyBytes := hashBody(w.src, n)
	fn := lib.FunctionEntry{
		Name:       w.text(n.ChildByFieldName("name")),
		Parameters: params,
		ReturnArgs: returns,
		BodyHash:   bodyHash,
		BodyBytes:  bodyBytes,
	}
	scope := map[string]binding{}
	for _, p := range params {
		bindPattern(scope, p.Name, typeBinding(p.Type))
	}
	if recv != "" {
		scope["self"] = binding{recv: normType(recv)}
	}
	return fn, collectCalls(w.src, n.ChildByFieldName("body"), scope, normType(recv))
}

// parameters skips self, which belongs to the impl type the way a Go
// receiver does.
func (w *walker) parameters(list *tree_sitter.Node) []lib.Parameter {
	if list == nil {
		return nil
	}
	var out []lib.Parameter
	cursor := list.Walk()
	defer cursor.Close()
	for _, p := range list.NamedChildren(cursor) {
		switch p.Kind() {
		case "parameter":
			out = append(out, lib.Parameter{Name: w.text(p.ChildByFieldName("pattern")), Type: w.text(p.ChildByFieldName("type"))})
		case "variadic_parameter":
			out = append(out, lib.Parameter{Type: "..."})
		}
	}
	return out
}

// hashBody is the hex SHA-256 of the function body source, and the length of
// those bytes. A declaration with no body has an empty hash and length 0.
func hashBody(src []byte, decl *tree_sitter.Node) (string, int) {
	body := decl.ChildByFieldName("body")
	if body == nil {
		return "", 0
	}
	start, end := body.StartByte(), body.EndByte()
	if end < start || int(end) > len(src) {
		return "", 0
	}
	sum := sha256.Sum256(src[start:end])
	return hex.EncodeToString(sum[:]), int(end - start)
}

// assignMethodHashes sets each type's MethodsHash from its method body hashes
// across every impl block in the file, in source order.
func assignMethodHashes(entries []lib.Entity) {
	byType := map[string][]string{}
	for _, entry := range entries {
		method, ok := entry.(lib.MethodEntry)
		if !ok || method.Type == nil || method.BodyHash == "" {
			continue
		}
		name := normType(method.Type.Name)
		byType[name] = append(byType[name], method.BodyHash)
	}
	for i, entry := range entries {
		typ, ok := entry.(lib.TypeEntry)
		if !ok {
			continue
		}
		parts := byType[typ.Name]
		if len(parts) == 0 {
			continue
		}
		typ.MethodsHash = cumulativeHash(parts)
		entries[i] = typ
	}
}

func cumulativeHash(hexHashes []string) string {
	h := sha256.New()
	for _, hexHash := range hexHashes {
		raw, err := hex.DecodeString(hexHash)
		if err != nil {
			continue
		}
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// normType is the bare type name: no references, generics, dyn or path.
func normType(t string) string {
	t = strings.TrimSpace(t)
	for {
		switch {
		case strings.HasPrefix(t, "&"):
			t = strings.TrimSpace(t[1:])
		case strings.HasPrefix(t, "mut "):
			t = strings.TrimSpace(t[4:])
		case strings.HasPrefix(t, "dyn "):
			t = strings.TrimSpace(t[4:])
		case strings.HasPrefix(t, "'"):
			i := strings.IndexAny(t, " \t")
			if i < 0 {
				return ""
			}
			t = strings.TrimSpace(t[i:])
		default:
			if i := strings.IndexByte(t, '<'); i >= 0 {
				t = t[:i]
			}
			if i := strings.LastIndex(t, "::"); i >= 0 {
				t = t[i+2:]
			}
			return strings.TrimSpace(t)
		}
	}
}
