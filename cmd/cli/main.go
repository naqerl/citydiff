package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"net/http"

	"citydiff/lib"
	"citydiff/lib/diff"
	"citydiff/lib/files"
	"citydiff/lib/git"
	"citydiff/lib/parser"
	"citydiff/lib/scene"
)

type jsonFile struct {
	Path       string      `json:"path"`
	Package    string      `json:"package,omitempty"`
	ImportPath string      `json:"importPath,omitempty"`
	Module     string      `json:"module,omitempty"`
	Entries    []jsonEntry `json:"entries"`
}

type jsonEntry struct {
	Kind  string     `json:"kind"`
	Entry lib.Entity `json:"entry"`
}

// jsonDiffSide is one side of a diff. Body and methods hashes sit beside
// entry because those fields are omitted from entity JSON.
type jsonDiffSide struct {
	Kind        string     `json:"kind"`
	Entry       lib.Entity `json:"entry"`
	BodyHash    string     `json:"bodyHash,omitempty"`
	MethodsHash string     `json:"methodsHash,omitempty"`
}

type jsonDiff struct {
	Action string        `json:"action"`
	Kind   string        `json:"kind,omitempty"`
	Name   string        `json:"name,omitempty"`
	Left   *jsonDiffSide `json:"left,omitempty"`
	Right  *jsonDiffSide `json:"right,omitempty"`
	Edits  []diff.Edit   `json:"edits,omitempty"`
}

type jsonFileDiff struct {
	Action  string     `json:"action"`
	Path    string     `json:"path"`
	Changes []jsonDiff `json:"changes,omitempty"`
}

func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "nodes":
			err = runNodes(os.Args[2:], os.Stdout)
		case "tour":
			err = runTour(os.Args[2:], os.Stdout, os.Stderr)
		default:
			legacy()
			return
		}
		switch {
		case errors.Is(err, errUsage), errors.Is(err, flag.ErrHelp):
			os.Exit(2)
		case err != nil:
			fail(err)
		}
		return
	}
	legacy()
}

func legacy() {
	path := flag.String("path", "", "path to a Go, Rust or Swift file or directory")
	flag.StringVar(path, "p", "", "path to a Go, Rust or Swift file or directory")
	commitRange := flag.String("range", "", "commit range to diff, A..B or A...B")
	flag.StringVar(commitRange, "r", "", "commit range to diff, A..B or A...B")
	asJSON := flag.Bool("json", false, "print entries as JSON")
	asScene := flag.Bool("scene", false, "print the 3D scene as JSON")
	asView := flag.Bool("view", false, "serve the 3D scene")
	addr := flag.String("addr", "127.0.0.1:8787", "listen address for -view")
	tourFile := flag.String("tour", "", "tour script for -view to load and play")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usageText())
		flag.PrintDefaults()
	}
	flag.Parse()

	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing file path: pass -path or -p")
		flag.Usage()
		os.Exit(2)
	}

	if *asView {
		snap, err := buildSnapshot(*path, *commitRange)
		if err != nil {
			fail(err)
		}
		var raw []byte
		if *tourFile != "" {
			if raw, err = os.ReadFile(*tourFile); err != nil {
				fail(err)
			}
		}
		if err := serve(*addr, snap, raw); err != nil {
			fail(err)
		}
		return
	}
	left, right, err := load(*path, *commitRange)
	if err != nil {
		fail(err)
	}
	if *asScene {
		if err := printScene(scene.Build(left, right)); err != nil {
			fail(err)
		}
		return
	}
	if *commitRange != "" {
		if err := printDiff(diff.Files(left, right), *asJSON); err != nil {
			fail(err)
		}
		return
	}
	if err := printFiles(right, *asJSON); err != nil {
		fail(err)
	}
}

func load(path, commitRange string) (left, right []lib.ParsedFile, err error) {
	if commitRange != "" {
		return git.Versions(path, commitRange, parser.New())
	}
	src, err := files.Tree(path)
	if err != nil {
		return nil, nil, err
	}
	right, err = parser.New().Parse(src)
	return nil, right, err
}

func printScene(sc scene.Scene) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(sc)
}

func serve(addr string, snap *snapshot, tourRaw []byte) error {
	v, err := newViewer(snap, tourRaw)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "citydiff: http://%s\n", addr)
	return http.ListenAndServe(addr, v.handler())
}

// The embedded viewer files carry no modification time, so a browser has
// nothing to revalidate against and can keep running an old build from cache.
// They are small, so serve them fresh.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func printFiles(files []lib.ParsedFile, asJSON bool) error {
	if asJSON {
		out := make([]jsonFile, len(files))
		for i, file := range files {
			entries := make([]jsonEntry, len(file.Entities))
			for j, entry := range file.Entities {
				entries[j] = jsonEntry{Kind: entry.Kind().String(), Entry: entry}
			}
			out[i] = jsonFile{
				Path:       file.Path,
				Package:    file.Package,
				ImportPath: file.ImportPath,
				Module:     file.Module,
				Entries:    entries,
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	for _, file := range files {
		fmt.Printf("%s\n", file.Path)
		for _, entry := range file.Entities {
			fmt.Printf("%s %+v\n", entry.Kind(), entry)
		}
	}
	return nil
}

func printDiff(changes []diff.FileChange, asJSON bool) error {
	if asJSON {
		out := make([]jsonFileDiff, len(changes))
		for i, file := range changes {
			parts := make([]jsonDiff, len(file.Changes))
			for j, change := range file.Changes {
				parts[j] = jsonDiffFrom(change)
			}
			out[i] = jsonFileDiff{Action: file.Action.String(), Path: file.Path, Changes: parts}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	for _, file := range changes {
		fmt.Printf("%s %s\n", file.Action, file.Path)
		for _, change := range file.Changes {
			switch change.Action {
			case diff.Added:
				fmt.Printf("  added %s %s\n", change.Right.Kind(), formatEntity(change.Right))
			case diff.Removed:
				fmt.Printf("  removed %s %s\n", change.Left.Kind(), formatEntity(change.Left))
			case diff.Modified:
				fmt.Printf("  %s\n", change.String())
				for _, edit := range change.Edits {
					fmt.Printf("    %s\n", edit)
				}
			default:
				fmt.Printf("  %s\n", change)
			}
		}
	}
	return nil
}

func jsonDiffFrom(change diff.Entry) jsonDiff {
	out := jsonDiff{Action: change.Action.String()}
	switch change.Action {
	case diff.Modified:
		out.Kind = change.Right.Kind().String()
		out.Name = change.Name()
		out.Edits = change.Edits
	case diff.Removed:
		out.Left = diffSide(change.Left)
	default:
		out.Right = diffSide(change.Right)
	}
	return out
}

func diffSide(entry lib.Entity) *jsonDiffSide {
	if entry == nil {
		return nil
	}
	side := &jsonDiffSide{Kind: entry.Kind().String(), Entry: entry}
	switch entry := entry.(type) {
	case lib.FunctionEntry:
		side.BodyHash = entry.BodyHash
	case lib.MethodEntry:
		side.BodyHash = entry.BodyHash
	case lib.TypeEntry:
		side.MethodsHash = entry.MethodsHash
	}
	return side
}

// formatEntity prints one added or removed entity. Methods include Calls and
// BodyHash, which their String method leaves out.
func formatEntity(entry lib.Entity) string {
	method, ok := entry.(lib.MethodEntry)
	if !ok {
		return fmt.Sprintf("%+v", entry)
	}
	receiver := ""
	if method.Type != nil {
		receiver = method.Type.Name
	}
	return fmt.Sprintf("{Name:%s Parameters:%+v ReturnArgs:%+v Calls:%+v BodyHash:%s Type:{Name:%s}}",
		method.Name, method.Parameters, method.ReturnArgs, method.Calls, method.BodyHash, receiver)
}

func usageText() string {
	return "Usage: citydiff [-json | -scene | -view [-tour file]] -path file-or-directory [-range A..B]\n" +
		"       citydiff nodes [-path dir] [-range A..B] [-changed] [-kind k] [-json]\n" +
		"       citydiff tour validate|serve|schema ...\n\n"
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "citydiff: %s\n", err)
	os.Exit(1)
}
