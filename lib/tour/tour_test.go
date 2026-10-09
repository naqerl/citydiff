package tour

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"citydiff/lib/scene"
)

func fixture() scene.Scene {
	return scene.Scene{Module: "m", Root: "m", Diff: true, Packages: []scene.Package{
		{ID: "m", Name: "m", Change: "same", Synthetic: true},
		{ID: "m/gen", Name: "gen", Parent: "m", Change: "modified", Entities: []scene.Entity{
			{ID: "gen/g.go#type#Generator", Kind: "type", Name: "Generator", File: "gen/g.go", Change: "modified"},
			{ID: "gen/g.go#method#Generator#Run", Kind: "method", Name: "Run", Recv: "Generator", File: "gen/g.go", Change: "modified",
				Calls: []scene.CallStep{{Change: "added", Expr: "save", Path: "db/db.go", Name: "Save", Resolved: true}}},
			{ID: "gen/g.go#function#New", Kind: "function", Name: "New", File: "gen/g.go", Change: "added",
				Calls: []scene.CallStep{{Change: "same", Expr: "g.Run", Path: "gen/g.go", Recv: "Generator", Name: "Run", Resolved: true}}},
		}},
		{ID: "m/db", Name: "db", Parent: "m", Change: "modified", Entities: []scene.Entity{
			{ID: "db/db.go#function#Save", Kind: "function", Name: "Save", File: "db/db.go", Change: "added"},
			{ID: "db/db.go#function#New", Kind: "function", Name: "New", File: "db/db.go", Change: "same"},
			{ID: "db/db.go#function#Lone", Kind: "function", Name: "Lone", File: "db/db.go", Change: "same"},
		}},
		{ID: "crate::store", Name: "store", Change: "modified", Entities: []scene.Entity{
			{ID: "src/store.rs#type#Entry", Kind: "type", Name: "Entry", File: "src/store.rs", Change: "same"},
			{ID: "src/store.rs#method#<Entry as Display>#fmt", Kind: "method", Name: "fmt", Recv: "<Entry as Display>", File: "src/store.rs", Change: "added"},
			{ID: "src/store.rs#method#<Entry as fmt::Debug>#dbg", Kind: "method", Name: "dbg", Recv: "<Entry as fmt::Debug>", File: "src/store.rs", Change: "same"},
		}},
		{ID: "fmt", Name: "fmt", External: true, Change: "same"},
	}}
}

func TestParseRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	if _, err := Parse(strings.NewReader(`{"version":1,"steps":[{"title":"a","foucs":"x"}]}`)); err == nil || !strings.Contains(err.Error(), "foucs") {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := Parse(strings.NewReader(`{"version":1,"steps":[]} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
	got, err := Parse(strings.NewReader(`{"version":1,"title":"T","range":"a..b","steps":[{"title":"s","path":{"from":"a","to":"b"},"dim":false,"zoom":0.5}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "T" || got.Steps[0].Path.To != "b" || *got.Steps[0].Dim || got.Steps[0].Zoom != 0.5 {
		t.Fatalf("parsed %+v", got)
	}
}

func TestSchemaIsJSONAndListsEveryStepField(t *testing.T) {
	var doc struct {
		Defs struct {
			Step struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"step"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(Schema, &doc); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Step{Path: &Path{}, Dim: new(bool), Highlight: []string{"x"}, Title: "t", Note: "n", Duration: 1, Mode: "m", Select: "s", Focus: "f", Camera: "c", Zoom: 1, Code: "c"})
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for name := range fields {
		if _, ok := doc.Defs.Step.Properties[name]; !ok {
			t.Errorf("schema has no step property %q", name)
		}
	}
	if len(fields) != len(doc.Defs.Step.Properties) {
		t.Errorf("step has %d fields, schema %d", len(fields), len(doc.Defs.Step.Properties))
	}
}

func TestSplitKeepsAngleBrackets(t *testing.T) {
	got := split("crate::store::<Entry as fmt::Debug>::dbg")
	want := []string{"crate", "store", "<Entry as fmt::Debug>", "dbg"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("split = %q", got)
	}
	if got := split("m/gen.Generator.Run"); strings.Join(got, "|") != "m|gen|Generator|Run" {
		t.Fatalf("split = %q", got)
	}
}

func TestResolveNames(t *testing.T) {
	ix := NewIndex(fixture())
	cases := map[string]string{
		"m/gen":                                 "m/gen",
		"gen":                                   "m/gen",
		"m/gen.Generator.Run":                   "gen/g.go#method#Generator#Run",
		"Generator.Run":                         "gen/g.go#method#Generator#Run",
		"Run":                                   "gen/g.go#method#Generator#Run",
		"gen.New":                               "gen/g.go#function#New",
		"db.New":                                "db/db.go#function#New",
		"gen/g.go#function#New":                 "gen/g.go#function#New",
		"<Entry as Display>::fmt":               "src/store.rs#method#<Entry as Display>#fmt",
		"crate::store::<Entry as Display>::fmt": "src/store.rs#method#<Entry as Display>#fmt",
		"store::<Entry as fmt::Debug>::dbg":     "src/store.rs#method#<Entry as fmt::Debug>#dbg",
		"Entry::fmt":                            "src/store.rs#method#<Entry as Display>#fmt",
		"generator.run":                         "gen/g.go#method#Generator#Run",
		"fmt":                                   "fmt",
		"generator.RUN":                         "gen/g.go#method#Generator#Run",
		"GEN.generator.run":                     "gen/g.go#method#Generator#Run",
	}
	cases["Generator.Missing"] = ""
	cases["db.Run"] = ""
	for name, want := range cases {
		node, err := ix.Resolve(name)
		if want == "" {
			if err == nil {
				t.Errorf("%q resolved to %s, want an error", name, node.ID)
			}
			continue
		}
		if err != nil || node.ID != want {
			t.Errorf("%q = %q, %v; want %s", name, node.ID, err, want)
		}
	}
}

func TestResolveExplainsFailures(t *testing.T) {
	ix := NewIndex(fixture())
	var e *Error
	_, err := ix.Resolve("New")
	if !errors.As(err, &e) || e.Reason != "ambiguous" || e.Total != 2 || !strings.Contains(err.Error(), "m/db.New") || !strings.Contains(err.Error(), "m/gen.New") {
		t.Fatalf("ambiguous: %v", err)
	}
	_, err = ix.Resolve("Generater.Run")
	if !errors.As(err, &e) || e.Reason != "unknown" {
		t.Fatalf("unknown: %v", err)
	}
	_, err = ix.Resolve("Sav")
	if !errors.As(err, &e) || len(e.Suggestions) == 0 || e.Suggestions[0].ID != "db/db.go#function#Save" {
		t.Fatalf("suggestions: %v", err)
	}
	_, err = ix.Resolve("gen.New", KindPackage)
	if !errors.As(err, &e) || e.Reason != "kind" || !strings.Contains(err.Error(), "is a function") {
		t.Fatalf("kind: %v", err)
	}
	if node, err := ix.Resolve("New", KindFunction); err == nil {
		t.Fatalf("New as function resolved to %s", node.ID)
	}
}

func TestCheckResolvesAStep(t *testing.T) {
	tr := Tour{Version: 1, Range: "a..b", Steps: []Step{
		{Title: "one", Mode: "changes", Select: "gen"},
		{Title: "two", Path: &Path{From: "gen.New", To: "Save"}, Highlight: []string{"db"}, Code: "Generator.Run", Duration: 3},
		{Title: "three", Focus: "<Entry as Display>::fmt"},
	}}
	res, probs := Check(tr, fixture(), func(string) error { return nil })
	if len(probs) > 0 {
		t.Fatalf("problems: %v", probs)
	}
	if res.Steps[0].Targets.Select.ID != "m/gen" || res.Steps[0].Duration != DefaultDuration || !res.Steps[0].Dim {
		t.Fatalf("step 1: %+v", res.Steps[0])
	}
	var ids []string
	for _, n := range res.Steps[1].Targets.Path {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, " ") != "gen/g.go#function#New gen/g.go#method#Generator#Run db/db.go#function#Save" {
		t.Fatalf("path = %v", ids)
	}
	if res.Steps[1].Duration != 3 || res.Steps[1].Targets.Code.Name != "m/gen.Generator.Run" || res.Steps[1].Targets.Highlight[0].ID != "m/db" {
		t.Fatalf("step 2: %+v", res.Steps[1])
	}
	if res.Steps[2].Targets.Focus.Kind != KindMethod {
		t.Fatalf("step 3: %+v", res.Steps[2])
	}
}

func TestCheckReportsEveryProblem(t *testing.T) {
	tr := Tour{Version: 2, Range: "x..y", Steps: []Step{
		{Mode: "diff", Camera: "side", Duration: -1, Zoom: -1},
		{Title: "t", Select: "gen.New", Focus: "Nope"},
		{Title: "p", Path: &Path{From: "Save", To: "Lone"}},
	}}
	_, probs := Check(tr, fixture(), func(string) error { return errors.New("other range") })
	var got []string
	for _, p := range probs {
		got = append(got, p.String())
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"version: is 2", "range: other range",
		"step 1 title: is required", "step 1 duration", "step 1 mode", "step 1 camera", "step 1 zoom",
		"step 2 focus: select and focus", "step 2 select: \"gen.New\" is a function", "step 2 focus: \"Nope\" matches no node",
		"step 3 path: no resolved call path",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if _, probs := Check(Tour{Version: 1}, fixture(), nil); len(probs) != 1 || probs[0].Field != "steps" {
		t.Fatalf("empty tour: %v", probs)
	}
}

func TestSnippetCutsTheDeclaration(t *testing.T) {
	src := []byte(`package gen

// Run runs.
func (g *Generator) Run(x int) error {
	s := "}"
	if x > 0 { // }
		return nil
	}
	return nil
}

func New() *Generator { return &Generator{} }

type Generator struct {
	a int
}
`)
	run := Snippet(src, Node{Name: "m/gen.Generator.Run", Kind: KindMethod, File: "gen/g.go"}, "*Generator")
	if !strings.HasPrefix(run, "// Run runs.\nfunc (g *Generator) Run") || !strings.HasSuffix(run, "\treturn nil\n}") {
		t.Fatalf("method snippet:\n%s", run)
	}
	if got := Snippet(src, Node{Name: "m/gen.New", Kind: KindFunction, File: "gen/g.go"}, ""); got != "func New() *Generator { return &Generator{} }" {
		t.Fatalf("function snippet: %q", got)
	}
	if got := Snippet(src, Node{Name: "m/gen.Generator", Kind: KindType, File: "gen/g.go"}, ""); got != "type Generator struct {\n\ta int\n}" {
		t.Fatalf("type snippet: %q", got)
	}
	rs := []byte("impl fmt::Display for Entry {\n    fn fmt(&self, f: &mut Formatter<'_>) -> Result {\n        write!(f, \"{}\", self.0)\n    }\n}\n")
	if got := Snippet(rs, Node{Name: "crate::store::<Entry as Display>::fmt", Kind: KindMethod, File: "src/store.rs"}, "<Entry as Display>"); !strings.HasPrefix(got, "    fn fmt") || !strings.HasSuffix(got, "    }") {
		t.Fatalf("rust snippet: %q", got)
	}
}
