package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// version is the release a binary was built as, set by the release build:
//
//	go build -ldflags "-X main.version=v0.0.27" ./cmd/cli
//
// A build without it is a development build, named by its commit instead.
var version string

// commit names a development build that cannot see its repository: a Docker
// build copies the tree without .git. Every other build reads the commit the
// Go toolchain stamped into it.
var commit string

// releaseRepo is the GitHub repository a release build looks in for a newer
// release.
var releaseRepo = "naqerl/citydiff"

// buildInfo is what the legend's header shows: a release by its version, a
// development build by its commit and whether the tree had uncommitted changes.
type buildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
}

func (b buildInfo) isRelease() bool { return b.Version != "dev" }

func readBuild() buildInfo {
	if version != "" {
		return buildInfo{Version: version}
	}
	b := buildInfo{Version: "dev", Commit: commit}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if b.Commit == "" {
				b.Commit = s.Value
			}
		case "vcs.modified":
			b.Dirty = s.Value == "true"
		}
	}
	if len(b.Commit) > 7 {
		b.Commit = b.Commit[:7]
	}
	return b
}

// release is a published release newer than this build.
type release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// latestAPI is where GitHub answers with the newest release. It leaves out
// drafts and prereleases.
func latestAPI(repo string) string {
	return "https://api.github.com/repos/" + repo + "/releases/latest"
}

// checkUpdate asks api for the newest release and returns it when it is newer
// than current, nil when current is up to date.
func checkUpdate(ctx context.Context, client *http.Client, api, current string) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "citydiff/"+current)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", api, res.Status)
	}
	var latest struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&latest); err != nil {
		return nil, err
	}
	if !newerVersion(latest.Tag, current) {
		return nil, nil
	}
	return &release{Version: latest.Tag, URL: latest.URL}, nil
}

// watchUpdate asks api once, in the background of a viewer that already
// serves, for a release newer than this one. A newer release is a line on
// stderr and a link in the legend. A development build has no release to
// compare, and a check that fails says nothing: being offline is not worth a
// line.
func (v *viewer) watchUpdate(api string) {
	if !v.about.isRelease() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rel, err := checkUpdate(ctx, http.DefaultClient, api, v.about.Version)
	if err != nil || rel == nil {
		return
	}
	v.update.Store(rel)
	fmt.Fprintf(os.Stderr, "citydiff: %s is out (this is %s); re-run install.sh to update: %s\n", rel.Version, v.about.Version, rel.URL)
}

// versionReply is GET /api/version: this build, and the newer release the
// startup check found, if it found one yet.
type versionReply struct {
	buildInfo
	Update *release `json:"update,omitempty"`
}

func (v *viewer) serveVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionReply{buildInfo: v.about, Update: v.update.Load()})
}

// newerVersion reports whether the semver tag a comes after b. A tag that is
// not a version is never newer, so a strange tag on GitHub announces nothing.
func newerVersion(a, b string) bool {
	va, ok := parseVersion(a)
	if !ok {
		return false
	}
	vb, ok := parseVersion(b)
	if !ok {
		return false
	}
	for i := range va.num {
		if va.num[i] != vb.num[i] {
			return va.num[i] > vb.num[i]
		}
	}
	// v1.2.0 comes after v1.2.0-rc1; two prereleases compare as text.
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	}
	return va.pre > vb.pre
}

type semver struct {
	num [3]int
	pre string
}

// parseVersion reads vMAJOR.MINOR.PATCH, with an optional -prerelease and
// +build. The build metadata takes no part in the order.
func parseVersion(s string) (semver, bool) {
	var v semver
	s, ok := strings.CutPrefix(s, "v")
	if !ok {
		return v, false
	}
	s, _, _ = strings.Cut(s, "+")
	s, v.pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.num[i] = n
	}
	return v, true
}
