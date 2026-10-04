// Package git reads both sides of a commit range with go-git and parses
// each side into language-agnostic entities.
package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"betterdiff/lib"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Versions parses file at each side of commitRange.
//
// file is a path to a file inside a worktree. The repository is the
// nearest parent directory that contains .git. commitRange uses Git's
// two-dot and three-dot notation. A..B compares those two commits.
// A...B compares their merge base with B, which is how git diff treats
// a three-dot range. An empty side means HEAD.
//
// parser turns each side's bytes into entities, so this package stays
// free of any one language. A side that does not contain the file
// yields a nil slice. The file missing from both commits is an error.
func Versions(file, commitRange string, parser lib.Parser) (left, right []lib.Entity, err error) {
	if parser == nil {
		return nil, nil, errors.New("nil parser")
	}
	if strings.TrimSpace(file) == "" {
		return nil, nil, errors.New("empty file path")
	}
	span, err := splitRange(commitRange)
	if err != nil {
		return nil, nil, err
	}

	repo, gitPath, err := openFile(file)
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

	leftSrc, leftOK, err := blob(leftCommit, gitPath)
	if err != nil {
		return nil, nil, err
	}
	rightSrc, rightOK, err := blob(rightCommit, gitPath)
	if err != nil {
		return nil, nil, err
	}
	if !leftOK && !rightOK {
		return nil, nil, fmt.Errorf("file %s not found in %s or %s", gitPath, leftCommit.Hash, rightCommit.Hash)
	}
	if leftOK {
		left, err = parser.Parse(leftSrc)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s at %s: %w", gitPath, leftCommit.Hash, err)
		}
	}
	if rightOK {
		right, err = parser.Parse(rightSrc)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s at %s: %w", gitPath, rightCommit.Hash, err)
		}
	}
	return left, right, nil
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

func openFile(file string) (*gogit.Repository, string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	repo, err := gogit.PlainOpenWithOptions(filepath.Dir(abs), &gogit.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("open repository for %s: %w", file, err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, "", fmt.Errorf("worktree for %s: %w", file, err)
	}
	root := worktree.Filesystem.Root()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("file %s is outside the repository", file)
	}
	return repo, filepath.ToSlash(rel), nil
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
