package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"betterdiff/lib"
	"betterdiff/lib/diff"
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

	changes := diff.Entries(left, right)
	if len(changes) != 1 || changes[0].Action != diff.Modified {
		t.Fatalf("changes: %v", changes)
	}
	if changes[0].Left.(lib.FunctionEntry).Name != "A" || changes[0].Right.(lib.FunctionEntry).Name != "A" {
		t.Fatalf("change = %s", changes[0])
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
	changes := diff.Entries(left, right)
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
	changes = diff.Entries(left, right)
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
	if got := diff.Entries(left, right); len(got) != 1 || got[0].Action != diff.Added {
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

func mustFunction(t *testing.T, entries []lib.Entity, name string) lib.FunctionEntry {
	t.Helper()
	for _, entry := range entries {
		fn, ok := entry.(lib.FunctionEntry)
		if ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return lib.FunctionEntry{}
}

func funcNames(entries []lib.Entity) []string {
	var names []string
	for _, entry := range entries {
		fn, ok := entry.(lib.FunctionEntry)
		if ok {
			names = append(names, fn.Name)
		}
	}
	return names
}
