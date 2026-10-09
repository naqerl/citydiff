package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"citydiff/lib"
	"citydiff/lib/files"
	"citydiff/lib/git"
	"citydiff/lib/parser"
	"citydiff/lib/scene"
	"citydiff/lib/tour"
)

// snapshot is one built scene with the sources it came from, so the viewer
// can show a declaration's code on either side.
type snapshot struct {
	path, commitRange string
	scene             scene.Scene
	before, after     map[string][]byte
}

func buildSnapshot(path, commitRange string) (*snapshot, error) {
	snap := &snapshot{path: path, commitRange: commitRange, after: map[string][]byte{}}
	var leftFiles, rightFiles []lib.File
	if commitRange != "" {
		sides, err := git.Sources(path, commitRange)
		if err != nil {
			return nil, err
		}
		leftFiles, rightFiles = sides.Left, sides.Right
		snap.before = map[string][]byte{}
		for _, f := range leftFiles {
			snap.before[f.Path] = f.Src
		}
	} else {
		src, err := files.Tree(path)
		if err != nil {
			return nil, err
		}
		for {
			f, err := src.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			rightFiles = append(rightFiles, f)
		}
	}
	for _, f := range rightFiles {
		snap.after[f.Path] = f.Src
	}
	right, err := parser.New().Parse(lib.Mem(rightFiles))
	if err != nil {
		return nil, err
	}
	var left []lib.ParsedFile
	if commitRange != "" {
		if left, err = parser.New().Parse(lib.Mem(leftFiles)); err != nil {
			return nil, err
		}
		if left == nil {
			left = []lib.ParsedFile{}
		}
	}
	snap.scene = scene.Build(left, right)
	return snap, nil
}

// sameRange compares a script's range with the snapshot's by commit hash,
// so an abbreviated or symbolic ref still matches.
func (s *snapshot) sameRange(script string) error {
	if s.commitRange == "" {
		return fmt.Errorf("script targets %q but the scene is the plain tree; pass -range", script)
	}
	wl, wr, err := git.Revs(s.path, s.commitRange)
	if err != nil {
		return err
	}
	gl, gr, err := git.Revs(s.path, script)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", script, err)
	}
	if wl != gl || wr != gr {
		return fmt.Errorf("script targets %s..%s but the scene is %s..%s", short(gl), short(gr), short(wl), short(wr))
	}
	return nil
}

func short(hash string) string {
	if len(hash) > 10 {
		return hash[:10]
	}
	return hash
}

func (s *snapshot) check(t tour.Tour) (tour.Resolved, []tour.Problem) {
	return tour.Check(t, s.scene, s.sameRange)
}

// codeView is one declaration's source on both sides of the range.
type codeView struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Change string `json:"change"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func (s *snapshot) code(id string) (codeView, bool) {
	ix := tour.NewIndex(s.scene)
	node, ok := ix.Node(id)
	if !ok || node.File == "" {
		return codeView{}, false
	}
	recv := ""
	for _, pkg := range s.scene.Packages {
		for _, e := range pkg.Entities {
			if e.ID == id {
				recv = e.Recv
			}
		}
	}
	out := codeView{Name: node.Name, File: node.File, Change: node.Change}
	out.After = tour.Snippet(s.after[node.File], node, recv)
	if s.before != nil {
		out.Before = tour.Snippet(s.before[node.File], node, recv)
	}
	return out, true
}

// parseArgs lets flags and positional arguments mix, as in
// "tour validate file.json -range a..b".
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return rest, nil
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func commonFlags(fs *flag.FlagSet) (path, commitRange *string) {
	path = fs.String("path", ".", "path to a Go or Rust file or directory")
	fs.StringVar(path, "p", ".", "path to a Go or Rust file or directory")
	commitRange = fs.String("range", "", "commit range, A..B or A...B")
	fs.StringVar(commitRange, "r", "", "commit range, A..B or A...B")
	return path, commitRange
}

func runNodes(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("citydiff nodes", flag.ContinueOnError)
	path, commitRange := commonFlags(fs)
	changed := fs.Bool("changed", false, "only nodes that changed in the range")
	kind := fs.String("kind", "", "only this kind: package, external, type, function, method, variable")
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage: citydiff nodes [-path dir] [-range A..B] [-changed] [-kind k] [-json]\n\nLists every name a tour can use.\n\n")
		fs.PrintDefaults()
	}
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	snap, err := buildSnapshot(*path, *commitRange)
	if err != nil {
		return err
	}
	var nodes []tour.Node
	for _, n := range tour.NewIndex(snap.scene).Nodes {
		if *changed && (n.Change == "same" || n.Change == "") {
			continue
		}
		if *kind != "" && n.Kind != *kind {
			continue
		}
		nodes = append(nodes, n)
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	if *asJSON {
		if nodes == nil {
			nodes = []tour.Node{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(nodes)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, n := range nodes {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", n.Kind, n.Change, n.Name)
	}
	return tw.Flush()
}

const tourUsage = `Usage:
  citydiff tour validate [-path dir] [-range A..B] [-json] tour.json
  citydiff tour serve    [-path dir] [-range A..B] [-addr host:port] tour.json
  citydiff tour schema

-range defaults to the script's own "range".
`

func runTour(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, tourUsage)
		return errUsage
	}
	switch args[0] {
	case "schema":
		_, err := stdout.Write(tour.Schema)
		return err
	case "validate", "serve":
	case "-h", "--help", "help":
		fmt.Fprint(stdout, tourUsage)
		return nil
	default:
		fmt.Fprint(stderr, tourUsage)
		return errUsage
	}
	fs := flag.NewFlagSet("citydiff tour "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	path, commitRange := commonFlags(fs)
	asJSON := fs.Bool("json", false, "print the resolved script as JSON")
	addr := fs.String("addr", "127.0.0.1:8787", "listen address for serve")
	fs.Usage = func() { fmt.Fprint(stderr, tourUsage) }
	rest, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		fmt.Fprint(stderr, tourUsage)
		return errUsage
	}
	raw, err := os.ReadFile(rest[0])
	if err != nil {
		return err
	}
	script, err := tour.Parse(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if *commitRange == "" || (args[0] == "serve" && script.Range != "") {
		// serve shows the tour's own range, as a loaded tour does in the viewer.
		*commitRange = script.Range
	}
	snap, err := buildSnapshot(*path, *commitRange)
	if err != nil {
		return err
	}
	resolved, probs := snap.check(script)
	if len(probs) > 0 {
		for _, p := range probs {
			fmt.Fprintf(stderr, "%s: %s\n", rest[0], p)
		}
		return fmt.Errorf("%d problem(s) in %s", len(probs), rest[0])
	}
	if args[0] == "serve" {
		fmt.Fprintf(stderr, "citydiff: tour %q, %d steps\n", script.Title, len(script.Steps))
		return serve(*addr, snap, raw)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resolved)
	}
	fmt.Fprintf(stdout, "ok: %s, %d steps\n", rest[0], len(resolved.Steps))
	for i, s := range resolved.Steps {
		var parts []string
		t := s.Targets
		if t.Select != nil {
			parts = append(parts, "select "+t.Select.Name)
		}
		if t.Focus != nil {
			parts = append(parts, "focus "+t.Focus.Name)
		}
		for _, h := range t.Highlight {
			parts = append(parts, "highlight "+h.Name)
		}
		if len(t.Path) > 0 {
			names := make([]string, len(t.Path))
			for j, n := range t.Path {
				names[j] = n.Name
			}
			parts = append(parts, "path "+strings.Join(names, " -> "))
		}
		if t.Code != nil {
			parts = append(parts, "code "+t.Code.Name)
		}
		fmt.Fprintf(stdout, "  %2d. %s\n", i+1, s.Title)
		for _, p := range parts {
			fmt.Fprintf(stdout, "        %s\n", p)
		}
	}
	return nil
}

var errUsage = errors.New("usage")

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
