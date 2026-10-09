// Package git reads both sides of a commit range with go-git and parses
// each side into language-agnostic entities.
package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"betterdiff/lib"
	"betterdiff/lib/files"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Versions parses path at each side of commitRange.
//
// path is a file or directory inside a worktree. The repository is the
// nearest parent directory that contains .git. A file yields that file.
// A directory yields the Go files and go.mod files under it. Paths in the
// snapshot are relative to path, matching files.Tree. commitRange uses
// Git's two-dot and three-dot notation. A..B compares those two commits.
// A...B compares their merge base with B, which is how git diff treats a
// three-dot range. An empty side means HEAD.
//
// parser turns each side's files into entities, so this package stays
// free of any one language. Each side is its own snapshot. A file missing
// on one side is absent from that side. A requested file missing from both
// commits is an error.
func Versions(path, commitRange string, parser lib.Parser) (left, right []lib.ParsedFile, err error) {
	if parser == nil {
		return nil, nil, errors.New("nil parser")
	}
	if strings.TrimSpace(path) == "" {
		return nil, nil, errors.New("empty file path")
	}
	span, err := splitRange(commitRange)
	if err != nil {
		return nil, nil, err
	}

	repo, gitPath, disk, err := openPath(path)
	if err != nil {
		return nil, nil, err
	}
	leftCommit, err := resolve(repo, span.left)
	if err != nil {
		return nil, nil, err
	}
	rightCommit, err := resolve(repo, span.right)
	if err != nil {
		return nil, nil, err
	}
	if span.threeDot {
		leftCommit, err = mergeBase(leftCommit, rightCommit)
		if err != nil {
			return nil, nil, fmt.Errorf("merge base for %q: %w", strings.TrimSpace(commitRange), err)
		}
	}
	dir, err := directorySnapshot(disk, gitPath, leftCommit, rightCommit)
	if err != nil {
		return nil, nil, err
	}

	leftFiles, leftOK, err := listFiles(leftCommit, gitPath, !dir)
	if err != nil {
		return nil, nil, err
	}
	rightFiles, rightOK, err := listFiles(rightCommit, gitPath, !dir)
	if err != nil {
		return nil, nil, err
	}
	if !dir && !leftOK && !rightOK {
		return nil, nil, fmt.Errorf("file %s not found in %s or %s", gitPath, leftCommit.Hash, rightCommit.Hash)
	}
	left, err = parser.Parse(lib.Mem(leftFiles))
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s at %s: %w", gitPath, leftCommit.Hash, err)
	}
	right, err = parser.Parse(lib.Mem(rightFiles))
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s at %s: %w", gitPath, rightCommit.Hash, err)
	}
	return left, right, nil
}

// Tree reads the Go files and go.mod files at rev under worktree.
// worktree is a file or directory inside the repository. An empty rev
// means HEAD. Paths in the stream are slash-separated and relative to
// worktree: the base name when worktree is a file, and paths relative to
// the directory when worktree is a directory. vendor and hidden directories
// are skipped.
func Tree(worktree, rev string) (lib.Source, error) {
	if strings.TrimSpace(worktree) == "" {
		return nil, errors.New("empty worktree")
	}
	repo, gitPath, disk, err := openPath(worktree)
	if err != nil {
		return nil, err
	}
	commit, err := resolve(repo, rev)
	if err != nil {
		return nil, err
	}
	dir, err := directorySnapshot(disk, gitPath, commit)
	if err != nil {
		return nil, err
	}
	found, _, err := listFiles(commit, gitPath, !dir)
	if err != nil {
		return nil, err
	}
	return lib.Mem(found), nil
}

// span is one revision range. Empty left or right means HEAD.
type span struct {
	left, right string
	threeDot    bool
}

func splitRange(spec string) (span, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return span{}, errors.New("empty commit range")
	}
	sep := ".."
	three := false
	if strings.Contains(spec, "...") {
		sep = "..."
		three = true
	}
	left, right, ok := strings.Cut(spec, sep)
	if !ok || strings.Contains(left, "..") || strings.Contains(right, "..") {
		return span{}, fmt.Errorf("commit range %q must use .. or ...", spec)
	}
	return span{
		left:     strings.TrimSpace(left),
		right:    strings.TrimSpace(right),
		threeDot: three,
	}, nil
}

// diskKind is what the worktree path is. A missing path is classified from
// the commits instead.
type diskKind int

const (
	diskMissing diskKind = iota
	diskFile
	diskDir
)

func openPath(file string) (*gogit.Repository, string, diskKind, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, "", diskMissing, err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	kind := diskMissing
	if info, err := os.Stat(abs); err == nil {
		if info.IsDir() {
			kind = diskDir
		} else {
			kind = diskFile
		}
	}

	repo, err := openRepo(abs)
	if err != nil {
		return nil, "", diskMissing, fmt.Errorf("open repository for %s: %w", file, err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, "", diskMissing, fmt.Errorf("worktree for %s: %w", file, err)
	}
	root := worktree.Filesystem.Root()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, "", diskMissing, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", diskMissing, fmt.Errorf("file %s is outside the repository", file)
	}
	if rel == "." {
		return repo, "", diskDir, nil
	}
	return repo, filepath.ToSlash(rel), kind, nil
}

// directorySnapshot reports whether path should be read as a tree.
// A directory in the worktree is a tree. A missing worktree path, or a
// worktree file whose object is a tree in either commit, follows the commits.
func directorySnapshot(disk diskKind, gitPath string, commits ...*object.Commit) (bool, error) {
	if disk == diskDir || gitPath == "" || gitPath == "." {
		return true, nil
	}
	for _, commit := range commits {
		kind, err := commitPathKind(commit, gitPath)
		if err != nil {
			return false, err
		}
		if kind == diskDir {
			return true, nil
		}
	}
	return false, nil
}

func commitPathKind(commit *object.Commit, gitPath string) (diskKind, error) {
	if gitPath == "" || gitPath == "." {
		return diskDir, nil
	}
	tree, err := commit.Tree()
	if err != nil {
		return diskMissing, fmt.Errorf("tree at %s: %w", commit.Hash, err)
	}
	entry, err := tree.FindEntry(gitPath)
	if err != nil {
		if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrDirectoryNotFound) || errors.Is(err, object.ErrFileNotFound) {
			return diskMissing, nil
		}
		return diskMissing, fmt.Errorf("stat %s at %s: %w", gitPath, commit.Hash, err)
	}
	if entry.Mode == filemode.Dir {
		return diskDir, nil
	}
	return diskFile, nil
}

func openRepo(path string) (*gogit.Repository, error) {
	start := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		start = filepath.Dir(path)
	}
	return gogit.PlainOpenWithOptions(start, &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
}

func resolve(repo *gogit.Repository, rev string) (*object.Commit, error) {
	if rev == "" {
		rev = "HEAD"
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", rev, err)
	}
	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", hash, err)
	}
	return commit, nil
}

// mergeBase returns the first best common ancestor of a and b.
func mergeBase(a, b *object.Commit) (*object.Commit, error) {
	bases, err := a.MergeBase(b)
	if err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		return nil, errors.New("no common ancestor")
	}
	return bases[0], nil
}

// listFiles reads Go files and go.mod files at commit.
// exact selects one path and yields its base name. Otherwise prefix is a
// directory, paths are relative to that directory, and an empty prefix is
// the whole tree. ok is false only when an exact file is missing.
// A path with a vendor or hidden segment is omitted, matching files.Tree.
func listFiles(commit *object.Commit, prefix string, exact bool) ([]lib.File, bool, error) {
	if exact {
		if skippedPath(prefix) {
			return nil, false, nil
		}
		src, ok, err := blob(commit, prefix)
		if err != nil || !ok {
			return nil, ok, err
		}
		if !files.Include(prefix) {
			return nil, false, fmt.Errorf("%s is not a Go file", prefix)
		}
		return []lib.File{{Path: path.Base(prefix), Src: src}}, true, nil
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, false, fmt.Errorf("tree at %s: %w", commit.Hash, err)
	}
	iter := tree.Files()
	defer iter.Close()
	var out []lib.File
	for {
		file, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("tree at %s: %w", commit.Hash, err)
		}
		if skippedPath(file.Name) || !under(file.Name, prefix) || !files.Include(file.Name) {
			continue
		}
		if file.Mode == filemode.Symlink || !file.Mode.IsFile() {
			continue
		}
		text, err := file.Contents()
		if err != nil {
			return nil, false, fmt.Errorf("read %s at %s: %w", file.Name, commit.Hash, err)
		}
		out = append(out, lib.File{Path: relTo(prefix, file.Name), Src: []byte(text)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, true, nil
}

func relTo(prefix, name string) string {
	if prefix == "" || prefix == "." {
		return name
	}
	if name == prefix {
		return path.Base(name)
	}
	if rest, ok := strings.CutPrefix(name, prefix+"/"); ok {
		return rest
	}
	return name
}

func skippedPath(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if files.SkipDir(seg) {
			return true
		}
	}
	return false
}

func under(name, prefix string) bool {
	if prefix == "" || prefix == "." {
		return true
	}
	return name == prefix || strings.HasPrefix(name, prefix+"/")
}

func blob(commit *object.Commit, path string) ([]byte, bool, error) {
	file, err := commit.File(path)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s at %s: %w", path, commit.Hash, err)
	}
	if file.Mode == filemode.Symlink || !file.Mode.IsFile() {
		return nil, false, fmt.Errorf("%s at %s is not a file", path, commit.Hash)
	}
	text, err := file.Contents()
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %s: %w", path, commit.Hash, err)
	}
	return []byte(text), true, nil
}
