package view

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// relImport matches a module the page loads by a relative path: an import or
// export from, a dynamic import, and in index.html a script src or an import
// map target.
var relImport = regexp.MustCompile(`(?:from|import\(|src=|:)\s*"(\.\.?/[^"]+\.js)"`)

// TestEmbeddedImportsAreEmbedded keeps the embed list whole. The list names
// each file, and a module that is on disk but not in it passes every node test
// while the served page fails to import it and draws no city.
func TestEmbeddedImportsAreEmbedded(t *testing.T) {
	err := fs.WalkDir(FS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".html") {
			return nil
		}
		src, err := fs.ReadFile(FS, name)
		if err != nil {
			return err
		}
		for _, m := range relImport.FindAllStringSubmatch(string(src), -1) {
			target := path.Join(path.Dir(name), m[1])
			if _, err := fs.Stat(FS, target); err != nil {
				t.Errorf("%s loads %s, which view/embed.go does not embed", name, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
