package tour

import (
	"fmt"
	"sort"
	"strings"

	"citydiff/lib/scene"
)

// Node is one name a tour can point at: a package, an external import
// target, or a declaration. Name is the canonical qualified name.
type Node struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Package string `json:"package"`
	Change  string `json:"change"`
	File    string `json:"file,omitempty"`

	segs  []string
	loose []string
}

// Package, external and the declaration kinds the scene uses.
const (
	KindPackage  = "package"
	KindExternal = "external"
	KindType     = "type"
	KindFunction = "function"
	KindMethod   = "method"
	KindVariable = "variable"
)

// Index holds every node of one scene.
type Index struct {
	Nodes []Node
	byID  map[string]int
}

// NewIndex lists the packages and declarations of sc. A Go declaration is
// named pkg/path.Func or pkg/path.Type.Method. A Rust declaration is named
// crate::path::func or crate::path::<T as Trait>::m.
func NewIndex(sc scene.Scene) *Index {
	ix := &Index{byID: map[string]int{}}
	for _, pkg := range sc.Packages {
		kind := KindPackage
		if pkg.External {
			kind = KindExternal
		}
		ix.add(Node{Name: pkg.ID, Kind: kind, ID: pkg.ID, Package: pkg.ID, Change: pkg.Change})
		for _, e := range pkg.Entities {
			sep := "."
			if strings.HasSuffix(e.File, ".rs") {
				sep = "::"
			}
			name := pkg.ID + sep + e.Name
			if e.Recv != "" {
				name = pkg.ID + sep + e.Recv + sep + e.Name
			}
			n := Node{Name: name, Kind: e.Kind, ID: e.ID, Package: pkg.ID, Change: e.Change, File: e.File}
			if bare := bareRecv(e.Recv); bare != e.Recv {
				n.loose = append(split(pkg.ID), bare, e.Name)
			}
			ix.add(n)
		}
	}
	return ix
}

func (ix *Index) add(n Node) {
	n.segs = split(n.Name)
	ix.byID[n.ID] = len(ix.Nodes)
	ix.Nodes = append(ix.Nodes, n)
}

// Node returns the node with this scene id.
func (ix *Index) Node(id string) (Node, bool) {
	i, ok := ix.byID[id]
	if !ok {
		return Node{}, false
	}
	return ix.Nodes[i], true
}

// bareRecv turns "<T as Trait>" into "T" and "*T" into "T".
func bareRecv(recv string) string {
	r := strings.TrimPrefix(recv, "*")
	if strings.HasPrefix(r, "<") && strings.HasSuffix(r, ">") {
		if i := strings.Index(r, " as "); i > 0 {
			return strings.TrimSpace(r[1:i])
		}
	}
	return r
}

// split cuts a qualified name at "/", "." and "::", but never inside <...>,
// so "<T as fmt::Display>" stays one segment.
func split(name string) []string {
	var out []string
	depth, start := 0, 0
	push := func(end int) {
		if end > start {
			out = append(out, name[start:end])
		}
	}
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '<':
			depth++
		case c == '>':
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case c == '/' || c == '.':
			push(i)
			start = i + 1
		case c == ':' && i+1 < len(name) && name[i+1] == ':':
			push(i)
			start = i + 2
			i++
		}
	}
	push(len(name))
	return out
}

func hasSuffix(segs, tail []string, fold bool) bool {
	if len(tail) == 0 || len(tail) > len(segs) {
		return false
	}
	off := len(segs) - len(tail)
	for i, t := range tail {
		s := segs[off+i]
		if s != t && (!fold || !strings.EqualFold(s, t)) {
			return false
		}
	}
	return true
}

// Error is a name that did not resolve to exactly one node.
type Error struct {
	Name        string
	Reason      string // "unknown", "ambiguous" or "kind"
	Want        []string
	Candidates  []Node
	Total       int
	Suggestions []Node
}

func (e *Error) Error() string {
	var b strings.Builder
	switch e.Reason {
	case "ambiguous":
		fmt.Fprintf(&b, "%q is ambiguous (%d matches); qualify it:", e.Name, e.Total)
		for _, n := range e.Candidates {
			fmt.Fprintf(&b, "\n      %s (%s)", n.Name, n.Kind)
		}
	case "kind":
		fmt.Fprintf(&b, "%q is a %s; this field wants %s", e.Name, e.Candidates[0].Kind, strings.Join(e.Want, " or "))
	default:
		fmt.Fprintf(&b, "%q matches no node", e.Name)
		if len(e.Suggestions) > 0 {
			b.WriteString("; did you mean:")
			for _, n := range e.Suggestions {
				fmt.Fprintf(&b, "\n      %s (%s)", n.Name, n.Kind)
			}
		}
	}
	return b.String()
}

const maxCandidates = 8

// Resolve finds the one node name points at. want limits the kinds; empty
// means any. Matching runs from strict to loose and stops at the first rule
// that matches anything: the scene id or the full name, a tail of whole
// segments (generator.New, Type.Method, <T as Trait>::m), a Rust method by
// its bare type (T::m for <T as Trait>::m), then the same tails ignoring
// case. Several matches at that rule are an ambiguity, never a guess.
func (ix *Index) Resolve(name string, want ...string) (Node, error) {
	name = strings.TrimSpace(name)
	q := split(name)
	rules := []func(n *Node) bool{
		func(n *Node) bool { return n.ID == name || n.Name == name },
		func(n *Node) bool { return hasSuffix(n.segs, q, false) },
		func(n *Node) bool { return n.loose != nil && hasSuffix(n.loose, q, false) },
		func(n *Node) bool {
			return hasSuffix(n.segs, q, true) || (n.loose != nil && hasSuffix(n.loose, q, true))
		},
	}
	var wrongKind []Node
	for _, rule := range rules {
		var hits, other []Node
		for i := range ix.Nodes {
			n := &ix.Nodes[i]
			if !rule(n) {
				continue
			}
			if kindOK(n.Kind, want) {
				hits = append(hits, *n)
			} else {
				other = append(other, *n)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			sortNodes(hits)
			total := len(hits)
			if total > maxCandidates {
				hits = hits[:maxCandidates]
			}
			return Node{}, &Error{Name: name, Reason: "ambiguous", Want: want, Candidates: hits, Total: total}
		}
		if wrongKind == nil && len(other) > 0 {
			wrongKind = other
		}
	}
	if len(wrongKind) == 1 {
		return Node{}, &Error{Name: name, Reason: "kind", Want: want, Candidates: wrongKind}
	}
	return Node{}, &Error{Name: name, Reason: "unknown", Want: want, Suggestions: ix.suggest(q, want)}
}

func kindOK(kind string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if w == kind {
			return true
		}
	}
	return false
}

func sortNodes(nodes []Node) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
}

// suggest ranks nodes whose last segment is close to the last segment asked for.
func (ix *Index) suggest(q []string, want []string) []Node {
	if len(q) == 0 {
		return nil
	}
	last := strings.ToLower(q[len(q)-1])
	type scored struct {
		n     Node
		score int
	}
	var all []scored
	for _, n := range ix.Nodes {
		if !kindOK(n.Kind, want) || len(n.segs) == 0 {
			continue
		}
		seg := strings.ToLower(n.segs[len(n.segs)-1])
		score := -1
		switch d := distance(seg, last); {
		case d <= 2 && d < len(last):
			score = d
		case len(last) >= 3 && strings.Contains(seg, last):
			score = 3
		}
		if score >= 0 {
			all = append(all, scored{n, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score < all[j].score
		}
		return all[i].n.Name < all[j].n.Name
	})
	var out []Node
	for i := 0; i < len(all) && i < 5; i++ {
		out = append(out, all[i].n)
	}
	return out
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
