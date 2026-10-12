package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A plugin is a JavaScript module the page imports and hands its API
// (PLUGINS.md). What a plugin does is the page's business. The process only
// has to know where the plugins live and serve their files, and that is
// configuration, not a flag.
const pluginsEnv = "CITYDIFF_PLUGINS_DIR"

// defaultPluginsDir is the plugins directory when the environment says
// nothing. A missing directory is left uncreated and loads no plugins.
const defaultPluginsDir = "~/.config/citydiff/plugins"

// pluginEntry is the file the page imports from a plugin's folder. A folder
// without it is not a plugin.
const pluginEntry = "plugin.js"

// pluginsDir returns the plugins directory, or "" when there is none. A path
// that cannot be used is reported once on stderr and the viewer still
// starts: a bad plugins directory must not cost the user the city.
func pluginsDir() string {
	dir, err := resolveConfigDir(os.Getenv(pluginsEnv), defaultPluginsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "citydiff: %v\n", err)
		return ""
	}
	return dir
}

type pluginCatalog struct {
	Plugins []string `json:"plugins"`
}

// pluginNames is every folder of root that holds a plugin.js, by name, in
// order. The folder's name is the plugin's.
func pluginNames(root string) []string {
	names := []string{}
	if root == "" {
		return names
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return names
	}
	for _, entry := range entries {
		name := entry.Name()
		if !configNameOK(name) {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, name, pluginEntry)); err != nil || info.IsDir() {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// mountPlugins serves the catalog and every plugin's folder. The catalog is
// read on each request, so a plugin added while the viewer runs loads on the
// next reload.
//
// The embedded viewer is served no-store because its files carry no
// modification time. A plugin's files are real files and do: they are served
// to be revalidated, so a reload asks again and an unchanged file, a model of
// many megabytes say, comes back as a 304 instead of its bytes.
func mountPlugins(mux *http.ServeMux, dir string) {
	mux.Handle("GET /plugins.json", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pluginCatalog{Plugins: pluginNames(dir)})
	})))
	mux.HandleFunc("GET /plugins/", func(w http.ResponseWriter, r *http.Request) {
		file, ok := pluginFile(dir, strings.TrimPrefix(r.URL.Path, "/plugins/"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, file)
	})
}

// pluginFile maps "name/path" under /plugins/ to a file in that plugin's
// folder. A name that is not a plugin, a path that leaves the folder, a dot
// file (a .git, an .env) and a directory are not served.
func pluginFile(root, rel string) (string, bool) {
	if root == "" {
		return "", false
	}
	name, rest, ok := strings.Cut(rel, "/")
	if !ok || !configNameOK(name) || rest == "" {
		return "", false
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	folder := filepath.Join(root, name)
	if info, err := os.Stat(filepath.Join(folder, pluginEntry)); err != nil || info.IsDir() {
		return "", false
	}
	file := filepath.Join(folder, filepath.FromSlash(rest))
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return "", false
	}
	return file, true
}
