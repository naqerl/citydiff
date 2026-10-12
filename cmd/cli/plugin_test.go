package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pluginMux(dir string) *httptest.Server {
	mux := http.NewServeMux()
	mountPlugins(mux, dir)
	return httptest.NewServer(mux)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plugins lays out a plugins directory: two plugins, a folder that is not
// one, a stray file at the top, and a name the URL could not carry.
func plugins(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hello", "plugin.js"), "export const api = 1;\n")
	writeFile(t, filepath.Join(root, "hello", "lib", "fold.js"), "export const fold = 1;\n")
	writeFile(t, filepath.Join(root, "hello", "model.bin"), "weights")
	writeFile(t, filepath.Join(root, "hello", ".env"), "TOKEN=secret")
	writeFile(t, filepath.Join(root, "hello", ".git", "config"), "[core]")
	writeFile(t, filepath.Join(root, "voice", "plugin.js"), "export const api = 1;\n")
	writeFile(t, filepath.Join(root, "notes", "README.md"), "not a plugin")
	writeFile(t, filepath.Join(root, "stray.js"), "export const api = 1;\n")
	writeFile(t, filepath.Join(root, "bad name", "plugin.js"), "export const api = 1;\n")
	return root
}

func TestPluginCatalog(t *testing.T) {
	srv := pluginMux(plugins(t))
	defer srv.Close()
	code, body := getBody(t, srv, "/plugins.json")
	if code != http.StatusOK {
		t.Fatalf("catalog: %d", code)
	}
	var catalog pluginCatalog
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	if strings.Join(catalog.Plugins, ",") != "hello,voice" {
		t.Fatalf("plugins: %v", catalog.Plugins)
	}

	// No directory is no plugins, written as an empty list the page can walk.
	none := pluginMux("")
	defer none.Close()
	if _, body := getBody(t, none, "/plugins.json"); strings.TrimSpace(body) != `{"plugins":[]}` {
		t.Fatalf("no directory: %s", body)
	}
}

func TestPluginFiles(t *testing.T) {
	srv := pluginMux(plugins(t))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/plugins/hello/plugin.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("plugin.js: %d", res.StatusCode)
	}
	// A module script is refused by the browser under any other type.
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Fatalf("plugin.js type: %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("plugin.js cache: %q", got)
	}
	modified := res.Header.Get("Last-Modified")
	if modified == "" {
		t.Fatal("plugin.js has no Last-Modified to revalidate against")
	}

	// A reload asks again, and an unchanged file is a 304, not its bytes.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/plugins/hello/model.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-Modified-Since", modified)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged model: %d", res.StatusCode)
	}

	for path, want := range map[string]int{
		"/plugins/hello/lib/fold.js":    http.StatusOK,
		"/plugins/hello/model.bin":      http.StatusOK,
		"/plugins/voice/plugin.js":      http.StatusOK,
		"/plugins/hello/.env":           http.StatusNotFound,
		"/plugins/hello/.git/config":    http.StatusNotFound,
		"/plugins/hello/lib":            http.StatusNotFound,
		"/plugins/hello/":               http.StatusNotFound,
		"/plugins/hello/missing.js":     http.StatusNotFound,
		"/plugins/notes/README.md":      http.StatusNotFound,
		"/plugins/bad%20name/plugin.js": http.StatusNotFound,
	} {
		if code, _ := getBody(t, srv, path); code != want {
			t.Errorf("%s: %d, want %d", path, code, want)
		}
	}
}

// The mux cleans a path before it gets here, so the checks of pluginFile are
// asked directly: a path the cleaning would have caught must still not leave
// the plugin's folder.
func TestPluginFileStaysInItsFolder(t *testing.T) {
	root := plugins(t)
	for _, rel := range []string{
		"hello/../voice/plugin.js",
		"hello/lib/../../stray.js",
		"../hello/plugin.js",
		"hello//plugin.js",
		"hello/./plugin.js",
		"hello",
		"",
	} {
		if file, ok := pluginFile(root, rel); ok {
			t.Errorf("%q served %s", rel, file)
		}
	}
	if _, ok := pluginFile("", "hello/plugin.js"); ok {
		t.Error("no directory served a file")
	}
	if file, ok := pluginFile(root, "hello/lib/fold.js"); !ok || file != filepath.Join(root, "hello", "lib", "fold.js") {
		t.Errorf("nested file: %q %v", file, ok)
	}
}

func TestPluginsDirFromEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv(pluginsEnv, root)
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := pluginsDir(); got != abs {
		t.Fatalf("got %q want %q", got, abs)
	}

	// A missing directory is not created and loads nothing.
	missing := filepath.Join(t.TempDir(), "none")
	t.Setenv(pluginsEnv, missing)
	if got := pluginsDir(); got != "" {
		t.Fatalf("missing directory resolved to %q", got)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("a missing plugins directory was created")
	}

	// A file where a directory belongs is reported, not fatal.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	writeFile(t, file, "x")
	t.Setenv(pluginsEnv, file)
	if got := pluginsDir(); got != "" {
		t.Fatalf("a file resolved to %q", got)
	}
}
