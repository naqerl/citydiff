package main

import (
	"bytes"
	"html"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"citydiff/lib/scene"
)

func TestCardStats(t *testing.T) {
	dir, hashes := rangeRepo(t)
	snap, err := buildSnapshot(dir, hashes[0]+".."+hashes[2])
	if err != nil {
		t.Fatal(err)
	}
	groups, quiet := cardStats(snap.scene)
	if quiet != "" {
		t.Fatalf("quiet %q", quiet)
	}
	got := map[string][]string{}
	for _, g := range groups {
		for _, part := range g.Parts {
			got[g.Label] = append(got[g.Label], part.Text)
		}
	}
	if strings.Join(got["packages"], " ") != "~1" {
		t.Fatalf("packages %v", got["packages"])
	}
	if strings.Join(got["declarations"], " ") != "+2" {
		t.Fatalf("declarations %v", got["declarations"])
	}
	if strings.Join(got["calls"], " ") != "+2" {
		t.Fatalf("calls %v", got["calls"])
	}
	if _, ok := got["dependencies"]; ok {
		t.Fatalf("dependencies %v", got["dependencies"])
	}

	over, err := buildSnapshot(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	groups, quiet = cardStats(over.scene)
	if quiet != "" || over.scene.Diff {
		t.Fatalf("overview quiet %q diff %v", quiet, over.scene.Diff)
	}
	totals := map[string]int{}
	for _, g := range groups {
		totals[g.Label] = g.Count
	}
	if totals["packages"] != 1 || totals["declarations"] != 3 || totals["calls"] != 2 {
		t.Fatalf("overview totals %v", totals)
	}

	_, quiet = cardStats(scene.Scene{Diff: true, Packages: []scene.Package{{
		ID: "a", Name: "a", Change: "same",
		Entities: []scene.Entity{{Kind: "function", Name: "F", Change: "same"}},
	}}})
	if quiet != "low level code change" {
		t.Fatalf("quiet %q", quiet)
	}
}

func TestHubCardCachedBesideRepo(t *testing.T) {
	dir, hashes := rangeRepo(t)
	srv := testHub(t, dir)
	status, opened, raw := postObs(t, srv, `{"remote":"https://github.com/Example/Repo"}`)
	if status != http.StatusCreated {
		t.Fatalf("open %d %s", status, raw)
	}

	body := getCard(t, opened.URL)
	if !strings.Contains(body, `property="og:title" content="Example/Repo"`) {
		t.Fatalf("overview title missing:\n%s", metaHead(body))
	}
	if !strings.Contains(body, `name="twitter:card" content="summary_large_image"`) {
		t.Fatal("twitter card missing")
	}
	imgURL := ogImage(t, body)
	if !strings.Contains(imgURL, "/card.png?") || !strings.Contains(imgURL, "v=") {
		t.Fatalf("image %s", imgURL)
	}

	pngBody := getCard(t, imgURL)
	assertPNG(t, pngBody)
	matches, err := filepath.Glob(filepath.Join(cardDir(dir), "*.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("cards %v", matches)
	}
	sentinel := []byte("cached-card")
	if err := os.WriteFile(matches[0], sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getCard(t, imgURL); got != string(sentinel) {
		t.Fatalf("second read rebuilt the card (%d bytes)", len(got))
	}

	span := hashes[0][:10] + ".." + hashes[2][:10]
	ranged := getCard(t, opened.URL+"?range="+hashes[0]+".."+hashes[2])
	if !strings.Contains(ranged, `property="og:title" content="`+span+`"`) {
		t.Fatalf("range title %s missing:\n%s", span, metaHead(ranged))
	}
	rangeImg := ogImage(t, ranged)
	rangePNG := getCard(t, rangeImg)
	assertPNG(t, rangePNG)

	tour := `{"version":1,"title":"Third arrives","range":"` + hashes[0] + `..` + hashes[2] + `","steps":[{"title":"The change","note":"Second calls First. Then Third arrives."}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/"+opened.ID+"/api/tours", strings.NewReader(tour))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testHubKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("tour %d", res.StatusCode)
	}
	page := getCard(t, opened.URL+"?tour=third-arrives")
	if !strings.Contains(page, `property="og:title" content="Third arrives"`) {
		t.Fatalf("tour title missing:\n%s", metaHead(page))
	}
	if !strings.Contains(page, `content="Second calls First.`) {
		t.Fatalf("tour description missing:\n%s", metaHead(page))
	}
	assertPNG(t, getCard(t, ogImage(t, page)))

	matches, err = filepath.Glob(filepath.Join(cardDir(dir), "*.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("one file per link, got %d", len(matches))
	}

	var wg sync.WaitGroup
	errCh := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := http.Get(rangeImg)
			if err != nil {
				errCh <- err.Error()
				return
			}
			buf, _ := readAll(res)
			res.Body.Close()
			if res.StatusCode != http.StatusOK || string(buf) != rangePNG {
				errCh <- res.Status
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Fatal(msg)
	}
}

func TestRenderCard(t *testing.T) {
	raw, err := renderCard(cardView{
		Kind:     "tour",
		Eyebrow:  "naqerl/citydiff",
		Title:    "hub: share a public repository as one URL",
		Subtitle: "3a564a5d4e..b7285ce1aa · 8 steps",
		Groups: []cardGroup{
			{Label: "packages", Parts: []cardPart{{Text: "~4", Color: cardModified}}},
			{Label: "declarations", Parts: []cardPart{{Text: "+120", Color: cardAdded}, {Text: "−18", Color: cardRemoved}, {Text: "~6", Color: cardModified}}},
			{Label: "calls", Parts: []cardPart{{Text: "+40", Color: cardAdded}, {Text: "−3", Color: cardRemoved}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPNG(t, string(raw))
	again, err := renderCard(cardView{
		Kind:     "overview",
		Eyebrow:  "citydiff",
		Title:    "Example/Repo",
		Subtitle: "main",
		Groups: []cardGroup{
			{Label: "packages", Count: 1},
			{Label: "declarations", Count: 3},
			{Label: "calls", Count: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPNG(t, string(again))
	if bytes.Equal(raw, again) {
		t.Fatal("overview and tour drew the same image")
	}
}

func getCard(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, err := readAll(res)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s %d %s", url, res.StatusCode, buf)
	}
	return string(buf)
}

func ogImage(t *testing.T, page string) string {
	t.Helper()
	const marker = `property="og:image" content="`
	i := strings.Index(page, marker)
	if i < 0 {
		t.Fatalf("no og:image\n%s", metaHead(page))
	}
	rest := page[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatal("unclosed og:image")
	}
	return html.UnescapeString(rest[:j])
}

func metaHead(page string) string {
	i := strings.Index(page, "</head>")
	if i < 0 || i > 2500 {
		if len(page) > 800 {
			return page[:800]
		}
		return page
	}
	head := page[:i]
	k := strings.Index(head, "<title>")
	if k >= 0 {
		head = head[k:]
	}
	return head
}

func assertPNG(t *testing.T, body string) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("png: %v", err)
	}
	if cfg.Width != cardWidth || cfg.Height != cardHeight {
		t.Fatalf("size %dx%d", cfg.Width, cfg.Height)
	}
}
