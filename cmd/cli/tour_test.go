package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"citydiff/lib/tour"
)

func TestParseArgsMixesFlagsAndFiles(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	r := fs.String("range", "", "")
	j := fs.Bool("json", false, "")
	rest, err := parseArgs(fs, []string{"a.json", "-range", "x..y", "b.json", "-json"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rest, ",") != "a.json,b.json" || *r != "x..y" || !*j {
		t.Fatalf("rest=%v range=%q json=%v", rest, *r, *j)
	}
}

func TestTourSchemaPrintsTheEmbeddedSchema(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runTour([]string{"schema"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), tour.Schema) {
		t.Fatal("schema output differs from the embedded schema")
	}
}

func TestNodesListsThisRepository(t *testing.T) {
	var out bytes.Buffer
	if err := runNodes([]string{"-path", "../..", "-json", "-kind", "function"}, &out); err != nil {
		t.Fatal(err)
	}
	var nodes []tour.Node
	if err := json.Unmarshal(out.Bytes(), &nodes); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range nodes {
		if n.Kind != "function" {
			t.Fatalf("kind filter let through %+v", n)
		}
		if strings.HasSuffix(n.Name, "cmd/cli.parseArgs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("parseArgs not listed among %d functions", len(nodes))
	}
}

func TestTourValidateReportsProblems(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "t.json")
	if err := os.WriteFile(file, []byte(`{"version":1,"steps":[{"title":"a","focus":"parseArgz"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := runTour([]string{"validate", "-path", "../..", file}, &out, &errOut)
	if err == nil || !strings.Contains(errOut.String(), `"parseArgz" matches no node; did you mean`) || !strings.Contains(errOut.String(), "parseArgs") {
		t.Fatalf("err=%v stderr=%s", err, errOut.String())
	}
}

func TestExampleTourValidatesAgainstBarse(t *testing.T) {
	barse := os.Getenv("CITYDIFF_BARSE")
	if barse == "" {
		barse = "/workspace/barse-scene"
	}
	if _, err := os.Stat(filepath.Join(barse, ".git")); err != nil {
		t.Skip("no barse checkout")
	}
	var out, errOut bytes.Buffer
	if err := runTour([]string{"validate", "-path", barse, "../../examples/barse-flashcard-versions.tour.json"}, &out, &errOut); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "ok: ") {
		t.Fatalf("output: %s", out.String())
	}
}
