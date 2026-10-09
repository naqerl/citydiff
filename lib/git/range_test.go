package git

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"betterdiff/lib"
	"betterdiff/lib/diff"
	"betterdiff/lib/files"
	"betterdiff/lib/parser/go"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSplitRange(t *testing.T) {
	cases := []struct {
		spec    string
		left    string
		right   string
		three   bool
		wantErr bool
	}{
		{spec: "a..b", left: "a", right: "b"},
		{spec: "a...b", left: "a", right: "b", three: true},
		{spec: "..b", right: "b"},
		{spec: "a..", left: "a"},
		{spec: "...b", right: "b", three: true},
		{spec: "a...", left: "a", three: true},
		{spec: "  a..b  ", left: "a", right: "b"},
		{spec: "HEAD^{/fix bug}..other", left: "HEAD^{/fix bug}", right: "other"},
		{spec: "", wantErr: true},
		{spec: "HEAD", wantErr: true},
		{spec: "a..b..c", wantErr: true},
		{spec: "a...b...c", wantErr: true},
		{spec: "a..b...c", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := splitRange(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.left != tc.left || got.right != tc.right || got.threeDot != tc.three {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestVersionsBodyChange(t *testing.T) {
	repo := newRepo(t)
	file := repo.commit("p.go", "package p\n\nfunc A() int { return 1 }\n", "add A")
	repo.commit("p.go", "package p\n\nfunc A() int { return 2 }\n", "edit A")

	left, right, err := Versions(file, repo.hash(0)+".."+repo.hash(1), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	leftFn := mustFunction(t, left, "A")
	rightFn := mustFunction(t, right, "A")
	if leftFn.BodyHash == "" || leftFn.BodyHash == rightFn.BodyHash {
		t.Fatalf("body hashes left %s right %s", leftFn.BodyHash, rightFn.BodyHash)
	}

	changes := diff.Entries(flat(left), flat(right))
	if len(changes) != 1 || changes[0].Action != diff.Modified {
		t.Fatalf("changes: %v", changes)
	}
	if changes[0].Left.(lib.FunctionEntry).Name != "A" || changes[0].Right.(lib.FunctionEntry).Name != "A" {
		t.Fatalf("change = %s", changes[0])
	}
	if len(changes[0].Edits) != 1 || changes[0].Edits[0].Field != "bodyHash" {
		t.Fatalf("edits = %+v", changes[0].Edits)
	}
	if changes[0].Edits[0].Left != leftFn.BodyHash || changes[0].Edits[0].Right != rightFn.BodyHash {
		t.Fatalf("edit = %+v", changes[0].Edits[0])
	}

	// An empty side of the range is HEAD. HEAD is the second commit.
	omittedLeft, omittedRight, err := Versions(file, repo.hash(0)+"..", golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if mustFunction(t, omittedLeft, "A").BodyHash != leftFn.BodyHash || mustFunction(t, omittedRight, "A").BodyHash != rightFn.BodyHash {
		t.Fatal("empty right side did not resolve to HEAD")
	}
}

func TestVersionsThreeDot(t *testing.T) {
	repo := newRepo(t)
	base := "package p\n\nfunc A() {}\n"
	repo.commit("service/sample.go", base, "base")
	repo.checkout("side", true)
	repo.commit("service/sample.go", base+"\nfunc B() {}\n", "add B")
	repo.checkout("master", false)
	file := repo.commit("service/sample.go", base+"\nfunc C() {}\n", "add C")

	left, right, err := Versions(file, "side...master", golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if names := funcNames(left); len(names) != 1 || names[0] != "A" {
		t.Fatalf("three-dot left = %v", names)
	}
	if names := funcNames(right); len(names) != 2 || names[0] != "A" || names[1] != "C" {
		t.Fatalf("three-dot right = %v", names)
	}
	changes := diff.Entries(flat(left), flat(right))
	if len(changes) != 1 || changes[0].String() != "added function C" {
		t.Fatalf("three-dot diff = %v", changes)
	}

	left, right, err = Versions(file, "side..master", golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if names := funcNames(left); len(names) != 2 || names[1] != "B" {
		t.Fatalf("two-dot left = %v", names)
	}
	changes = diff.Entries(flat(left), flat(right))
	if len(changes) != 2 || changes[0].String() != "added function C" || changes[1].String() != "removed function B" {
		t.Fatalf("two-dot diff = %v", changes)
	}
}

func TestVersionsFileAddedAndRemoved(t *testing.T) {
	repo := newRepo(t)
	repo.commit("readme.txt", "hi\n", "readme")
	added := repo.commit("p.go", "package p\n\nfunc A() {}\n", "add p.go")

	left, right, err := Versions(added, repo.hash(0)+".."+repo.hash(1), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 || len(funcNames(right)) != 1 {
		t.Fatalf("added file left %d right %v", len(left), funcNames(right))
	}
	if got := diff.Entries(flat(left), flat(right)); len(got) != 1 || got[0].Action != diff.Added {
		t.Fatalf("added diff = %v", got)
	}

	repo.remove("p.go", "remove p.go")
	left, right, err = Versions(added, repo.hash(1)+".."+repo.hash(2), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(funcNames(left)) != 1 || len(right) != 0 {
		t.Fatalf("removed file left %v right %d", funcNames(left), len(right))
	}

	_, _, err = Versions(filepath.Join(repo.dir, "missing.go"), repo.hash(0)+".."+repo.hash(1), golang.New())
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestTreeAndDirectoryDiff(t *testing.T) {
	repo := newRepo(t)
	repo.commit("a.go", "package p\n\nfunc A() { B() }\n", "add A")
	repo.commit("b.go", "package p\n\nfunc B() {}\n", "add B")
	repo.commit("readme.txt", "hi\n", "readme")
	repo.commit("b.go", "package p\n\nfunc B() { A() }\n", "edit B")

	src, err := Tree(repo.dir, repo.hash(3))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, file.Path)
	}
	if strings.Join(paths, ",") != "a.go,b.go" {
		t.Fatalf("tree = %v", paths)
	}

	left, right, err := Versions(repo.dir, repo.hash(2)+".."+repo.hash(3), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	changes := diff.Files(left, right)
	if len(changes) != 1 || changes[0].String() != "modified b.go" {
		t.Fatalf("directory diff = %v", changes)
	}
	var sawCalls, sawBody, sawBytes bool
	for _, edit := range changes[0].Changes[0].Edits {
		switch edit.Field {
		case "bodyHash":
			sawBody = true
		case "bodyBytes":
			sawBytes = true
			leftN, lok := edit.Left.(int)
			rightN, rok := edit.Right.(int)
			if !lok || !rok || leftN >= rightN {
				t.Fatalf("bodyBytes = %+v", edit)
			}
		case "calls":
			sawCalls = true
			rightCalls, ok := edit.Right.([]lib.Call)
			if !ok || len(rightCalls) != 1 || rightCalls[0].Expr != "A" || rightCalls[0].Ref == nil || rightCalls[0].Ref.Path != "a.go" {
				t.Fatalf("calls = %+v", edit.Right)
			}
		default:
			t.Fatalf("unexpected edit %s", edit.Field)
		}
	}
	if !sawCalls || !sawBody || !sawBytes {
		t.Fatalf("edits = %+v", changes[0].Changes[0].Edits)
	}
}

func TestTreeSkipsVendorAndHidden(t *testing.T) {
	repo := newRepo(t)
	repo.commit("p.go", "package p\n\nfunc P() {}\n", "p")
	repo.commit("vendor/v.go", "package v\n\nfunc V() {}\n", "v")
	repo.commit(".hidden/h.go", "package h\n\nfunc H() {}\n", "h")
	rev := repo.hash(2)

	src, err := Tree(repo.dir, rev)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(t, src); len(got) != 1 || got[0] != "p.go" {
		t.Fatalf("tree = %v", got)
	}
	left, right, err := Versions(repo.dir, rev+".."+rev, golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if got := parsedPaths(left); len(got) != 1 || got[0] != "p.go" || strings.Join(parsedPaths(right), ",") != "p.go" {
		t.Fatalf("versions left %v right %v", parsedPaths(left), parsedPaths(right))
	}

	vendor := filepath.Join(repo.dir, "vendor", "v.go")
	hidden := filepath.Join(repo.dir, ".hidden", "h.go")
	for _, exact := range []string{vendor, hidden} {
		src, err = Tree(exact, rev)
		if err != nil {
			t.Fatal(err)
		}
		if got := sourcePaths(t, src); len(got) != 0 {
			t.Fatalf("tree %s = %v", exact, got)
		}
		if _, _, err = Versions(exact, rev+".."+rev, golang.New()); err == nil {
			t.Fatalf("versions %s returned a file", exact)
		}
	}
}

func TestSnapshotPathsFollowRequestedRoot(t *testing.T) {
	repo := newRepo(t)
	repo.commit("pkg/a.go", "package pkg\n\nfunc A() { B() }\n", "a")
	repo.commit("pkg/b.go", "package pkg\n\nfunc B() {}\n", "b")
	rev := repo.hash(1)
	pkg := filepath.Join(repo.dir, "pkg")
	file := filepath.Join(pkg, "a.go")
	span := rev + ".." + rev

	disk, err := files.Tree(pkg)
	if err != nil {
		t.Fatal(err)
	}
	gitSrc, err := Tree(pkg, rev)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sourcePaths(t, disk), []string{"a.go", "b.go"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files.Tree = %v", got)
	}
	if got := sourcePaths(t, gitSrc); strings.Join(got, ",") != "a.go,b.go" {
		t.Fatalf("git.Tree = %v", got)
	}
	left, right, err := Versions(pkg, span, golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsedPaths(left), ",") != "a.go,b.go" || strings.Join(parsedPaths(right), ",") != "a.go,b.go" {
		t.Fatalf("versions left %v right %v", parsedPaths(left), parsedPaths(right))
	}
	fn := mustFunction(t, right, "A")
	if len(fn.Calls) != 1 || fn.Calls[0].Ref == nil || fn.Calls[0].Ref.Path != "b.go" {
		t.Fatalf("A calls = %+v", fn.Calls)
	}

	one, err := files.Tree(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(t, one); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("files.Tree file = %v", got)
	}
	oneGit, err := Tree(file, rev)
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(t, oneGit); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("git.Tree file = %v", got)
	}
	left, right, err = Versions(file, span, golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsedPaths(left), ",") != "a.go" || strings.Join(parsedPaths(right), ",") != "a.go" {
		t.Fatalf("file versions left %v right %v", parsedPaths(left), parsedPaths(right))
	}
}

func TestVersionsDeletedWorktreeDirectory(t *testing.T) {
	repo := newRepo(t)
	repo.commit("dir/a.go", "package dir\n\nfunc A() {}\n", "add")
	repo.commit("dir/a.go", "package dir\n\nfunc A() {}\nfunc B() {}\n", "add B")
	dir := filepath.Join(repo.dir, "dir")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	left, right, err := Versions(dir, repo.hash(0)+".."+repo.hash(1), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsedPaths(left), ",") != "a.go" || strings.Join(parsedPaths(right), ",") != "a.go" {
		t.Fatalf("paths left %v right %v", parsedPaths(left), parsedPaths(right))
	}
	if names := funcNames(left); len(names) != 1 || names[0] != "A" {
		t.Fatalf("left = %v", names)
	}
	if names := funcNames(right); len(names) != 2 || names[0] != "A" || names[1] != "B" {
		t.Fatalf("right = %v", names)
	}
}

func TestVersionsWorktreeFileWhenCommitHasDirectory(t *testing.T) {
	repo := newRepo(t)
	repo.commit("dir/a.go", "package dir\n\nfunc A() {}\n", "add")
	repo.commit("dir/a.go", "package dir\n\nfunc A() {}\nfunc B() {}\n", "add B")
	dir := filepath.Join(repo.dir, "dir")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	left, right, err := Versions(dir, repo.hash(0)+".."+repo.hash(1), golang.New())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(parsedPaths(right), ",") != "a.go" || len(funcNames(right)) != 2 {
		t.Fatalf("right paths %v names %v", parsedPaths(right), funcNames(right))
	}
	if len(funcNames(left)) != 1 {
		t.Fatalf("left = %v", funcNames(left))
	}
}

func TestVersionsRejectsBadInput(t *testing.T) {
	repo := newRepo(t)
	file := repo.commit("p.go", "package p\n\nfunc A() {}\n", "add")
	if _, _, err := Versions(file, "HEAD", golang.New()); err == nil {
		t.Fatal("expected range syntax error")
	}
	if _, _, err := Versions(file, "does-not-exist..HEAD", golang.New()); err == nil {
		t.Fatal("expected resolve error")
	}
	if _, _, err := Versions(file, "HEAD..HEAD", nil); err == nil {
		t.Fatal("expected nil parser error")
	}
	if _, _, err := Versions("  ", "HEAD..HEAD", golang.New()); err == nil {
		t.Fatal("expected empty path error")
	}

	outside := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(outside, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Versions(outside, "HEAD..HEAD", golang.New()); err == nil {
		t.Fatal("expected missing repository error")
	}
}

type repoFix struct {
	t      *testing.T
	dir    string
	when   time.Time
	hashes []plumbing.Hash
}

func newRepo(t *testing.T) *repoFix {
	t.Helper()
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	return &repoFix{t: t, dir: dir, when: time.Unix(1_700_000_000, 0).UTC()}
}

func (r *repoFix) commit(path, contents, message string) string {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		r.t.Fatal(err)
	}
	wt := r.worktree()
	if _, err := wt.Add(path); err != nil {
		r.t.Fatal(err)
	}
	r.hashes = append(r.hashes, r.commitWorktree(wt, message))
	return full
}

func (r *repoFix) remove(path, message string) {
	r.t.Helper()
	wt := r.worktree()
	if _, err := wt.Remove(path); err != nil {
		r.t.Fatal(err)
	}
	r.hashes = append(r.hashes, r.commitWorktree(wt, message))
}

func (r *repoFix) checkout(branch string, create bool) {
	r.t.Helper()
	wt := r.worktree()
	err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: create,
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *repoFix) hash(i int) string {
	r.t.Helper()
	return r.hashes[i].String()
}

func (r *repoFix) worktree() *gogit.Worktree {
	r.t.Helper()
	repo, err := gogit.PlainOpen(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		r.t.Fatal(err)
	}
	return wt
}

func (r *repoFix) commitWorktree(wt *gogit.Worktree, message string) plumbing.Hash {
	r.t.Helper()
	r.when = r.when.Add(time.Minute)
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Betterdiff",
			Email: "betterdiff@example.com",
			When:  r.when,
		},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return hash
}

func sourcePaths(t *testing.T, src lib.Source) []string {
	t.Helper()
	var paths []string
	for {
		file, err := src.Next()
		if errors.Is(err, io.EOF) {
			return paths
		}
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, file.Path)
	}
}

func parsedPaths(files []lib.ParsedFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func flat(files []lib.ParsedFile) []lib.Entity {
	var out []lib.Entity
	for _, file := range files {
		out = append(out, file.Entities...)
	}
	return out
}

func mustFunction(t *testing.T, files []lib.ParsedFile, name string) lib.FunctionEntry {
	t.Helper()
	for _, entry := range flat(files) {
		fn, ok := entry.(lib.FunctionEntry)
		if ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return lib.FunctionEntry{}
}

func funcNames(files []lib.ParsedFile) []string {
	var names []string
	for _, entry := range flat(files) {
		fn, ok := entry.(lib.FunctionEntry)
		if ok {
			names = append(names, fn.Name)
		}
	}
	return names
}
