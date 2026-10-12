package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v0.0.28", "v0.0.27", true},
		{"v0.0.27", "v0.0.27", false},
		{"v0.0.26", "v0.0.27", false},
		{"v0.1.0", "v0.0.99", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.0.10", "v0.0.9", true}, // numbers, not text
		{"v1.2.0", "v1.2.0-rc1", true},
		{"v1.2.0-rc1", "v1.2.0", false},
		{"v1.2.0-rc2", "v1.2.0-rc1", true},
		{"v1.2.0+build", "v1.2.0", false},
		{"nightly", "v0.0.27", false},
		{"v0.0.28", "dev", false},
		{"v1.2", "v1.1.0", false},
		{"", "v0.0.27", false},
	} {
		if got := newerVersion(tc.a, tc.b); got != tc.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestReadBuild(t *testing.T) {
	defer func(v, c string) { version, commit = v, c }(version, commit)

	version, commit = "v0.0.27", ""
	if b := readBuild(); b != (buildInfo{Version: "v0.0.27"}) || !b.isRelease() {
		t.Fatalf("release build: %+v", b)
	}
	// A commit the build was given wins over the toolchain's stamp, cut short.
	version, commit = "", "68537c2c0ffee"
	if b := readBuild(); b.Version != "dev" || b.Commit != "68537c2" || b.isRelease() {
		t.Fatalf("development build: %+v", b)
	}
}

// latestServer answers like GitHub's releases/latest with tag, and counts the
// requests it is sent.
func latestServer(t *testing.T, tag string) (*httptest.Server, *int) {
	t.Helper()
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.Header.Get("User-Agent") == "" {
			t.Error("the update check sent no User-Agent; GitHub refuses those")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": tag,
			"html_url": "https://github.com/naqerl/citydiff/releases/tag/" + tag,
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestCheckUpdate(t *testing.T) {
	srv, _ := latestServer(t, "v0.0.28")
	rel, err := checkUpdate(context.Background(), srv.Client(), srv.URL, "v0.0.27")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "v0.0.28" || rel.URL != "https://github.com/naqerl/citydiff/releases/tag/v0.0.28" {
		t.Fatalf("update = %+v", rel)
	}
	if rel, err := checkUpdate(context.Background(), srv.Client(), srv.URL, "v0.0.28"); err != nil || rel != nil {
		t.Fatalf("up to date: update = %+v, err = %v", rel, err)
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	if _, err := checkUpdate(context.Background(), gone.Client(), gone.URL, "v0.0.27"); err == nil {
		t.Fatal("a 404 is not an answer")
	}
}

func getVersion(t *testing.T, v *viewer) versionReply {
	t.Helper()
	srv := httptest.NewServer(v.handler())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var reply versionReply
	if err := json.NewDecoder(res.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestVersionEndpoint(t *testing.T) {
	srv, _ := latestServer(t, "v0.0.28")
	v := &viewer{about: buildInfo{Version: "v0.0.27"}}
	if reply := getVersion(t, v); reply.Version != "v0.0.27" || reply.Update != nil {
		t.Fatalf("before the check: %+v", reply)
	}
	v.watchUpdate(srv.URL)
	reply := getVersion(t, v)
	if reply.Update == nil || reply.Update.Version != "v0.0.28" {
		t.Fatalf("after the check: %+v", reply)
	}
}

func TestDevelopmentBuildDoesNotCheck(t *testing.T) {
	srv, asked := latestServer(t, "v0.0.28")
	v := &viewer{about: buildInfo{Version: "dev", Commit: "68537c2", Dirty: true}}
	v.watchUpdate(srv.URL)
	if *asked != 0 {
		t.Fatal("a development build asked for the latest release")
	}
	reply := getVersion(t, v)
	if reply.Version != "dev" || reply.Commit != "68537c2" || !reply.Dirty || reply.Update != nil {
		t.Fatalf("development build: %+v", reply)
	}
}
