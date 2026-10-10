package main

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"citydiff/lib/git"
)

// maxDiffFiles caps how many files a package's diff shows at once: a package
// can hold a whole change, and the page is a preview, not a review.
const maxDiffFiles = 8

// diffFile is one file of the reply: the hunks the page draws, how many of
// that file's hunks were left out because they are not the node's, and a note
// when the node's own lines are not what changed.
type diffFile struct {
	Path    string     `json:"path"`
	Lang    string     `json:"lang,omitempty"`
	Binary  bool       `json:"binary,omitempty"`
	Hunks   []git.Hunk `json:"hunks"`
	Omitted int        `json:"omitted,omitempty"`
}

// diffReply is what the page draws for D: the range it came from, and the
// files with the hunks that cover the selection.
type diffReply struct {
	Range string     `json:"range,omitempty"`
	Files []diffFile `json:"files"`
	More  int        `json:"more,omitempty"`
}

// serveDiff answers with the hunks of the selected node's change. An entity
// sends its file and the lines it covers; a package sends its directory.
func (v *viewer) serveDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v.mu.Lock()
	snap := v.snap
	v.mu.Unlock()
	if snap == nil {
		problem(w, "diff", errors.New("no scene is loaded"))
		return
	}
	patch, err := git.Patch(snap.path, snap.commitRange, 3)
	if err != nil {
		problem(w, "diff", err)
		return
	}
	reply := diffReply{Range: snap.commitRange, Files: []diffFile{}}
	if dir := q.Get("dir"); dir != "" {
		reply.Files, reply.More = packageDiff(patch, dir)
	} else if file := q.Get("file"); file != "" {
		reply.Files = nodeDiff(patch, file, atoi(q.Get("line")), atoi(q.Get("end")), q.Get("side"))
	}
	writeJSON(w, http.StatusOK, reply)
}

// nodeDiff is the file's hunks whose own changed lines are inside the node,
// and a count of the ones that are not. Only the node's own hunks: a diff of
// the whole file would answer a question nobody asked, and the hunks around
// the node are the ones that belong to it.
//
// A node whose own lines are untouched gets no hunks and an omitted count, and
// the page says so rather than showing the file's change under the node's
// name.
//
// side says which side of the diff the node's lines are numbered on: a
// removed declaration only has lines on the old side.
func nodeDiff(patch []git.FileDiff, file string, line, end int, side string) []diffFile {
	for _, fd := range patch {
		if fd.File != file {
			continue
		}
		out := diffFile{Path: fd.File, Lang: languageOf(fd.File), Binary: fd.Binary}
		if line <= 0 {
			out.Hunks = fd.Hunks
			return []diffFile{out}
		}
		for _, hunk := range fd.Hunks {
			if hunkCovers(hunk, line, end, side) {
				out.Hunks = append(out.Hunks, hunk)
				continue
			}
			out.Omitted++
		}
		return []diffFile{out}
	}
	return []diffFile{}
}

// hunkCovers is true when one of the hunk's changed lines sits inside the
// node. Context lines do not count: a hunk that merely passes the node's
// boundary belongs to the declaration on the other side of it.
//
// end is the line after the node's last. When the page does not know it — the
// node is the last declaration in its file, so there is no next one to bound
// it — the node runs to the end of the file and every changed line below it
// counts. Guessing one line instead cut such a node off at its first line,
// which made a body change read as someone else's.
func hunkCovers(hunk git.Hunk, line, end int, side string) bool {
	if end <= line {
		end = 1 << 30
	}
	old, fresh := hunk.OldStart, hunk.NewStart
	for _, l := range hunk.Lines {
		switch l.Kind {
		case "add":
			if side != "old" && fresh >= line && fresh < end {
				return true
			}
			fresh++
		case "del":
			if side == "old" && old >= line && old < end {
				return true
			}
			old++
		default:
			old++
			fresh++
		}
	}
	return false
}

// packageDiff is every changed file under a package's directory, capped, with
// a count of the files the cap left out.
func packageDiff(patch []git.FileDiff, dir string) ([]diffFile, int) {
	dir = strings.TrimSuffix(strings.TrimPrefix(dir, "./"), "/")
	var out []diffFile
	more := 0
	for _, fd := range patch {
		if !underDir(fd.File, dir) {
			continue
		}
		if len(out) == maxDiffFiles {
			more++
			continue
		}
		out = append(out, diffFile{Path: fd.File, Lang: languageOf(fd.File), Binary: fd.Binary, Hunks: fd.Hunks})
	}
	return out, more
}

// underDir is true when the file is the directory itself or lives under it.
// The root package, whose directory is ".", holds every file.
func underDir(file, dir string) bool {
	if dir == "" || dir == "." {
		return true
	}
	return file == dir || strings.HasPrefix(file, dir+"/")
}

// languageOf is the language the page highlights the diff as, from the file
// name. Anything else is left to the page's plain text handling.
func languageOf(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".swift":
		return "swift"
	}
	return ""
}
