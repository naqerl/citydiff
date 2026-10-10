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

// defaultSkinsDir is the extra skins directory. A missing directory is left
// uncreated and contributes no skins.
const defaultSkinsDir = "~/.config/citydiff/skins"

// skinPick is the skin the viewer opens on, plus any extra skins directory.
// dark and light are always the embedded skins. dark is the default.
type skinPick struct {
	embedName string
	dir       string
	file      string
	extraDir  string
}

func pickSkin(spec, skinsDir string) (skinPick, error) {
	extra, err := resolveSkinsDir(skinsDir)
	if err != nil {
		return skinPick{}, err
	}
	pick, err := resolveActive(strings.TrimSpace(spec), extra)
	if err != nil {
		return skinPick{}, err
	}
	pick.extraDir = extra
	return pick, nil
}

// resolveSkinsDir returns the extra skins directory. An empty spec uses
// defaultSkinsDir. A path that does not exist yields an empty directory and
// is not created.
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
		return "", fmt.Errorf("skins path %s is not a directory", expanded)
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

func resolveActive(spec, extraDir string) (skinPick, error) {
	if spec == "" {
		return skinPick{embedName: "dark"}, nil
	}
	info, err := os.Stat(spec)
	if err == nil {
		if info.IsDir() {
			if !hasSkinFile(spec) {
				return skinPick{}, fmt.Errorf("skin directory %s has no skin.json or skin.js", spec)
			}
			return skinPick{dir: spec}, nil
		}
		return skinPick{file: spec}, nil
	}
	if !os.IsNotExist(err) {
		return skinPick{}, err
	}
	if strings.ContainsAny(spec, `/\`) || !skinNameOK(spec) {
		return skinPick{}, fmt.Errorf("unknown skin %q", spec)
	}
	if builtinSkin(spec) {
		return skinPick{embedName: spec}, nil
	}
	if extraDir != "" {
		if found, ok := skinInDir(extraDir, spec); ok {
			return found, nil
		}
	}
	return skinPick{}, fmt.Errorf("unknown skin %q", spec)
}

func skinNameOK(name string) bool {
	if name == "" || name == "active" {
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

// skinInDir finds one extra skin. A directory named name that contains
// skin.json or skin.js wins over a sibling file. A JSON file wins over a
// script of the same name. dark and light are not found here; the embedded
// skins keep those names.
func skinInDir(root, name string) (skinPick, bool) {
	if !skinNameOK(name) || builtinSkin(name) {
		return skinPick{}, false
	}
	dir := filepath.Join(root, name)
	if info, err := os.Stat(dir); err == nil && info.IsDir() && hasSkinFile(dir) {
		return skinPick{dir: dir}, true
	}
	for _, ext := range []string{".json", ".js"} {
		file := filepath.Join(root, name+ext)
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			return skinPick{file: file}, true
		}
	}
	return skinPick{}, false
}

// skinChoice is one row in the page's skin menu. An empty id is the skin
// selected with -skin when that skin is a path, not a catalog name.
type skinChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type skinCatalog struct {
	Opened string       `json:"opened"`
	Skins  []skinChoice `json:"skins"`
}

func mountSkins(mux *http.ServeMux, pick skinPick) {
	mux.Handle("GET /skins.json", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skinCatalogFor(pick))
	})))
	mux.Handle("GET /skins/", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/skins/"))
		if rel == "/" || strings.Contains(rel, "..") {
			http.NotFound(w, r)
			return
		}
		rel = strings.TrimPrefix(rel, "/")
		if rel == "active" || strings.HasPrefix(rel, "active/") {
			rest := strings.TrimPrefix(strings.TrimPrefix(rel, "active"), "/")
			serveSkinFiles(w, r, "/skins/active", rest, pick)
			return
		}
		name, rest, ok := cutSkin(rel)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if builtinSkin(name) {
			serveEmbedSkin(w, r, rel)
			return
		}
		if pick.extraDir != "" {
			if found, foundOK := skinInDir(pick.extraDir, name); foundOK {
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

func skinCatalogFor(pick skinPick) skinCatalog {
	var skins []skinChoice
	for _, name := range builtinSkinNames() {
		skins = append(skins, skinChoice{ID: name, Label: name})
	}
	for _, name := range extraSkinNames(pick.extraDir) {
		skins = append(skins, skinChoice{ID: name, Label: name})
	}
	opened := catalogOpened(pick)
	if opened == "" && (pick.dir != "" || pick.file != "") {
		label := oneOffLabel(pick)
		for _, skin := range skins {
			if skin.ID == label {
				label += " (path)"
				break
			}
		}
		skins = append(skins, skinChoice{ID: "", Label: label})
	}
	return skinCatalog{Opened: opened, Skins: skins}
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

func catalogOpened(pick skinPick) string {
	if pick.embedName != "" {
		return pick.embedName
	}
	if pick.extraDir != "" && pick.dir != "" {
		rel, err := filepath.Rel(pick.extraDir, pick.dir)
		if err == nil && skinNameOK(rel) {
			return rel
		}
	}
	if pick.extraDir != "" && pick.file != "" && filepath.Clean(filepath.Dir(pick.file)) == filepath.Clean(pick.extraDir) {
		base := strings.TrimSuffix(filepath.Base(pick.file), filepath.Ext(pick.file))
		if skinNameOK(base) {
			return base
		}
	}
	if pick.dir == "" && pick.file == "" {
		return "dark"
	}
	return ""
}

func oneOffLabel(pick skinPick) string {
	if pick.dir != "" {
		return filepath.Base(pick.dir)
	}
	if pick.file != "" {
		return strings.TrimSuffix(filepath.Base(pick.file), filepath.Ext(pick.file))
	}
	return ""
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

func skinEntryName(file string) string {
	if strings.HasSuffix(strings.ToLower(file), ".js") {
		return "skin.js"
	}
	return "skin.json"
}

func serveSkinFiles(w http.ResponseWriter, r *http.Request, base, rest string, pick skinPick) {
	if pick.file != "" {
		if rest != "" && rest != skinEntryName(pick.file) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, pick.file)
		return
	}
	if pick.dir != "" {
		if rest == "" {
			target := "skin.json"
			if _, err := os.Stat(filepath.Join(pick.dir, "skin.json")); err != nil {
				target = "skin.js"
			}
			http.Redirect(w, r, path.Join(base, target), http.StatusTemporaryRedirect)
			return
		}
		full := filepath.Join(pick.dir, filepath.FromSlash(rest))
		rel, err := filepath.Rel(pick.dir, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, full)
		return
	}
	name := pick.embedName
	if name == "" {
		name = "dark"
	}
	if rest == "" {
		rest = "skin.json"
	}
	serveEmbedSkin(w, r, path.Join(name, rest))
}
