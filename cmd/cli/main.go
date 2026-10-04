package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"betterdiff/lib"
	"betterdiff/lib/parser/go"
)

type jsonEntry struct {
	Kind  string     `json:"kind"`
	Entry lib.Entity `json:"entry"`
}

func main() {
	path := flag.String("path", "", "path to the source file")
	flag.StringVar(path, "p", "", "path to the source file")
	asJSON := flag.Bool("json", false, "print parsed entries as JSON")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: betterdiff [-json] -path file\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing file path: pass -path or -p")
		flag.Usage()
		os.Exit(2)
	}

	src, err := os.ReadFile(*path)
	if err != nil {
		fail(err)
	}

	entries, err := golang.New().Parse(src)
	if err != nil {
		fail(err)
	}

	if *asJSON {
		out := make([]jsonEntry, len(entries))
		for i, entry := range entries {
			out[i] = jsonEntry{Kind: entry.Kind().String(), Entry: entry}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fail(err)
		}
		return
	}

	for _, entry := range entries {
		fmt.Printf("%s %+v\n", entry.Kind(), entry)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "betterdiff: %s\n", err)
	os.Exit(1)
}
