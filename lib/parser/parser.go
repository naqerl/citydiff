// Package parser routes a snapshot to the language parser for each file.
package parser

import (
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"citydiff/lib"
	golang "citydiff/lib/parser/go"
	"citydiff/lib/parser/rust"
	"citydiff/lib/parser/swift"
)

// Language is one supported language: the files it reads and its parser.
type Language struct {
	Name    string
	Include func(base string) bool
	Parser  lib.Parser
}

// Languages are the supported languages.
var Languages = []Language{
	{
		Name:    "go",
		Include: func(base string) bool { return base == "go.mod" || strings.HasSuffix(base, ".go") },
		Parser:  golang.New(),
	},
	{
		Name:    "rust",
		Include: func(base string) bool { return base == "Cargo.toml" || strings.HasSuffix(base, ".rs") },
		Parser:  rust.New(),
	},
	{
		Name:    "swift",
		Include: func(base string) bool { return base == "Package.swift" || strings.HasSuffix(base, ".swift") },
		Parser:  swift.New(),
	},
}

// Include reports whether a slash-separated path belongs to a supported language.
func Include(name string) bool {
	_, ok := languageOf(name)
	return ok
}

func languageOf(name string) (int, bool) {
	base := path.Base(filepath.ToSlash(name))
	for i, lang := range Languages {
		if lang.Include(base) {
			return i, true
		}
	}
	return 0, false
}

// Parser parses a mixed snapshot. Each language parses its own files as one
// closed world. Parsed files keep the order of the stream.
type Parser struct{}

// New returns a parser over every supported language.
func New() Parser { return Parser{} }

// Parse implements lib.Parser.
func (Parser) Parse(src lib.Source) ([]lib.ParsedFile, error) {
	if src == nil {
		return nil, errors.New("nil source")
	}
	groups := make([][]lib.File, len(Languages))
	order := map[string]int{}
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		i, ok := languageOf(file.Path)
		if !ok {
			return nil, fmt.Errorf("%s is not a supported source file", file.Path)
		}
		order[path.Clean(filepath.ToSlash(file.Path))] = len(order)
		groups[i] = append(groups[i], file)
	}
	var out []lib.ParsedFile
	for i, files := range groups {
		if len(files) == 0 {
			continue
		}
		parsed, err := Languages[i].Parser.Parse(lib.Mem(files))
		if err != nil {
			return nil, err
		}
		out = append(out, parsed...)
	}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Path] < order[out[j].Path] })
	return out, nil
}

var _ lib.Parser = Parser{}
