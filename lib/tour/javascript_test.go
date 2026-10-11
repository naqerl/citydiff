package tour

import (
	"strings"
	"testing"

	"citydiff/lib"
	"citydiff/lib/parser"
	"citydiff/lib/scene"
)

func jsScene(t *testing.T) scene.Scene {
	t.Helper()
	files, err := parser.New().Parse(lib.Mem([]lib.File{
		{Path: "package.json", Src: []byte(`{"name": "shop"}`)},
		{Path: "lib/cart.js", Src: []byte("export class Cart {\n  add(item) { this.total() }\n  total() {}\n}\nexport function empty() { return new Cart() }\n")},
		{Path: "src/main.mjs", Src: []byte("import { Cart, empty } from '../lib/cart.js'\nexport function run() { const c = empty(); new Cart().add(1); const k = new Cart(); k.add(2) }\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return scene.Build(nil, files)
}

// A JavaScript declaration is named module.func or Class.method, the
// module being its directory under the package name.
func TestResolveJavaScriptNames(t *testing.T) {
	ix := NewIndex(jsScene(t))
	for name, want := range map[string]string{
		"shop/lib":            "shop/lib",
		"lib.empty":           "lib.empty",
		"shop/lib.empty":      "lib.empty",
		"Cart.add":            "Cart.add",
		"shop/lib.Cart.total": "Cart.total",
		"src.run":             "src.run",
	} {
		node, err := ix.Resolve(name)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if !strings.HasSuffix(node.Name, want) {
			t.Errorf("%q = %s, want %s", name, node.Name, want)
		}
	}
}

func TestJavaScriptCallPath(t *testing.T) {
	resolved, probs := Check(Tour{Version: 1, Steps: []Step{{Title: "p", Path: &Path{From: "src.run", To: "Cart.total"}}}}, jsScene(t), nil)
	if len(probs) > 0 {
		t.Fatalf("problems: %v", probs)
	}
	var names []string
	for _, n := range resolved.Steps[0].Targets.Path {
		names = append(names, n.Name)
	}
	if len(names) != 3 || !strings.HasSuffix(names[2], "Cart.total") || !strings.HasSuffix(names[1], "Cart.add") {
		t.Fatalf("path = %v", names)
	}
}
