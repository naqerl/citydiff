package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"citydiff/skins"
)

// The viewer is a browser program. Which skin is open is the page's business:
// the page keeps the choice in localStorage and switches skins without a
// reload. The process only has to know where the skins that are not in the
// binary live, and that is configuration, not a flag.
const skinsEnv = "CITYDIFF_SKINS_DIR"

// defaultSkinsDir is the extra skins directory when the environment says
// nothing. A missing directory is left uncreated and contributes no skins.
const defaultSkinsDir = "~/.config/citydiff/skins"

// skinsDir returns the extra skins directory, or "" when there is none. A
// path that cannot be used is reported once on stderr and the viewer still
// starts: a bad skins directory must not cost the user the city.
func skinsDir() string {
	dir, err := resolveSkinsDir(os.Getenv(skinsEnv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "citydiff: %v\n", err)
		return ""
	}
	return dir
}

// resolveSkinsDir expands spec, or defaultSkinsDir when spec is empty, and
// returns an absolute path. A directory that does not exist is not created.
func resolveSkinsDir(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = defaultSkinsDir
	}
	expanded, err := expandHome(spec)
	if err != nil {
		if spec == defaultSkinsDir {
			return "", nil
		}
		return "", err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", expanded)
	}
	return filepath.Abs(expanded)
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

func skinNameOK(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func builtinSkin(name string) bool {
	f, err := skins.FS.Open(name + "/skin.json")
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func hasSkinFile(dir string) bool {
	for _, name := range []string{"skin.json", "skin.js"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// skinInDir finds one skin in the extra skins directory: a folder of that
// name holding a skin file, else a skin.json, else a skin.js. A built-in
// name is never found here; the embedded skins keep those names.
func skinInDir(root, name string) (string, bool) {
	if !skinNameOK(name) || builtinSkin(name) {
		return "", false
	}
	dir := filepath.Join(root, name)
	if info, err := os.Stat(dir); err == nil && info.IsDir() && hasSkinFile(dir) {
		return dir, true
	}
	for _, ext := range []string{".json", ".js"} {
		file := filepath.Join(root, name+ext)
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			return file, true
		}
	}
	return "", false
}

// skinEntry is the file inside a skin directory that the page reads first.
func skinEntry(dir string) string {
	if info, err := os.Stat(filepath.Join(dir, "skin.json")); err == nil && !info.IsDir() {
		return "skin.json"
	}
	return "skin.js"
}

// skinChoice is one row in the page's skin menu.
type skinChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type skinCatalog struct {
	Skins []skinChoice `json:"skins"`
}

// mountSkins serves the catalog and the skin files. The page decides which
// one to open; the server only knows where they are.
func mountSkins(mux *http.ServeMux, dir string) {
	mux.Handle("GET /skins.json", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skinCatalogFor(dir))
	})))
	mux.Handle("GET /skins/", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/skins/"))
		if rel == "/" || strings.Contains(rel, "..") {
			http.NotFound(w, r)
			return
		}
		rel = strings.TrimPrefix(rel, "/")
		name, rest, ok := cutSkin(rel)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if builtinSkin(name) {
			serveEmbedSkin(w, r, rel)
			return
		}
		if dir != "" {
			if found, foundOK := skinInDir(dir, name); foundOK {
				serveSkinFiles(w, r, "/skins/"+name, rest, found)
				return
			}
		}
		http.NotFound(w, r)
	})))
}

func cutSkin(rel string) (name, rest string, ok bool) {
	name, rest, _ = strings.Cut(rel, "/")
	if !skinNameOK(name) {
		return "", "", false
	}
	return name, rest, true
}

func skinCatalogFor(dir string) skinCatalog {
	var skins []skinChoice
	for _, name := range builtinSkinNames() {
		skins = append(skins, skinChoice{ID: name, Label: name})
	}
	for _, name := range extraSkinNames(dir) {
		skins = append(skins, skinChoice{ID: name, Label: name})
	}
	return skinCatalog{Skins: skins}
}

func builtinSkinNames() []string {
	entries, err := skins.FS.ReadDir(".")
	if err != nil {
		return []string{"dark", "light"}
	}
	hasDark := false
	hasLight := false
	var rest []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !builtinSkin(name) {
			continue
		}
		switch name {
		case "dark":
			hasDark = true
		case "light":
			hasLight = true
		default:
			if skinNameOK(name) {
				rest = append(rest, name)
			}
		}
	}
	sort.Strings(rest)
	names := make([]string, 0, 2+len(rest))
	if hasDark {
		names = append(names, "dark")
	}
	if hasLight {
		names = append(names, "light")
	}
	return append(names, rest...)
}

func extraSkinNames(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !skinNameOK(name) || builtinSkin(name) || !hasSkinFile(filepath.Join(root, name)) {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json" && ext != ".js" {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if !skinNameOK(base) || builtinSkin(base) || seen[base] {
			continue
		}
		if ext == ".js" {
			if _, err := os.Stat(filepath.Join(root, base+".json")); err == nil {
				continue
			}
		}
		seen[base] = true
		names = append(names, base)
	}
	sort.Strings(names)
	return names
}

func serveEmbedSkin(w http.ResponseWriter, r *http.Request, name string) {
	f, err := skins.FS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stat, err := f.Stat()
	f.Close()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, skins.FS, name)
}

// skinEntryName is the name a single-file skin is served under: the page
// always reads the canonical entry name, never the file's own name.
func skinEntryName(file string) string {
	if strings.HasSuffix(strings.ToLower(file), ".js") {
		return "skin.js"
	}
	return "skin.json"
}

// serveSkinFiles serves one skin from the extra skins directory. A file
// target serves that file alone; a directory target redirects to its entry
// file and then serves the files beside it.
func serveSkinFiles(w http.ResponseWriter, r *http.Request, base, rest, target string) {
	info, err := os.Stat(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !info.IsDir() {
		if rest != "" && rest != skinEntryName(target) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, target)
		return
	}
	if rest == "" {
		http.Redirect(w, r, path.Join(base, skinEntry(target)), http.StatusTemporaryRedirect)
		return
	}
	full := filepath.Join(target, filepath.FromSlash(rest))
	rel, err := filepath.Rel(target, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}
