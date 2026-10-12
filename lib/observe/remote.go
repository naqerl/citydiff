// Package observe clones a public repository and keeps the observations
// that serve it. An observation is one submission: its own id, its own
// tours, and a clone it may share with other submissions of the same remote.
package observe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// MaxCloneBytes is the largest clone an observation will keep.
const MaxCloneBytes int64 = 1 << 30

// cloneTimeout bounds one clone. The dialer checks the address again, so a
// name that changes between the lookup and the connection still has to be public.
const cloneTimeout = 2 * time.Minute

// Canonical is the remote observations share a clone by. The host is
// lowercased, a trailing .git is removed, and a query, a fragment and
// credentials are rejected. The path's own case stays, so two spellings of
// one repository that differ only by case stay two clones.
func Canonical(raw string) (string, error) {
	u, err := parseRemote(raw)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".git") {
		path = path[:len(path)-len(".git")]
		path = strings.TrimSuffix(path, "/")
	}
	if path == "" || path == "/" || strings.Contains(path, "..") {
		return "", errors.New("remote must name a repository")
	}
	return "https://" + hostport(u) + path, nil
}

// Lookup resolves a host. Tests pass their own; a nil lookup uses the
// process resolver.
type Lookup func(ctx context.Context, host string) ([]net.IP, error)

// Public reports whether canonical is an https name whose every address is a
// public unicast address. A literal IP is rejected even when it is public.
func Public(ctx context.Context, canonical string, lookup Lookup) error {
	u, err := url.Parse(canonical)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return errors.New("remote must be https")
	}
	if u.User != nil {
		return errors.New("remote must not carry credentials")
	}
	return publicHost(ctx, u.Hostname(), lookup)
}

func parseRemote(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("remote must be an https URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("remote must be https")
	}
	if u.User != nil {
		return nil, errors.New("remote must not carry credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("remote must not carry a query or a fragment")
	}
	if net.ParseIP(u.Hostname()) != nil {
		return nil, errors.New("remote host must be a name")
	}
	if u.Hostname() == "" {
		return nil, errors.New("remote must be an https URL")
	}
	return u, nil
}

func hostport(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		return net.JoinHostPort(host, p)
	}
	return host
}

func publicHost(ctx context.Context, host string, lookup Lookup) error {
	if net.ParseIP(host) != nil {
		return errors.New("remote host must be a name")
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve %s: no address", host)
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return fmt.Errorf("%s is not a public address", host)
		}
	}
	return nil
}

func defaultLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		out[i] = addr.IP
	}
	return out, nil
}

// publicIP is a public unicast address. Private, loopback, link-local
// (including the cloud metadata address), CGNAT and documentation ranges are not.
func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return !cgnat.Contains(v4) && !bench.Contains(v4) && !doc1.Contains(v4) && !doc2.Contains(v4) && !doc3.Contains(v4) && !ietf.Contains(v4)
	}
	return !ipv6doc.Contains(ip)
}

var (
	cgnat   = mustCIDR("100.64.0.0/10")
	bench   = mustCIDR("198.18.0.0/15")
	doc1    = mustCIDR("192.0.2.0/24")
	doc2    = mustCIDR("198.51.100.0/24")
	doc3    = mustCIDR("203.0.113.0/24")
	ietf    = mustCIDR("192.0.0.0/24")
	ipv6doc = mustCIDR("2001:db8::/32")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// RepoKey is the directory name of the clone shared by one canonical remote.
func RepoKey(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// Clone clones canonical into dest when dest is not already a repository.
// The remote must already have passed Public. The dialer checks the address
// again, and a clone larger than limit is deleted.
func Clone(ctx context.Context, canonical, dest string, limit int64) error {
	if _, err := Canonical(canonical); err != nil {
		return err
	}
	if limit <= 0 {
		limit = MaxCloneBytes
	}
	installClient()
	if repo, err := gogit.PlainOpen(dest); err == nil {
		if _, err := repo.Head(); err == nil {
			return nil
		}
	}
	key := RepoKey(canonical)
	lock := cloneLock(key)
	lock.Lock()
	defer lock.Unlock()
	if repo, err := gogit.PlainOpen(dest); err == nil {
		if _, err := repo.Head(); err == nil {
			return nil
		}
	}
	part := dest + ".part"
	_ = os.RemoveAll(part)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	_, err := gogit.PlainCloneContext(ctx, part, false, &gogit.CloneOptions{
		URL:  canonical,
		Tags: gogit.AllTags,
	})
	if err != nil {
		_ = os.RemoveAll(part)
		if errors.Is(err, transport.ErrAuthenticationRequired) {
			return errors.New("only a public remote")
		}
		return err
	}
	if err := withinLimit(part, limit); err != nil {
		return err
	}
	return os.Rename(part, dest)
}

// Refresh fetches a clone that already exists, so an observation of a moving
// branch sees the commits that arrived since the clone. It advances the local
// default branch and its working tree to the remote's, because the plain
// overview is read from the working tree. The clone is checked against limit
// without deleting it: a refresh that would push the clone past the limit
// leaves the clone in place and reports the error.
//
// The remote must already have passed Public. Refresh takes the same lock as
// Clone for the clone's key, so two refreshes of one remote cannot overlap.
func Refresh(ctx context.Context, canonical, dest string, limit int64) error {
	if _, err := Canonical(canonical); err != nil {
		return err
	}
	if limit <= 0 {
		limit = MaxCloneBytes
	}
	installClient()
	repo, err := gogit.PlainOpen(dest)
	if err != nil {
		return err
	}
	if _, err := repo.Head(); err != nil {
		return err
	}
	lock := cloneLock(RepoKey(canonical))
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	err = repo.FetchContext(ctx, &gogit.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
		Prune:      true,
		Tags:       gogit.AllTags,
		Force:      true,
	})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		if errors.Is(err, transport.ErrAuthenticationRequired) {
			return errors.New("only a public remote")
		}
		return err
	}
	if err := followDefaultBranch(repo); err != nil {
		return err
	}
	n, err := dirBytes(dest)
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("repository is larger than %d bytes", limit)
	}
	return nil
}

// followDefaultBranch moves the checked-out branch to its origin counterpart
// when a fetch advanced it. Cloning checks out the default branch only, so the
// worktree behind the overview has to move with the ref.
func followDefaultBranch(repo *gogit.Repository) error {
	head, err := repo.Head()
	if err != nil {
		return err
	}
	if !head.Name().IsBranch() {
		return nil
	}
	remote, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true)
	if err != nil || remote.Hash() == head.Hash() {
		// The remote no longer has the branch, or the branch did not move.
		return nil
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&gogit.ResetOptions{Commit: remote.Hash(), Mode: gogit.HardReset})
}

// withinLimit deletes root when its files add up to more than limit.
func withinLimit(root string, limit int64) error {
	n, err := dirBytes(root)
	if err != nil {
		_ = os.RemoveAll(root)
		return err
	}
	if n > limit {
		_ = os.RemoveAll(root)
		return fmt.Errorf("repository is larger than %d bytes", limit)
	}
	return nil
}

func dirBytes(root string) (int64, error) {
	var n int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n, err
}

var (
	cloneMu    sync.Mutex
	cloneLocks = map[string]*sync.Mutex{}
	clientOnce sync.Once
)

func cloneLock(key string) *sync.Mutex {
	cloneMu.Lock()
	defer cloneMu.Unlock()
	m, ok := cloneLocks[key]
	if !ok {
		m = &sync.Mutex{}
		cloneLocks[key] = m
	}
	return m
}

func installClient() {
	clientOnce.Do(func() {
		c := &http.Client{
			Timeout:   cloneTimeout,
			Transport: publicTransport(),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				// Git's smart HTTP puts the service in the query. That is not
				// the remote the user submitted, so the query is allowed here.
				if req.URL.User != nil || !strings.EqualFold(req.URL.Scheme, "https") {
					return errors.New("remote must be https")
				}
				return publicHost(req.Context(), req.URL.Hostname(), nil)
			},
		}
		client.InstallProtocol("https", githttp.NewClient(c))
	})
}

func publicTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	var t *http.Transport
	if ok {
		t = base.Clone()
	} else {
		t = &http.Transport{}
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) != nil {
			return nil, errors.New("remote host must be a name")
		}
		ips, err := defaultLookup(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, fmt.Errorf("%s is not a public address", host)
			}
		}
		var d net.Dialer
		var last error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("no address for %s", host)
		}
		return nil, last
	}
	return t
}
