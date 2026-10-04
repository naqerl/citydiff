package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"betterdiff/lib"
	"betterdiff/lib/diff"
	"betterdiff/lib/git"
	"betterdiff/lib/parser/go"
)

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
	Left   *jsonDiffSide `json:"left,omitempty"`
	Right  *jsonDiffSide `json:"right,omitempty"`
}

func main() {
	path := flag.String("path", "", "path to the source file")
	flag.StringVar(path, "p", "", "path to the source file")
	commitRange := flag.String("range", "", "commit range to diff, A..B or A...B")
	flag.StringVar(commitRange, "r", "", "commit range to diff, A..B or A...B")
	asJSON := flag.Bool("json", false, "print entries as JSON")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: betterdiff [-json] -path file [-range A..B]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing file path: pass -path or -p")
		flag.Usage()
		os.Exit(2)
	}

	if *commitRange != "" {
		diffCommits(*path, *commitRange, *asJSON)
		return
	}

	src, err := os.ReadFile(*path)
	if err != nil {
		fail(err)
	}
	entries, err := golang.New().Parse(src)
	if err != nil {
		fail(err)
	}
	if err := printEntries(entries, *asJSON); err != nil {
		fail(err)
	}
}

func diffCommits(path, commitRange string, asJSON bool) {
	left, right, err := git.Versions(path, commitRange, golang.New())
	if err != nil {
		fail(err)
	}
	if err := printDiff(diff.Entries(left, right), asJSON); err != nil {
		fail(err)
	}
}

func printEntries(entries []lib.Entity, asJSON bool) error {
	if asJSON {
		out := make([]jsonEntry, len(entries))
		for i, entry := range entries {
			out[i] = jsonEntry{Kind: entry.Kind().String(), Entry: entry}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	for _, entry := range entries {
		fmt.Printf("%s %+v\n", entry.Kind(), entry)
	}
	return nil
}

func printDiff(changes []diff.Entry, asJSON bool) error {
	if asJSON {
		out := make([]jsonDiff, len(changes))
		for i, change := range changes {
			out[i] = jsonDiff{
				Action: change.Action.String(),
				Left:   diffSide(change.Left),
				Right:  diffSide(change.Right),
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	for _, change := range changes {
		switch change.Action {
		case diff.Added:
			fmt.Printf("added %s %s\n", change.Right.Kind(), formatEntity(change.Right))
		case diff.Removed:
			fmt.Printf("removed %s %s\n", change.Left.Kind(), formatEntity(change.Left))
		case diff.Modified:
			fmt.Printf("modified %s %s => %s\n", change.Right.Kind(), formatEntity(change.Left), formatEntity(change.Right))
		default:
			fmt.Printf("%s\n", change)
		}
	}
	return nil
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

// formatEntity prints one entity. Methods include BodyHash, which their
// String method leaves out, so a body-only change is visible.
func formatEntity(entry lib.Entity) string {
	method, ok := entry.(lib.MethodEntry)
	if !ok {
		return fmt.Sprintf("%+v", entry)
	}
	receiver := ""
	if method.Type != nil {
		receiver = method.Type.Name
	}
	return fmt.Sprintf("{Name:%s Parameters:%+v ReturnArgs:%+v BodyHash:%s Type:{Name:%s}}",
		method.Name, method.Parameters, method.ReturnArgs, method.BodyHash, receiver)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "betterdiff: %s\n", err)
	os.Exit(1)
}
