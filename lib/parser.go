package lib

import "io"

// File is one file from a snapshot. Path is slash-separated and relative to
// the snapshot root. Src is the file contents.
type File struct {
	Path string
	Src  []byte
}

// Source yields the files of one snapshot. Next returns io.EOF after the last file.
type Source interface {
	Next() (File, error)
}

// ParsedFile is the entities parsed from one file.
// Package is the package clause. ImportPath is the module-relative import
// path, filled when the snapshot contains the go.mod that covers the file.
// Module is that go.mod module path. Files in one directory share an import
// path, including an external test package in the same directory.
type ParsedFile struct {
	Path       string
	Package    string
	ImportPath string
	Module     string
	Entities   []Entity
}

// Mem is a Source over files in order.
func Mem(files []File) Source {
	return &mem{files: files}
}

type mem struct {
	files []File
	i     int
}

func (m *mem) Next() (File, error) {
	if m.i >= len(m.files) {
		return File{}, io.EOF
	}
	file := m.files[m.i]
	m.i++
	return file, nil
}

// Parser turns a snapshot into language-agnostic entities.
// Language implementations live under lib/parser.
// The stream is the closed world for one parse: declarations and calls are
// resolved after every file has been read, so file order does not matter.
type Parser interface {
	Parse(src Source) ([]ParsedFile, error)
}
