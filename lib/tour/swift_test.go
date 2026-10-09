package tour

import (
	"strings"
	"testing"

	"citydiff/lib"
	"citydiff/lib/parser"
	"citydiff/lib/scene"
)

const swiftWidget = `import Foundation

/// A widget.
public struct Widget {
    var size: Int
    public init(size: Int) { self.size = size }
    public init() { self.init(size: 1) }
    public func draw() {
        render()
    }
    func render() {}
}

extension Widget: Equatable {
    public static func == (l: Widget, r: Widget) -> Bool { l.size == r.size }
    func describe() -> String { "w" }
}
`

func swiftScene(t *testing.T) (scene.Scene, []byte) {
	t.Helper()
	files, err := parser.New().Parse(lib.Mem([]lib.File{
		{Path: "Package.swift", Src: []byte(`let package = Package(name: "kit", targets: [.target(name: "Core"), .target(name: "App")])`)},
		{Path: "Sources/Core/Widget.swift", Src: []byte(swiftWidget)},
		{Path: "Sources/App/main.swift", Src: []byte("import Core\nfunc run() { Widget().draw() }\n")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return scene.Build(nil, files), []byte(swiftWidget)
}

// A Swift declaration is named Module.Type.member. The scene files a
// conformance member (extension Widget: Equatable) under its type, as it
// does a Rust trait impl.
func TestResolveSwiftNames(t *testing.T) {
	sc, _ := swiftScene(t)
	ix := NewIndex(sc)
	for name, want := range map[string]string{
		"Core":             "Core",
		"Core.Widget":      "Widget",
		"Core.Widget.draw": "Core.Widget.draw",
		"Widget.draw":      "Core.Widget.draw",
		"App.run":          "App.run",
		"Core.Widget.==":   "Core.Widget.==",
		"Widget.describe":  "Core.Widget.describe",
		"widget.render":    "Core.Widget.render",
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
	if _, err := ix.Resolve("Widget.init"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("two inits should be ambiguous: %v", err)
	}
}

func TestSwiftCallPathAndSnippet(t *testing.T) {
	sc, src := swiftScene(t)
	resolved, probs := Check(Tour{Version: 1, Steps: []Step{{Title: "p", Path: &Path{From: "App.run", To: "Widget.render"}}}}, sc, nil)
	if len(probs) > 0 {
		t.Fatalf("problems: %v", probs)
	}
	var names []string
	for _, n := range resolved.Steps[0].Targets.Path {
		names = append(names, n.Name)
	}
	if strings.Join(names, " -> ") != "App.run -> Core.Widget.draw -> Core.Widget.render" {
		t.Fatalf("path = %v", names)
	}
	ix := NewIndex(sc)
	draw, _ := ix.Resolve("Widget.draw")
	if got := Snippet(src, draw, "Widget"); got != "    public func draw() {\n        render()\n    }" {
		t.Fatalf("draw snippet: %q", got)
	}
	eq, _ := ix.Resolve("Widget.==")
	if got := Snippet(src, eq, "<Widget as Equatable>"); !strings.Contains(got, "static func ==") {
		t.Fatalf("== snippet: %q", got)
	}
	typ, _ := ix.Resolve("Core.Widget")
	if got := Snippet(src, typ, ""); !strings.HasPrefix(got, "/// A widget.\npublic struct Widget {") || !strings.HasSuffix(got, "\n}") {
		t.Fatalf("type snippet: %q", got)
	}
}
