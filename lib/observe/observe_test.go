package observe

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanonical(t *testing.T) {
	got, err := Canonical("https://GitHub.com/Org/Repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/Org/Repo" {
		t.Fatalf("canonical = %q", got)
	}
	again, err := Canonical(got)
	if err != nil || again != got {
		t.Fatalf("canonical is not stable: %q %v", again, err)
	}
	slash, err := Canonical("https://github.com/Org/Repo/")
	if err != nil || slash != got || RepoKey(slash) != RepoKey(got) {
		t.Fatalf("trailing slash canonical = %q %v", slash, err)
	}
	for _, raw := range []string{
		"file:///tmp/repo",
		"http://github.com/org/repo",
		"ssh://git@github.com/org/repo",
		"git@github.com:org/repo",
		"https://user:pass@github.com/org/repo",
		"https://github.com/org/repo?token=1",
		"https://127.0.0.1/org/repo",
		"https://github.com",
	} {
		if _, err := Canonical(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestPublic(t *testing.T) {
	ctx := context.Background()
	lookup := func(ip string) Lookup {
		return func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP(ip)}, nil
		}
	}
	if err := Public(ctx, "https://example.com/org/repo", lookup("8.8.8.8")); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{
		"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.0.1",
		"169.254.169.254", "100.64.1.1", "::1", "fe80::1", "fc00::1",
	} {
		err := Public(ctx, "https://example.com/org/repo", lookup(ip))
		if err == nil {
			t.Errorf("accepted %s", ip)
		}
	}
	mixed := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1")}, nil
	}
	if err := Public(ctx, "https://example.com/org/repo", mixed); err == nil {
		t.Fatal("accepted a name with a private address")
	}
}

func TestToursStayApart(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Create("https://github.com/org/repo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("https://github.com/org/repo", a.Repo)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("two opens shared an id")
	}
	raw := []byte(`{"version":1,"title":"Third arrives","range":"a..b","steps":[{"title":"The change"}]}`)
	name, err := s.UniqueName(a.ID, "Third arrives")
	if err != nil || name != "third-arrives" {
		t.Fatalf("name %q %v", name, err)
	}
	if err := s.PutTour(a.ID, name, raw); err != nil {
		t.Fatal(err)
	}
	again, err := s.UniqueName(a.ID, "Third arrives")
	if err != nil || again != "third-arrives-2" {
		t.Fatalf("second name %q %v", again, err)
	}
	list, err := s.Tours(b.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("other observation sees %v %v", list, err)
	}
	got, err := s.Tour(a.ID, name)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("tour = %s %v", got, err)
	}
}

func TestWithinLimitDeletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := withinLimit(dir, 1); err == nil {
		t.Fatal("kept a directory over the limit")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still there: %v", err)
	}
}

func TestCacheOneBuildAndOverviewStays(t *testing.T) {
	var n atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c := NewCache[int](2, 2)
	build := func(context.Context) (int, error) {
		n.Add(1)
		once.Do(func() { close(started) })
		<-release
		return 1, nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	var first, second int
	go func() {
		defer wg.Done()
		v, err := c.Get(context.Background(), "repo", "left a right b", false, build)
		if err != nil {
			t.Error(err)
		}
		first = v
	}()
	<-started
	go func() {
		defer wg.Done()
		v, err := c.Get(context.Background(), "repo", "left a right b", false, build)
		if err != nil {
			t.Error(err)
		}
		second = v
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n.Load() != 1 || first != 1 || second != 1 {
		t.Fatalf("builds %d values %d %d", n.Load(), first, second)
	}
	ctx := context.Background()
	if _, err := c.Get(ctx, "repo", OverviewKey("h"), true, func(context.Context) (int, error) { return 7, nil }); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"left a right c", "left a right d", "left a right e"} {
		if _, err := c.Get(ctx, "repo", key, false, func(context.Context) (int, error) { return 1, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if !c.Has("repo", OverviewKey("h"), true) {
		t.Fatal("overview was evicted")
	}
	if c.Has("repo", "left a right b", false) {
		t.Fatal("oldest range was kept past the cap")
	}
}

func TestSlug(t *testing.T) {
	if Slug("Third arrives!") != "third-arrives" {
		t.Fatal(Slug("Third arrives!"))
	}
	if Slug("...") != "tour" {
		t.Fatal(Slug("..."))
	}
}
