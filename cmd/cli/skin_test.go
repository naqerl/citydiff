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

func skinMux(dir string) *httptest.Server {
	mux := http.NewServeMux()
	mountSkins(mux, dir)
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
	if _, err := resolveSkinsDir(none); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(none); !os.IsNotExist(err) {
		t.Fatal("a missing skins directory was created")
	}
	srv := skinMux("")
	defer srv.Close()
	for _, path := range []string{"/skins/dark/skin.json", "/skins/light/skin.json"} {
		code, body := getBody(t, srv, path)
		if code != http.StatusOK {
			t.Fatalf("%s: %d", path, code)
		}
		if !strings.Contains(body, `"name"`) {
			t.Fatalf("%s: %s", path, body)
		}
	}
	code, _ := getBody(t, srv, "/skins/missing/skin.json")
	if code != http.StatusNotFound {
		t.Fatalf("missing skin: %d", code)
	}
	if code, _ = getBody(t, srv, "/skins/active/skin.json"); code != http.StatusNotFound {
		t.Fatalf("the process no longer picks a skin, so there is no active one: %d", code)
	}
}

func TestSkinsDirectory(t *testing.T) {
	root := t.TempDir()
	paper := filepath.Join(root, "paper")
	if err := os.Mkdir(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"paper","plane":{"color":"#ffffff"}}`)
	if err := os.WriteFile(filepath.Join(paper, "skin.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paper, "sky.webp"), []byte("webp"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file skin beside the folder, and a folder without a skin file.
	if err := os.WriteFile(filepath.Join(root, "ink.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// dark is embedded, so a folder of that name is not taken from here.
	if err := os.Mkdir(filepath.Join(root, "dark"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dark", "skin.json"), []byte(`{"name":"dark-here"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := skinMux(root)
	defer srv.Close()

	code, body := getBody(t, srv, "/skins/paper/skin.json")
	if code != http.StatusOK || !strings.Contains(body, "paper") {
		t.Fatalf("folder skin: %d %s", code, body)
	}
	code, body = getBody(t, srv, "/skins/paper/sky.webp")
	if code != http.StatusOK || body != "webp" {
		t.Fatalf("folder asset: %d %q", code, body)
	}
	code, body = getBody(t, srv, "/skins/paper/")
	if code != http.StatusOK || !strings.Contains(body, "paper") {
		t.Fatalf("folder entry redirect lands on the skin file: %d %s", code, body)
	}
	code, body = getBody(t, srv, "/skins/ink/skin.json")
	if code != http.StatusOK || !strings.Contains(body, "paper") {
		t.Fatalf("file skin: %d %s", code, body)
	}
	if code, _ = getBody(t, srv, "/skins/ink/other.png"); code != http.StatusNotFound {
		t.Fatalf("file skin extra: %d", code)
	}
	if code, _ = getBody(t, srv, "/skins/empty/skin.json"); code != http.StatusNotFound {
		t.Fatalf("folder without a skin file: %d", code)
	}
	code, body = getBody(t, srv, "/skins/dark/skin.json")
	if code != http.StatusOK || strings.Contains(body, "dark-here") {
		t.Fatalf("the embedded dark skin wins: %d %s", code, body)
	}
	if code, _ = getBody(t, srv, "/skins/paper/../../skin_test.go"); code != http.StatusNotFound {
		t.Fatalf("escape: %d", code)
	}
}

func TestSkinCatalog(t *testing.T) {
	root := t.TempDir()
	paper := filepath.Join(root, "paper")
	if err := os.Mkdir(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paper, "skin.json"), []byte(`{"name":"paper"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ink.json"), []byte(`{"name":"ink"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	ids := choiceIDs(getCatalog(t, "").Skins)
	if len(ids) < 2 || ids[0] != "dark" || ids[1] != "light" {
		t.Fatalf("built-ins first: %v", ids)
	}
	if strings.Contains(strings.Join(ids, ","), "paper") {
		t.Fatalf("a missing directory contributed skins: %v", ids)
	}

	ids = choiceIDs(getCatalog(t, root).Skins)
	want := []string{"dark", "light", "ink", "paper"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("catalog = %v want %v", ids, want)
	}
}

func getCatalog(t *testing.T, dir string) skinCatalog {
	t.Helper()
	srv := skinMux(dir)
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
	srv := skinMux(root)
	defer srv.Close()
	code, body := getBody(t, srv, "/skins/ink/skin.js")
	if code != http.StatusOK || !strings.Contains(body, "112233") {
		t.Fatalf("script skin: %d %s", code, body)
	}
	if code, _ = getBody(t, srv, "/skins/ink/skin.json"); code != http.StatusNotFound {
		t.Fatalf("a folder with only skin.js has no skin.json: %d", code)
	}
}

func TestSkinsDirFromEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv(skinsEnv, root)
	if got := skinsDir(); got == "" {
		t.Fatalf("%s=%s resolved to nothing", skinsEnv, root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := skinsDir(); got != abs {
		t.Fatalf("got %q want %q", got, abs)
	}

	// A missing directory is not created and contributes nothing.
	t.Setenv(skinsEnv, missingSkins(t))
	if got := skinsDir(); got != "" {
		t.Fatalf("missing directory resolved to %q", got)
	}

	// A file where a directory belongs is reported, not fatal.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(skinsEnv, file)
	if got := skinsDir(); got != "" {
		t.Fatalf("a file resolved to %q", got)
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
