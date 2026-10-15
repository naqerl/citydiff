package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"citydiff/lib/git"
	"citydiff/lib/observe"
)

// hubKeyEnv is the one API key for every write on this hub. open sends it,
// and serve refuses to listen until it is set.
const hubKeyEnv = "CITYDIFF_HUB_KEY"

const observeUsage = `Usage:
  citydiff hub serve [-addr host:port]
  citydiff hub open [-range A..B] [-tour file.json] https://host/owner/repo

serve is the observation hub. Every write requires $CITYDIFF_HUB_KEY.
When that variable is empty, serve prints an export line and exits.

open talks to $CITYDIFF_OBSERVE_URL (default http://127.0.0.1:8787)
and sends $CITYDIFF_HUB_KEY. serve stores clones in $CITYDIFF_OBSERVE_DIR
(default ~/.cache/citydiff/observe).
`

func runObserve(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, observeUsage)
		return errUsage
	}
	switch args[0] {
	case "serve":
		return runObserveServe(args[1:], stderr)
	case "open":
		return runObserveOpen(args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, observeUsage)
		return errUsage
	}
}

func runObserveServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("citydiff hub serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	fs.Usage = func() { fmt.Fprint(stderr, observeUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, observeUsage)
		return errUsage
	}
	key, err := hubKey(stderr)
	if err != nil {
		return err
	}
	dir, err := observeDir()
	if err != nil {
		return err
	}
	store, err := observe.OpenStore(dir)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "citydiff: http://%s\n", ln.Addr())
	return http.Serve(ln, newHub(store, key).handler())
}

// hubKey is the API key every write checks. An empty environment prints one
// export line and refuses to serve, so the process never listens without a key.
func hubKey(stderr io.Writer) (string, error) {
	if key := strings.TrimSpace(os.Getenv(hubKeyEnv)); key != "" {
		return key, nil
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	fmt.Fprintf(stderr, "No API key is set. Paste this line, then start the hub again:\nexport %s=%s\n", hubKeyEnv, hex.EncodeToString(b[:]))
	return "", errUsage
}

func observeDir() (string, error) {
	if d := os.Getenv("CITYDIFF_OBSERVE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "citydiff", "observe"), nil
}

type openBody struct {
	Remote string          `json:"remote"`
	Range  string          `json:"range,omitempty"`
	Tour   json.RawMessage `json:"tour,omitempty"`
}

type openReply struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Remote string `json:"remote"`
}

func runObserveOpen(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("citydiff hub open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commitRange := fs.String("range", "", "commit range, A..B or A...B")
	fs.StringVar(commitRange, "r", "", "commit range, A..B or A...B")
	tourFile := fs.String("tour", "", "tour script to submit")
	fs.Usage = func() { fmt.Fprint(stderr, observeUsage) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, observeUsage)
		return errUsage
	}
	key := strings.TrimSpace(os.Getenv(hubKeyEnv))
	if key == "" {
		fmt.Fprintf(stderr, "hub open: set %s to the hub API key\n", hubKeyEnv)
		return errUsage
	}
	body := openBody{Remote: fs.Arg(0), Range: *commitRange}
	if *tourFile != "" {
		raw, err := os.ReadFile(*tourFile)
		if err != nil {
			return err
		}
		body.Tour = raw
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := os.Getenv("CITYDIFF_OBSERVE_URL")
	if base == "" {
		base = "http://127.0.0.1:8787"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/observations", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusCreated {
		fmt.Fprintf(stderr, "citydiff: %s\n", strings.TrimSpace(string(reply)))
		return fmt.Errorf("hub open: %s", res.Status)
	}
	var out openReply
	if err := json.Unmarshal(reply, &out); err != nil {
		return err
	}
	if out.URL == "" {
		return errors.New("hub open: server returned no URL")
	}
	fmt.Fprintln(stdout, out.URL)
	return nil
}

// hub is one observation server. public, ensure and refresh are replaceable
// in tests: the production ones refuse a remote that is not public https,
// then clone or fetch it.
type hub struct {
	store   *observe.Store
	scenes  *sceneCache
	key     string
	public  func(ctx context.Context, canonical string) error
	ensure  func(ctx context.Context, canonical string) (string, error)
	refresh func(ctx context.Context, canonical, repo string) error
	cards   cardCache
}

func newHub(store *observe.Store, key string) *hub {
	h := &hub{store: store, scenes: newSceneCache(), key: key}
	h.public = func(ctx context.Context, canonical string) error {
		return observe.Public(ctx, canonical, nil)
	}
	h.ensure = func(ctx context.Context, canonical string) (string, error) {
		dest := store.RepoDir(canonical)
		if err := observe.Clone(ctx, canonical, dest, observe.MaxCloneBytes); err != nil {
			return "", err
		}
		return dest, nil
	}
	h.refresh = func(ctx context.Context, canonical, repo string) error {
		return observe.Refresh(ctx, canonical, repo, observe.MaxCloneBytes)
	}
	return h
}

func (h *hub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/observations", h.create)
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, versionReply{buildInfo: readBuild()})
	})
	mountSkins(mux, skinsDir())
	mountPlugins(mux, pluginsDir())
	mux.HandleFunc("GET /o/{id}", h.redirect)
	mux.HandleFunc("/o/{id}/", h.observation)
	return mux
}

func (h *hub) redirect(w http.ResponseWriter, r *http.Request) {
	target := "/o/" + r.PathValue("id") + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

// sceneCache builds a scene at most once per clone and range, and keeps the
// overview plus eight ranges. The scene depends only on the clone, so
// observations that share a clone share the cache.
type sceneCache struct {
	inner *observe.Cache[*heldScene]
}

type heldScene struct {
	snap    *snapshot
	payload []byte
	packed  []byte
}

func newSceneCache() *sceneCache {
	return &sceneCache{inner: observe.NewCache[*heldScene](8, 2)}
}

func (c *sceneCache) invalidate(repo string) {
	c.inner.Invalidate(repo)
}

func (c *sceneCache) get(ctx context.Context, repo, commitRange string) (*heldScene, error) {
	key, overview, err := sceneKey(repo, commitRange)
	if err != nil {
		return nil, err
	}
	return c.inner.Get(ctx, repo, key, overview, func(context.Context) (*heldScene, error) {
		// buildSnapshot reads the clone's worktree, and Refresh resets that
		// worktree in place. Reading the clone's lock keeps a build from seeing
		// half of the old tree and half of the new one, while still letting two
		// builds run together.
		lock := observe.RepoLock(repo)
		lock.RLock()
		defer lock.RUnlock()
		snap, err := buildSnapshot(repo, commitRange)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(snap.scene)
		if err != nil {
			return nil, err
		}
		return &heldScene{snap: snap, payload: payload, packed: gzipped(payload)}, nil
	})
}

func sceneKey(repo, commitRange string) (string, bool, error) {
	if strings.TrimSpace(commitRange) == "" {
		hash, _, err := observe.Head(repo)
		if err != nil {
			return "", false, err
		}
		return observe.OverviewKey(hash), true, nil
	}
	spec, err := git.ParseSpec(commitRange)
	if err != nil {
		return "", false, err
	}
	left, right, err := git.Revs(repo, commitRange)
	if err != nil {
		return "", false, err
	}
	if spec.Snapshot {
		// A snapshot at a ref is a range scene with one side, so it is kept
		// in the range cache beside the diffs rather than replacing the
		// clone's own overview.
		return observe.SnapshotKey(right), false, nil
	}
	return observe.RangeKey(left, right), false, nil
}
