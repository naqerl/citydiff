package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"citydiff/lib/git"
	"citydiff/lib/observe"
	"citydiff/lib/scene"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const testHubKey = "test-hub-key"

func testHub(t *testing.T, repo string) *httptest.Server {
	t.Helper()
	store, err := observe.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHub(store, testHubKey)
	h.public = func(context.Context, string) error { return nil }
	h.ensure = func(context.Context, string) (string, error) { return repo, nil }
	srv := httptest.NewServer(h.handler())
	t.Cleanup(srv.Close)
	return srv
}

func postObs(t *testing.T, srv *httptest.Server, body string) (int, openReply, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/observations", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testHubKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := readAll(res)
	if err != nil {
		t.Fatal(err)
	}
	var out openReply
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out, raw
}

func readAll(res *http.Response) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(res.Body)
	return buf.Bytes(), err
}

func TestObserveOpenServesOverviewAndRange(t *testing.T) {
	dir, hashes := rangeRepo(t)
	srv := testHub(t, dir)
	status, opened, raw := postObs(t, srv, `{"remote":"https://GitHub.com/Example/Repo.git"}`)
	if status != http.StatusCreated {
		t.Fatalf("open %d %s", status, raw)
	}
	if opened.Remote != "https://github.com/Example/Repo" {
		t.Fatalf("remote %q", opened.Remote)
	}
	if !strings.HasSuffix(strings.TrimRight(opened.URL, "?"), "/") || !strings.Contains(opened.URL, "/o/"+opened.ID+"/") {
		t.Fatalf("url %q", opened.URL)
	}
	res, err := http.Get(opened.URL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := readAll(res)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("citydiff")) {
		t.Fatalf("page %d", res.StatusCode)
	}
	overview := getScene(t, opened.URL)
	if overview.Diff || overview.Range != "" {
		t.Fatalf("overview diff=%v range=%q", overview.Diff, overview.Range)
	}
	ranged := opened.URL + "?range=" + hashes[0] + ".." + hashes[2]
	diffScene := getScene(t, ranged)
	if !diffScene.Diff || diffScene.Range == "" {
		t.Fatalf("range scene diff=%v range=%q", diffScene.Diff, diffScene.Range)
	}

	status, second, raw := postObs(t, srv, `{"remote":"https://github.com/Example/Repo"}`)
	if status != http.StatusCreated || second.ID == opened.ID {
		t.Fatalf("second open %d %s id %s", status, raw, second.ID)
	}
	tour := `{"version":1,"title":"Third arrives","range":"` + hashes[0] + `..` + hashes[2] + `","steps":[{"title":"The change"}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/"+opened.ID+"/api/tours", strings.NewReader(tour))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testHubKey)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := readAll(res)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || !bytes.Contains(saved, []byte(`"name":"third-arrives"`)) {
		t.Fatalf("save tour %d %s", res.StatusCode, saved)
	}
	bare, err := http.Post(srv.URL+"/o/"+opened.ID+"/api/tours", "application/json", strings.NewReader(tour))
	if err != nil {
		t.Fatal(err)
	}
	bare.Body.Close()
	if bare.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tour without key: %d", bare.StatusCode)
	}
	wrong, err := http.NewRequest(http.MethodPost, srv.URL+"/api/observations", strings.NewReader(`{"remote":"https://github.com/Example/Repo"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Header.Set("Content-Type", "application/json")
	wrongRes, err := http.DefaultClient.Do(wrong)
	if err != nil {
		t.Fatal(err)
	}
	wrongRes.Body.Close()
	if wrongRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("open without key: %d", wrongRes.StatusCode)
	}
	others, err := http.Get(srv.URL + "/o/" + second.ID + "/api/tours")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := readAll(others)
	others.Body.Close()
	if strings.TrimSpace(string(list)) != "[]" {
		t.Fatalf("second observation tours %s", list)
	}
	same, err := http.NewRequest(http.MethodPost, srv.URL+"/o/"+second.ID+"/api/tours", strings.NewReader(tour))
	if err != nil {
		t.Fatal(err)
	}
	same.Header.Set("Authorization", "Bearer "+testHubKey)
	sameRes, err := http.DefaultClient.Do(same)
	if err != nil {
		t.Fatal(err)
	}
	sameBody, _ := readAll(sameRes)
	sameRes.Body.Close()
	if sameRes.StatusCode != http.StatusCreated {
		t.Fatalf("same key on another observation: %d %s", sameRes.StatusCode, sameBody)
	}
	played := getScene(t, opened.URL+"?tour=third-arrives")
	if !played.Diff {
		t.Fatal("tour did not select its range")
	}
	refsRes, err := http.Get(srv.URL + "/o/" + opened.ID + "/api/refs")
	if err != nil {
		t.Fatal(err)
	}
	defer refsRes.Body.Close()
	var refs observe.Refs
	if err := json.NewDecoder(refsRes.Body).Decode(&refs); err != nil {
		t.Fatal(err)
	}
	if refs.Head == "" || len(refs.Commits) < 3 || len(refs.Branches) == 0 {
		t.Fatalf("refs %+v", refs)
	}
	diffRes, err := http.Get(srv.URL + "/o/" + opened.ID + "/api/diff?file=a.go&range=" + hashes[0] + ".." + hashes[2])
	if err != nil {
		t.Fatal(err)
	}
	defer diffRes.Body.Close()
	var diffBody struct {
		Range string `json:"range"`
		Files []struct {
			Hunks []any `json:"hunks"`
		} `json:"files"`
	}
	if err := json.NewDecoder(diffRes.Body).Decode(&diffBody); err != nil {
		t.Fatal(err)
	}
	if diffBody.Range == "" || len(diffBody.Files) == 0 || len(diffBody.Files[0].Hunks) == 0 {
		t.Fatalf("diff %+v", diffBody)
	}
}

func TestObserveOpenRejectsDisagreeingRange(t *testing.T) {
	dir, hashes := rangeRepo(t)
	srv := testHub(t, dir)
	t.Setenv("CITYDIFF_OBSERVE_URL", srv.URL)
	t.Setenv(hubKeyEnv, testHubKey)
	path := filepath.Join(t.TempDir(), "tour.json")
	script := `{"version":1,"title":"Third arrives","range":"` + hashes[0] + `..` + hashes[2] + `","steps":[{"title":"The change"}]}`
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := runObserveOpen([]string{"-range", hashes[0] + ".." + hashes[1], "-tour", path, "https://github.com/Example/Repo"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("disagreeing range was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "tour targets") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestObserveOpenPrintsURL(t *testing.T) {
	dir, hashes := rangeRepo(t)
	srv := testHub(t, dir)
	t.Setenv("CITYDIFF_OBSERVE_URL", srv.URL)
	t.Setenv(hubKeyEnv, testHubKey)
	var stdout, stderr bytes.Buffer
	err := runObserveOpen([]string{"-range", hashes[0] + ".." + hashes[2], "https://github.com/Example/Repo.git"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if strings.Contains(got, "token") || strings.Contains(got, hubKeyEnv) || !strings.Contains(got, "?range=") {
		t.Fatalf("stdout %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr %q", stderr.String())
	}
	res, err := http.Get(got)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("shared url %d", res.StatusCode)
	}
}

func TestHubServePrintsKeyAndExits(t *testing.T) {
	t.Setenv(hubKeyEnv, "")
	var stderr bytes.Buffer
	err := runObserveServe([]string{"-addr", "127.0.0.1:0"}, &stderr)
	if err == nil {
		t.Fatal("serve started without a key")
	}
	got := stderr.String()
	if !strings.Contains(got, "Paste this line, then start the hub again:\nexport "+hubKeyEnv+"=") {
		t.Fatalf("stderr %q", got)
	}
	line := ""
	for _, part := range strings.Split(got, "\n") {
		if strings.HasPrefix(part, "export "+hubKeyEnv+"=") {
			line = part
		}
	}
	key := strings.TrimPrefix(line, "export "+hubKeyEnv+"=")
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		t.Fatalf("key line %q", line)
	}
}

func TestHubOpenNeedsKey(t *testing.T) {
	t.Setenv(hubKeyEnv, "")
	var stdout, stderr bytes.Buffer
	err := runObserveOpen([]string{"https://github.com/Example/Repo"}, &stdout, &stderr)
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("open without key: %v stdout %q", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), hubKeyEnv) {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestSceneKeyCollapsesThreeDot(t *testing.T) {
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
	commit := func(src, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("a.go"); err != nil {
			t.Fatal(err)
		}
		when = when.Add(time.Minute)
		h, err := wt.Commit(msg, &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: when}})
		if err != nil {
			t.Fatal(err)
		}
		return h.String()
	}
	base := commit("package a\n\nfunc A() {}\n", "base")
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("feature"), Create: true}); err != nil {
		t.Fatal(err)
	}
	feature := commit("package a\n\nfunc A() {}\n\nfunc B() { A() }\n", "feature")
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.Master}); err != nil {
		t.Fatal(err)
	}
	tip := commit("package a\n\nfunc A() {}\n\nfunc C() {}\n", "tip")
	key, overview, err := sceneKey(dir, "master...feature")
	if err != nil {
		t.Fatal(err)
	}
	left, right, err := git.Revs(dir, "master...feature")
	if err != nil {
		t.Fatal(err)
	}
	if overview || key != observe.RangeKey(left, right) {
		t.Fatalf("key %q overview %v", key, overview)
	}
	if left != base || right != feature {
		t.Fatalf("three-dot %s..%s, base %s feature %s tip %s", left, right, base, feature, tip)
	}
	two, _, err := git.Revs(dir, "master..feature")
	if err != nil {
		t.Fatal(err)
	}
	if two == left {
		t.Fatal("three-dot did not move to the merge base")
	}
}

func getScene(t *testing.T, url string) scene.Scene {
	t.Helper()
	if !strings.Contains(url, "scene.json") {
		if strings.Contains(url, "?") {
			url = strings.Replace(url, "?", "scene.json?", 1)
		} else {
			url += "scene.json"
		}
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := readAll(res)
		t.Fatalf("scene %d %s", res.StatusCode, raw)
	}
	var sc scene.Scene
	if err := json.NewDecoder(res.Body).Decode(&sc); err != nil {
		t.Fatal(err)
	}
	return sc
}
