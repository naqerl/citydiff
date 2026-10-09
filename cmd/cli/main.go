package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"net/http"

	"citydiff/lib"
	"citydiff/lib/diff"
	"citydiff/lib/files"
	"citydiff/lib/git"
	"citydiff/lib/parser/go"
	"citydiff/lib/scene"
	"citydiff/view"
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
	path := flag.String("path", "", "path to a Go file or directory")
	flag.StringVar(path, "p", "", "path to a Go file or directory")
	commitRange := flag.String("range", "", "commit range to diff, A..B or A...B")
	flag.StringVar(commitRange, "r", "", "commit range to diff, A..B or A...B")
	asJSON := flag.Bool("json", false, "print entries as JSON")
	asScene := flag.Bool("scene", false, "print the 3D scene as JSON")
	asView := flag.Bool("view", false, "serve the 3D scene")
	addr := flag.String("addr", "127.0.0.1:8787", "listen address for -view")
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

	left, right, err := load(*path, *commitRange)
	if err != nil {
		fail(err)
	}
	if *asView {
		if err := serve(*addr, scene.Build(left, right)); err != nil {
			fail(err)
		}
		return
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
		return git.Versions(path, commitRange, golang.New())
	}
	src, err := files.Tree(path)
	if err != nil {
		return nil, nil, err
	}
	right, err = golang.New().Parse(src)
	return nil, right, err
}

func printScene(sc scene.Scene) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(sc)
}

func serve(addr string, sc scene.Scene) error {
	payload, err := json.Marshal(sc)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /scene.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
	mux.Handle("/", noStore(http.FileServer(http.FS(view.FS))))
	fmt.Fprintf(os.Stderr, "citydiff: http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
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
	return "Usage: citydiff [-json | -scene | -view] -path file-or-directory [-range A..B]\n\n"
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "citydiff: %s\n", err)
	os.Exit(1)
}
