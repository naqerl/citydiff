package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// rangeRepo commits three versions of one Go package and returns the dir
// and the three hashes. Only the third version declares Third.
func rangeRepo(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1_700_000_000, 0).UTC()
	var hashes []string
	for i, src := range []string{
		"package a\n\nfunc First() {}\n",
		"package a\n\nfunc First() {}\n\nfunc Second() { First() }\n",
		"package a\n\nfunc First() {}\n\nfunc Second() { First() }\n\nfunc Third() { Second() }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("a.go"); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Minute)
		h, err := wt.Commit("v"+string(rune('0'+i)), &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: when}})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h.String())
	}
	return dir, hashes
}

func postTour(t *testing.T, srv *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(srv.URL+"/api/tour", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc map[string]any
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, doc
}

func sceneHas(t *testing.T, srv *httptest.Server, name string) bool {
	t.Helper()
	res, err := http.Get(srv.URL + "/scene.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return strings.Contains(string(raw), `"name":"`+name+`"`)
}

func TestLoadingATourSwitchesTheSceneToItsRange(t *testing.T) {
	dir, h := rangeRepo(t)
	snap, err := buildSnapshot(dir, h[0]+".."+h[1])
	if err != nil {
		t.Fatal(err)
	}
	v, err := newViewer(snap, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(v.handler())
	defer srv.Close()
	if sceneHas(t, srv, "Third") {
		t.Fatal("Third is in the first scene")
	}

	tourFor := func(r string) string {
		return `{"version":1,"range":"` + r + `","steps":[{"title":"t","focus":"a.Third"}]}`
	}
	code, doc := postTour(t, srv, tourFor(h[1][:8]+".."+h[2][:8]))
	scene, _ := doc["scene"].(map[string]any)
	if code != 200 || scene["switched"] != true {
		t.Fatalf("switch: %d %v", code, doc)
	}
	if !sceneHas(t, srv, "Third") {
		t.Fatal("scene.json still serves the old range")
	}
	res, err := http.Get(srv.URL + "/tour.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(raw), "a.Third") {
		t.Fatalf("tour.json = %s", raw)
	}

	code, doc = postTour(t, srv, tourFor(h[1]+".."+h[2]))
	scene, _ = doc["scene"].(map[string]any)
	if code != 200 || scene["switched"] != false {
		t.Fatalf("same range by full hash should not switch: %d %v", code, doc)
	}

	code, doc = postTour(t, srv, tourFor("nope..also-nope"))
	if code != http.StatusUnprocessableEntity || !strings.Contains(toJSON(doc), "cannot load the tour's range") {
		t.Fatalf("bad range: %d %v", code, doc)
	}
	if !sceneHas(t, srv, "Third") {
		t.Fatal("a bad range replaced the scene")
	}
}

func TestViewWithATourStartsOnTheToursRange(t *testing.T) {
	dir, h := rangeRepo(t)
	snap, err := buildSnapshot(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	v, err := newViewer(snap, []byte(`{"version":1,"range":"`+h[0]+`..`+h[1]+`","steps":[{"title":"t","focus":"Second"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.snap.commitRange != h[0]+".."+h[1] || !v.snap.scene.Diff {
		t.Fatalf("scene range %q diff=%v", v.snap.commitRange, v.snap.scene.Diff)
	}
	if _, err := newViewer(snap, []byte(`{"version":1,"range":"x..y","steps":[{"title":"t"}]}`)); err == nil || !strings.Contains(err.Error(), "cannot load the tour's range") {
		t.Fatalf("bad range at startup: %v", err)
	}
}

func toJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
