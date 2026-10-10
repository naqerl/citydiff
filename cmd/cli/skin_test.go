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
)

func skinMux(pick skinPick) *httptest.Server {
	mux := http.NewServeMux()
	mountSkins(mux, pick)
	return httptest.NewServer(mux)
}

func getBody(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(raw)
}

func missingSkins(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "none")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp skins dir %s already exists", dir)
	}
	return dir
}

func TestBuiltinSkins(t *testing.T) {
	none := missingSkins(t)
	pick, err := pickSkin("", none)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(none); !os.IsNotExist(err) {
		t.Fatal("a missing skins directory was created")
	}
	srv := skinMux(pick)
	defer srv.Close()
	for _, path := range []string{"/skins/dark/skin.json", "/skins/light/skin.json", "/skins/active/skin.json"} {
		code, body := getBody(t, srv, path)
		if code != http.StatusOK {
			t.Fatalf("%s: %d", path, code)
		}
		if !strings.Contains(body, `"name"`) {
			t.Fatalf("%s: %s", path, body)
		}
	}
	_, active := getBody(t, srv, "/skins/active/skin.json")
	_, dark := getBody(t, srv, "/skins/dark/skin.json")
	if active != dark {
		t.Fatal("the default active skin is dark")
	}
	code, _ := getBody(t, srv, "/skins/missing/skin.json")
	if code != http.StatusNotFound {
		t.Fatalf("missing skin: %d", code)
	}
}

func TestPickSkin(t *testing.T) {
	none := missingSkins(t)
	if _, err := pickSkin("light", none); err != nil {
		t.Fatal(err)
	}
	if _, err := pickSkin("nope", none); err == nil {
		t.Fatal("unknown name accepted")
	}
	dir := t.TempDir()
	if _, err := pickSkin(dir, none); err == nil {
		t.Fatal("directory without skin.json accepted")
	}
	raw := []byte(`{"name":"paper","plane":{"color":"#ffffff"}}`)
	file := filepath.Join(dir, "paper.json")
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pick, err := pickSkin(file, none)
	if err != nil {
		t.Fatal(err)
	}
	srv := skinMux(pick)
	defer srv.Close()
	code, body := getBody(t, srv, "/skins/active/skin.json")
	if code != http.StatusOK || !strings.Contains(body, `"paper"`) {
		t.Fatalf("file skin: %d %s", code, body)
	}
	code, _ = getBody(t, srv, "/skins/active/other.png")
	if code != http.StatusNotFound {
		t.Fatalf("file skin extra: %d", code)
	}

	skinDir := filepath.Join(dir, "folder")
	if err := os.Mkdir(skinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skinDir, "skin.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skinDir, "sky.webp"), []byte("webp"), 0o644); err != nil {
		t.Fatal(err)
	}
	pick, err = pickSkin(skinDir, none)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := skinMux(pick)
	defer srv2.Close()
	code, body = getBody(t, srv2, "/skins/active/skin.json")
	if code != http.StatusOK || !strings.Contains(body, `"paper"`) {
		t.Fatalf("dir skin: %d %s", code, body)
	}
	code, body = getBody(t, srv2, "/skins/active/sky.webp")
	if code != http.StatusOK || body != "webp" {
		t.Fatalf("dir asset: %d %q", code, body)
	}
	code, _ = getBody(t, srv2, "/skins/active/../../skin_test.go")
	if code != http.StatusNotFound {
		t.Fatalf("escape: %d", code)
	}
}

func TestSkinsDirectory(t *testing.T) {
	root := t.TempDir()
	paper := filepath.Join(root, "paper")
	if err := os.Mkdir(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	paperJSON := []byte(`{"name":"paper","plane":{"color":"#abcdef"}}`)
	if err := os.WriteFile(filepath.Join(paper, "skin.json"), paperJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paper, "sky.webp"), []byte("webp"), 0o644); err != nil {
		t.Fatal(err)
	}
	ink := []byte(`{"name":"ink","plane":{"color":"#111111"}}`)
	if err := os.WriteFile(filepath.Join(root, "ink.json"), ink, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "light"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "light", "skin.json"), []byte(`{"name":"not-light"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	pick, err := pickSkin("", root)
	if err != nil {
		t.Fatal(err)
	}
	if pick.embedName != "dark" {
		t.Fatalf("default skin %q", pick.embedName)
	}
	srv := skinMux(pick)
	defer srv.Close()

	code, body := getBody(t, srv, "/skins/paper/skin.json")
	if code != http.StatusOK || !strings.Contains(body, `"paper"`) {
		t.Fatalf("paper: %d %s", code, body)
	}
	code, body = getBody(t, srv, "/skins/paper/sky.webp")
	if code != http.StatusOK || body != "webp" {
		t.Fatalf("paper asset: %d %q", code, body)
	}
	code, body = getBody(t, srv, "/skins/ink/skin.json")
	if code != http.StatusOK || !strings.Contains(body, `"ink"`) {
		t.Fatalf("ink: %d %s", code, body)
	}
	code, _ = getBody(t, srv, "/skins/ink/other.png")
	if code != http.StatusNotFound {
		t.Fatalf("ink extra: %d", code)
	}
	code, body = getBody(t, srv, "/skins/light/skin.json")
	if code != http.StatusOK || strings.Contains(body, "not-light") || !strings.Contains(body, `"name": "light"`) {
		t.Fatalf("builtin light shadowed: %d %s", code, body)
	}
	code, _ = getBody(t, srv, "/skins/paper/../../skin_test.go")
	if code != http.StatusNotFound {
		t.Fatalf("catalog escape: %d", code)
	}
	_, active := getBody(t, srv, "/skins/active/skin.json")
	_, dark := getBody(t, srv, "/skins/dark/skin.json")
	if active != dark {
		t.Fatal("an extra directory still opens on dark")
	}

	pick, err = pickSkin("paper", root)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := skinMux(pick)
	defer srv2.Close()
	_, active = getBody(t, srv2, "/skins/active/skin.json")
	if !strings.Contains(active, `"paper"`) {
		t.Fatalf("named extra skin: %s", active)
	}
	code, body = getBody(t, srv2, "/skins/ink/skin.json")
	if code != http.StatusOK || !strings.Contains(body, `"ink"`) {
		t.Fatalf("other extra skin: %d %s", code, body)
	}

	pick, err = pickSkin("light", root)
	if err != nil {
		t.Fatal(err)
	}
	if pick.embedName != "light" || pick.dir != "" || pick.file != "" {
		t.Fatalf("light resolved to %+v", pick)
	}

	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pickSkin("", file); err == nil {
		t.Fatal("a file was accepted as the skins directory")
	}
}

func TestSkinCatalog(t *testing.T) {
	none := missingSkins(t)
	pick, err := pickSkin("", none)
	if err != nil {
		t.Fatal(err)
	}
	catalog := getCatalog(t, pick)
	if catalog.Opened != "dark" {
		t.Fatalf("opened %q", catalog.Opened)
	}
	if ids := choiceIDs(catalog.Skins); strings.Join(ids, ",") != "dark,light" {
		t.Fatalf("builtins %v", ids)
	}

	root := t.TempDir()
	ink := filepath.Join(root, "ink")
	if err := os.Mkdir(ink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ink, "skin.js"), []byte("export default { name: \"ink\" };\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "paper.json"), []byte(`{"name":"paper"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "paper.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(root, "dark")
	if err := os.Mkdir(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "skin.json"), []byte(`{"name":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pick, err = pickSkin("ink", root)
	if err != nil {
		t.Fatal(err)
	}
	catalog = getCatalog(t, pick)
	if catalog.Opened != "ink" {
		t.Fatalf("opened %q", catalog.Opened)
	}
	if ids := choiceIDs(catalog.Skins); strings.Join(ids, ",") != "dark,light,ink,paper" {
		t.Fatalf("catalog %v", ids)
	}

	one := t.TempDir()
	if err := os.WriteFile(filepath.Join(one, "skin.json"), []byte(`{"name":"custom"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pick, err = pickSkin(one, none)
	if err != nil {
		t.Fatal(err)
	}
	catalog = getCatalog(t, pick)
	if catalog.Opened != "" {
		t.Fatalf("path skin opened as %q", catalog.Opened)
	}
	last := catalog.Skins[len(catalog.Skins)-1]
	if last.ID != "" || last.Label != filepath.Base(one) {
		t.Fatalf("path row %+v", last)
	}
}

func getCatalog(t *testing.T, pick skinPick) skinCatalog {
	t.Helper()
	srv := skinMux(pick)
	defer srv.Close()
	code, body := getBody(t, srv, "/skins.json")
	if code != http.StatusOK {
		t.Fatalf("catalog: %d %s", code, body)
	}
	var catalog skinCatalog
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func choiceIDs(skins []skinChoice) []string {
	ids := make([]string, len(skins))
	for i, skin := range skins {
		ids[i] = skin.ID
	}
	return ids
}

func TestScriptSkin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ink")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := []byte("export default { name: \"ink\", plane: { color: \"#112233\" } };\n")
	if err := os.WriteFile(filepath.Join(dir, "skin.js"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	pick, err := pickSkin("ink", root)
	if err != nil {
		t.Fatal(err)
	}
	srv := skinMux(pick)
	defer srv.Close()
	code, body := getBody(t, srv, "/skins/active/skin.js")
	if code != http.StatusOK || !strings.Contains(body, "export default") {
		t.Fatalf("script skin: %d %s", code, body)
	}
	code, _ = getBody(t, srv, "/skins/active/skin.json")
	if code != http.StatusNotFound {
		t.Fatalf("script served as json: %d", code)
	}
	code, body = getBody(t, srv, "/skins/ink/skin.js")
	if code != http.StatusOK || !strings.Contains(body, "112233") {
		t.Fatalf("named script: %d %s", code, body)
	}
}

func TestDefaultSkinsDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "citydiff", "skins")
	def, err := resolveSkinsDir("")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := resolveSkinsDir(defaultSkinsDir)
	if err != nil {
		t.Fatal(err)
	}
	if def != explicit {
		t.Fatalf("default %q, explicit %q", def, explicit)
	}
	info, statErr := os.Stat(want)
	if os.IsNotExist(statErr) {
		if def != "" {
			t.Fatalf("missing default dir resolved to %q", def)
		}
		return
	}
	if statErr != nil {
		t.Fatal(statErr)
	}
	if !info.IsDir() {
		t.Fatal("default skins path is not a directory")
	}
	abs, err := filepath.Abs(want)
	if err != nil {
		t.Fatal(err)
	}
	if def != abs {
		t.Fatalf("got %q want %q", def, abs)
	}
}
