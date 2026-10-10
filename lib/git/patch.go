package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/diff"
)

// A FileDiff is one file's change across a range, cut into hunks. The page
// prints it with a gutter on each side, so both line numbers are kept: a line
// that is not on one side has no number there.
//
// Path is the file's name in the repository and File is its name the way the
// scene has it, relative to the path citydiff was pointed at.
type FileDiff struct {
	Path   string `json:"path"`
	File   string `json:"file"`
	Binary bool   `json:"binary,omitempty"`
	Hunks  []Hunk `json:"hunks,omitempty"`
}

// Hunk is a run of changed lines and the lines around them.
type Hunk struct {
	OldStart int    `json:"oldStart"`
	OldLines int    `json:"oldLines"`
	NewStart int    `json:"newStart"`
	NewLines int    `json:"newLines"`
	Lines    []Line `json:"lines"`
}

// Line is one line of a hunk. Kind is "context", "add" or "del".
type Line struct {
	Kind string `json:"kind"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

// Patch is the change across commitRange, one FileDiff per changed file, in
// the order go-git reports them. go-git resolves both ends of the range and
// patches their trees against each other; the line chunks it returns are cut
// into hunks with contextLines lines either side of every change.
//
// An empty range has no two sides to compare and is an error rather than an
// empty diff.
func Patch(path, commitRange string, contextLines int) ([]FileDiff, error) {
	if strings.TrimSpace(commitRange) == "" {
		return nil, errors.New("this view is one snapshot: start citydiff with -range to diff a node")
	}
	if contextLines < 0 {
		contextLines = 0
	}
	repo, gitPath, _, err := openPath(path)
	if err != nil {
		return nil, err
	}
	leftRev, rightRev, err := Revs(path, commitRange)
	if err != nil {
		return nil, err
	}
	left, err := resolve(repo, leftRev)
	if err != nil {
		return nil, err
	}
	right, err := resolve(repo, rightRev)
	if err != nil {
		return nil, err
	}
	patch, err := left.PatchContext(context.Background(), right)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", commitRange, err)
	}
	var out []FileDiff
	for _, fp := range patch.FilePatches() {
		// A rename shows the same change under two names; the new name is the
		// one the scene has, and a deleted file only has the old one.
		name := patchPath(fp)
		if name == "" {
			continue
		}
		diff := buildFileDiff(name, fp, contextLines)
		diff.File = scenePath(gitPath, name)
		out = append(out, diff)
	}
	return out, nil
}

// patchPath is the file's name in the repository: the name on the new side,
// or the old one for a file the range deleted.
func patchPath(fp diff.FilePatch) string {
	from, to := fp.Files()
	if to != nil {
		return to.Path()
	}
	if from != nil {
		return from.Path()
	}
	return ""
}

// scenePath is the repository's name for the file turned back into the name
// the scene uses: relative to the path citydiff serves.
func scenePath(gitPath, name string) string {
	if gitPath == "" {
		return name
	}
	if trimmed := strings.TrimPrefix(name, gitPath+"/"); trimmed != name {
		return trimmed
	}
	return name
}

// buildFileDiff numbers the chunks and cuts them into hunks. go-git's chunks
// are line oriented and cover the whole file from its first line, so the
// numbering is the file's own.
func buildFileDiff(name string, fp diff.FilePatch, contextLines int) FileDiff {
	out := FileDiff{Path: name}
	if fp.IsBinary() {
		out.Binary = true
		return out
	}
	oldNo, newNo := 1, 1
	var all []Line
	for _, chunk := range fp.Chunks() {
		kind := "context"
		switch chunk.Type() {
		case diff.Add:
			kind = "add"
		case diff.Delete:
			kind = "del"
		}
		for _, text := range splitLines(chunk.Content()) {
			line := Line{Kind: kind, Text: text}
			if kind != "add" {
				line.Old = oldNo
				oldNo++
			}
			if kind != "del" {
				line.New = newNo
				newNo++
			}
			all = append(all, line)
		}
	}
	out.Hunks = cutHunks(all, contextLines)
	return out
}

// splitLines splits a chunk into lines, without the newline that ends each
// one: the page draws its own.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// cutHunks groups the changes into hunks: a run of changes with up to
// contextLines unchanged lines between them stays one hunk, and every hunk
// carries contextLines lines either side. A hunk's start is the position of its first line on each side,
// which is the line before it when the hunk opens with an insertion.
func cutHunks(all []Line, contextLines int) []Hunk {
	var out []Hunk
	for i := 0; i < len(all); {
		if all[i].Kind == "context" {
			i++
			continue
		}
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i
		for end < len(all) {
			if all[end].Kind != "context" {
				end++
				continue
			}
			// Look past the unchanged run: a change close enough behind it
			// belongs to this hunk, one further away opens the next.
			next := end
			for next < len(all) && all[next].Kind == "context" {
				next++
			}
			if next < len(all) && next-end <= 2*contextLines {
				end = next
				continue
			}
			break
		}
		stop := end + contextLines
		if stop > len(all) {
			stop = len(all)
		}
		out = append(out, cutHunk(all, start, stop))
		i = stop
	}
	return out
}

// cutHunk is all[start:stop] with the numbers of where it begins on each
// side: the first line that has one, less the lines before it in the hunk.
func cutHunk(all []Line, start, stop int) Hunk {
	hunk := Hunk{}
	for i := start; i < stop; i++ {
		line := all[i]
		hunk.Lines = append(hunk.Lines, line)
		if line.Kind != "add" {
			hunk.OldLines++
			if hunk.OldStart == 0 {
				hunk.OldStart = line.Old
			}
		}
		if line.Kind != "del" {
			hunk.NewLines++
			if hunk.NewStart == 0 {
				hunk.NewStart = line.New
			}
		}
	}
	if hunk.OldStart == 0 {
		hunk.OldStart = openingNumber(all, start, func(l Line) int { return l.Old })
	}
	if hunk.NewStart == 0 {
		hunk.NewStart = openingNumber(all, start, func(l Line) int { return l.New })
	}
	return hunk
}

// openingNumber is the line number a hunk starts at on one side when its
// first line is not on that side: the last number before it, plus one, which
// is where the insertion or deletion sits.
func openingNumber(all []Line, start int, number func(Line) int) int {
	for i := start - 1; i >= 0; i-- {
		if n := number(all[i]); n > 0 {
			return n + 1
		}
	}
	return 1
}
