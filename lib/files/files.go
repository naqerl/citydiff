// Package files reads a tree of files from the filesystem.
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"citydiff/lib"
	"citydiff/lib/parser"
)

// Include reports whether a slash-separated path belongs to a supported
// language: Go source or go.mod, Rust source or Cargo.toml.
func Include(name string) bool {
	return parser.Include(name)
}

// Tree reads root into a snapshot.
//
// A file yields that file when Include accepts it. A directory yields
// those files under it. Paths are slash-separated and relative to the walk
// root: the file's directory when root is a file, and root itself when root
// is a directory. Hidden directories and vendor are skipped. The result is
// in lexical path order.
func Tree(root string) (lib.Source, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("empty path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !Include(abs) {
			return nil, errors.New(root + " is not a Go or Rust file")
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		return lib.Mem([]lib.File{{
			Path: filepath.ToSlash(filepath.Base(abs)),
			Src:  src,
		}}), nil
	}

	var found []lib.File
	err = filepath.WalkDir(abs, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != abs && SkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !Include(name) {
			return nil
		}
		rel, err := filepath.Rel(abs, name)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		found = append(found, lib.File{Path: filepath.ToSlash(rel), Src: src})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return lib.Mem(found), nil
}

// SkipDir reports whether a directory name is left out of a snapshot.
// vendor, Cargo's target and hidden directories are not module source.
func SkipDir(name string) bool {
	return name == "vendor" || name == "target" || strings.HasPrefix(name, ".")
}
