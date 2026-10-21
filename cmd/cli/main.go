package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

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
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "nodes":
			err = runNodes(os.Args[2:], os.Stdout)
		case "tour":
			err = runTour(os.Args[2:], os.Stdout, os.Stderr)
		case "hub":
			err = runObserve(os.Args[2:], os.Stdout, os.Stderr)
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
	path := flag.String("path", "", "path to a Go, Rust, Swift or JavaScript file or directory")
	flag.StringVar(path, "p", "", "path to a Go, Rust, Swift or JavaScript file or directory")
	commitRange := flag.String("range", "", "commit range A..B or A...B, or one ref for a snapshot of the tree there")
	flag.StringVar(commitRange, "r", "", "commit range A..B or A...B, or one ref for a snapshot of the tree there")
	asJSON := flag.Bool("json", false, "print entries as JSON")
	asScene := flag.Bool("scene", false, "print the 3D scene as JSON")
	asText := flag.Bool("text", false, "print the plain text diff or file list")
	addr := flag.String("addr", "127.0.0.1:8787", "listen address for the viewer")
	tourFile := flag.String("tour", "", "tour script for the viewer to load and play")
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

	// With no output flag the viewer is served, so the three printing flags are
	// mutually exclusive: naming two of them is a mistake, not a preference.
	modes := 0
	for _, on := range []bool{*asJSON, *asScene, *asText} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		usageErr("pass at most one of -json, -scene, -text")
	}
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if modes == 1 && (set["addr"] || set["tour"]) {
		usageErr("-addr and -tour only apply when serving the viewer; they do nothing with -json, -scene or -text")
	}

	if modes == 0 {
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
	left, right, isSnapshot, err := load(*path, *commitRange)
	if err != nil {
		fail(err)
	}
	if *asScene {
		sc := scene.Build(left, right)
		sc.Range = *commitRange
		if err := printScene(sc); err != nil {
			fail(err)
		}
		return
	}
	if *commitRange != "" && !isSnapshot {
		if err := printDiff(diff.Files(left, right), *asJSON); err != nil {
			fail(err)
		}
		return
	}
	if err := printFiles(right, *asJSON); err != nil {
		fail(err)
	}
}

// load reads the side or sides of commitRange. A one-ref spec is a
// snapshot: right is the tree at that ref and left is nil, so a caller can
// tell it apart from a diff (an empty, non-nil left side).
func load(path, commitRange string) (left, right []lib.ParsedFile, isSnapshot bool, err error) {
	if commitRange != "" {
		spec, err := git.ParseSpec(commitRange)
		if err != nil {
			return nil, nil, false, err
		}
		left, right, err = git.Versions(path, commitRange, parser.New())
		return left, right, spec.Snapshot, err
	}
	src, err := files.Tree(path)
	if err != nil {
		return nil, nil, false, err
	}
	right, err = parser.New().Parse(src)
	return nil, right, false, err
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
	// Listen before opening the browser, so the page it asks for is already
	// being accepted, and a port that is taken opens nothing.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "citydiff: http://%s\n", addr)
	go v.watchUpdate(latestAPI(releaseRepo))
	openBrowser(browserURL(ln.Addr()))
	return http.Serve(ln, v.handler())
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

// usageErr reports a bad flag combination the way the flag package reports an
// unknown flag: to stderr, with the usage, and exit status 2.
func usageErr(msg string) {
	fmt.Fprintf(os.Stderr, "citydiff: %s\n", msg)
	flag.Usage()
	os.Exit(2)
}

func usageText() string {
	return "Usage: citydiff -path file-or-directory [-range A..B|REF] [-addr addr] [-tour file]\n" +
		"       citydiff [-json | -scene | -text] -path file-or-directory [-range A..B|REF]\n" +
		"       citydiff nodes [-path dir] [-range A..B|REF] [-changed] [-kind k] [-json]\n" +
		"       citydiff tour validate|serve|schema ...\n" +
		"       citydiff hub serve|open ...\n\n" +
		"With no output flag, citydiff serves the 3D viewer and does not return.\n\n"
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "citydiff: %s\n", err)
	os.Exit(1)
}
