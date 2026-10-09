package tour

import "citydiff/lib/scene"

// callGraph is every resolved call between declarations of one scene.
type callGraph map[string][]string

func newCallGraph(sc scene.Scene, ix *Index) callGraph {
	type key struct{ file, recv, name string }
	decls := map[key]string{}
	for _, pkg := range sc.Packages {
		for _, e := range pkg.Entities {
			k := key{e.File, e.Recv, e.Name}
			if _, ok := decls[k]; !ok {
				decls[k] = e.ID
			}
		}
	}
	g := callGraph{}
	for _, pkg := range sc.Packages {
		for _, e := range pkg.Entities {
			seen := map[string]bool{}
			for _, step := range e.Calls {
				if !step.Resolved {
					continue
				}
				to, ok := decls[key{step.Path, step.Recv, step.Name}]
				if !ok || to == e.ID || seen[to] {
					continue
				}
				seen[to] = true
				g[e.ID] = append(g[e.ID], to)
			}
		}
	}
	return g
}

// path is the shortest chain of calls from one declaration to another,
// both ends included, or nil when there is none.
func (g callGraph) path(from, to string) []string {
	if from == to {
		return []string{from}
	}
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range g[cur] {
			if _, ok := prev[next]; ok {
				continue
			}
			prev[next] = cur
			if next == to {
				var out []string
				for at := to; at != ""; at = prev[at] {
					out = append([]string{at}, out...)
				}
				return out
			}
			queue = append(queue, next)
		}
	}
	return nil
}
