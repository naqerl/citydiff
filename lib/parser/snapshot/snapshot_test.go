package snapshot

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"citydiff/lib"
	"citydiff/lib/files"
	"citydiff/lib/parser"
)

// updateFlag is the command line form of CITYDIFF_PARSER_UPDATE=1. It rewrites
// the goldens from the local clones; review the diff before committing.
var updateFlag = flag.Bool("update", false, "rewrite the snapshot goldens from the local clones")

// TestFetchCorpus clones or updates every corpus at its pinned commit. It
// touches the network, so it only runs under CITYDIFF_PARSER_FETCH=1. Repos are
// fetched one at a time and an existing checkout is never deleted.
func TestFetchCorpus(t *testing.T) {
	if os.Getenv("CITYDIFF_PARSER_FETCH") != "1" {
		t.Skip("set CITYDIFF_PARSER_FETCH=1 to clone or update the corpus")
	}
	for _, c := range Corpora {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			fetchCorpus(t, c)
			t.Logf("%s: %s at %s", c.Name, c.Dir(), c.Commit)
		})
	}
}

// TestSnapshot parses every corpus through the real pipeline and compares the
// canonical output, and for an order-stable corpus also the parser's own
// entity order, with the committed golden. It skips, with the command that
// fixes it, when a clone is absent, and in -short mode.
func TestSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("snapshot test skipped in -short mode")
	}
	update := *updateFlag || os.Getenv("CITYDIFF_PARSER_UPDATE") == "1"
	for _, c := range Corpora {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			if _, err := os.Stat(c.Dir()); err != nil {
				t.Skipf("clone missing: %s\n  fetch with: %s", c.Dir(), FetchCommand())
			}
			parsed, err := parseCorpus(c)
			if err != nil {
				t.Fatalf("parse %s: %v", c.Dir(), err)
			}
			got := buildForms(c, parsed)
			if update {
				content := goldenBytes(c, got)
				if err := writeGolden(c.Golden(), content); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: wrote %s (%d files, %d sources, %d entities, %d calls; %s decompressed, %s on disk)",
					c.Name, c.Golden(), got.canonical.header.Files, got.canonical.header.Sources,
					got.canonical.header.Entities, got.canonical.header.Calls,
					size(len(content)), size(fileSize(c.Golden())))
				return
			}
			want, err := readGolden(c.Golden())
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					t.Fatalf("golden %s missing\n  regenerate with: %s", c.Golden(), UpdateCommand())
				}
				t.Fatalf("read %s: %v", c.Golden(), err)
			}
			compareGolden(t, c, want, got)
		})
	}
}

// compareGolden checks the live parse against the golden. The canonical
// projection is always compared. A corpus whose parser order is a function of
// the source additionally pins that order, so a change to it is a failure
// rather than something canonicalization would hide.
func compareGolden(t *testing.T, c Corpus, want []section, got forms) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("golden %s is empty\n  regenerate with: %s", c.Golden(), UpdateCommand())
	}
	if !bytes.Equal(want[0].payload, got.canonical.payload) {
		t.Errorf("canonical snapshot differs from %s\n%s", c.Golden(), diffReport("canonical", want[0].payload, got.canonical.payload))
	} else if !bytes.Equal(want[0].line, got.canonical.line) {
		t.Errorf("summary line differs from %s\n  want %s\n   got %s", c.Golden(), clip(want[0].line), clip(got.canonical.line))
	}
	if !c.OrderStable {
		if len(want) > 1 {
			t.Errorf("%s has a raw-order section but %s is not order-stable\n  regenerate with: %s", c.Golden(), c.Language, UpdateCommand())
		}
		return
	}
	if len(want) < 2 {
		t.Fatalf("golden %s has no raw-order section for order-stable %s\n  regenerate with: %s", c.Golden(), c.Language, UpdateCommand())
	}
	if !bytes.Equal(want[1].payload, got.source.payload) {
		t.Errorf("parser entity order differs from %s\n%s", c.Golden(), diffReport("raw order", want[1].payload, got.source.payload))
	}
}

// TestSnapshotDeterminism parses every corpus several times in one process and
// fails when the canonical output changes. It is the check that the golden is
// a function of the source and not of map order or timing. It reports whether
// the parser's own entity order changed, failing for an order-stable corpus
// and recording it as a known finding for JavaScript. It only runs under
// CITYDIFF_PARSER_DETERMINISM=1 because it multiplies the parse cost;
// CITYDIFF_PARSER_DETERMINISM_RUNS overrides the five runs.
func TestSnapshotDeterminism(t *testing.T) {
	if os.Getenv("CITYDIFF_PARSER_DETERMINISM") != "1" {
		t.Skip("set CITYDIFF_PARSER_DETERMINISM=1 to parse each corpus several times")
	}
	runs := 5
	if v := os.Getenv("CITYDIFF_PARSER_DETERMINISM_RUNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 {
			t.Fatalf("CITYDIFF_PARSER_DETERMINISM_RUNS=%q is not an integer of at least 2", v)
		}
		runs = n
	}
	for _, c := range Corpora {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			if _, err := os.Stat(c.Dir()); err != nil {
				t.Skipf("clone missing: %s\n  fetch with: %s", c.Dir(), FetchCommand())
			}
			var first, last forms
			rawStable, canonicalStable, countsStable := true, true, true
			var firstCanonical, firstRaw string
			for i := 0; i < runs; i++ {
				parsed, err := parseCorpus(c)
				if err != nil {
					t.Fatalf("parse %s: %v", c.Dir(), err)
				}
				run := buildForms(c, parsed)
				parsed = nil
				if i == 0 {
					first = run
					firstCanonical = hash8(run.canonical.payload)
					firstRaw = hash8(run.source.payload)
					last = run
					continue
				}
				last = run
				if !bytes.Equal(run.canonical.payload, first.canonical.payload) {
					canonicalStable = false
				}
				if !bytes.Equal(run.source.payload, first.source.payload) {
					rawStable = false
				}
				if run.canonical.header.Entities != first.canonical.header.Entities ||
					run.canonical.header.Calls != first.canonical.header.Calls ||
					run.canonical.header.Files != first.canonical.header.Files {
					countsStable = false
				}
			}
			t.Logf("%s: %d runs, files=%d entities=%d calls=%d; canonicalStable=%v countsStable=%v rawOrderStable=%v; canonical=%s raw=%s",
				c.Name, runs, first.canonical.header.Files, first.canonical.header.Entities,
				first.canonical.header.Calls, canonicalStable, countsStable, rawStable, firstCanonical, firstRaw)
			if !canonicalStable {
				t.Errorf("canonical output is not stable across %d parses of %s\n%s",
					runs, c.Name, diffReport("canonical", first.canonical.payload, last.canonical.payload))
			}
			if !countsStable {
				t.Errorf("entity, call or file counts are not stable across %d parses of %s", runs, c.Name)
			}
			if c.OrderStable && !rawStable {
				t.Errorf("parser entity order is not stable across %d parses of %s\n%s",
					runs, c.Name, diffReport("raw order", first.source.payload, last.source.payload))
			}
			if !c.OrderStable && !rawStable {
				t.Logf("%s: known finding, not a failure: the %s parser's raw entity order is not a function of the source (see doc.go)", c.Name, c.Language)
			}
		})
	}
}

// parseCorpus reads the checkout with lib/files and parses it with the
// language-agnostic entry point.
func parseCorpus(c Corpus) ([]lib.ParsedFile, error) {
	src, err := files.Tree(c.Dir())
	if err != nil {
		return nil, err
	}
	return parser.New().Parse(src)
}

// forms are the two serializations of one parse: the canonical projection the
// golden always pins, and the parser's own order.
type forms struct {
	canonical section
	source    section
}

// section is one header line and the records it summarizes.
type section struct {
	header  recordHeader
	line    []byte
	payload []byte
}

// buildForms serializes one parse both ways. The source section keeps the
// parser's order, canonical sorts files by path and entities into a total
// order, so the two differ only where the parser's order is not canonical.
func buildForms(c Corpus, parsed []lib.ParsedFile) forms {
	return forms{
		canonical: encodeSection(c, canonicalize(parsed), "canonical"),
		source:    encodeSection(c, parsed, "source"),
	}
}

type recordHeader struct {
	Snapshot string `json:"snapshot"`
	Version  int    `json:"version"`
	Repo     string `json:"repo"`
	Commit   string `json:"commit"`
	Language string `json:"language"`
	// Order is "canonical" or "source": the order of this section's records.
	Order string `json:"order"`
	// RawOrder is set on the canonical section of an order-stable corpus: the
	// golden then also pins the parser's own entity order.
	RawOrder bool   `json:"rawOrder,omitempty"`
	Files    int    `json:"files"`
	Sources  int    `json:"sources"`
	Entities int    `json:"entities"`
	Calls    int    `json:"calls"`
	Hash     string `json:"hash"`
}

type fileRecord struct {
	Record     string `json:"record"`
	Path       string `json:"path"`
	Package    string `json:"package,omitempty"`
	ImportPath string `json:"importPath,omitempty"`
	Module     string `json:"module,omitempty"`
}

type entityRecord struct {
	Record      string        `json:"record"`
	Kind        string        `json:"kind"`
	Name        string        `json:"name,omitempty"`
	Line        int           `json:"line,omitempty"`
	Column      int           `json:"column,omitempty"`
	Fields      []string      `json:"fields,omitempty"`
	Parameters  []paramRecord `json:"parameters,omitempty"`
	ReturnArgs  []paramRecord `json:"returnArgs,omitempty"`
	Calls       []callRecord  `json:"calls,omitempty"`
	BodyHash    string        `json:"bodyHash,omitempty"`
	BodyBytes   int           `json:"bodyBytes,omitempty"`
	MethodsHash string        `json:"methodsHash,omitempty"`
	Recv        string        `json:"recv,omitempty"`
}

type paramRecord struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

type callRecord struct {
	Expr string     `json:"expr"`
	Ref  *refRecord `json:"ref,omitempty"`
}

type refRecord struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Recv string `json:"recv,omitempty"`
}

// encodeSection writes one section: a summary line, then one JSON record per
// file and per entity in the given order. Field order is the struct order,
// nothing is a map, and every line ends in LF. The header hash covers the
// payload that follows it, and the header's counts cover the same records.
func encodeSection(c Corpus, parsed []lib.ParsedFile, order string) section {
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	enc.SetEscapeHTML(false)
	filesN, sourcesN, entitiesN, callsN := 0, 0, 0, 0
	lang := languageOf(c.Language)
	for _, f := range parsed {
		filesN++
		if lang != nil && lang.Include(path.Base(f.Path)) {
			sourcesN++
		}
		encode(enc, fileRecord{
			Record:     "file",
			Path:       f.Path,
			Package:    f.Package,
			ImportPath: f.ImportPath,
			Module:     f.Module,
		})
		for _, e := range f.Entities {
			entitiesN++
			rec := entityOf(e)
			callsN += len(rec.Calls)
			encode(enc, rec)
		}
	}
	sum := sha256.Sum256(payload.Bytes())
	header := recordHeader{
		Snapshot: "citydiff/parser",
		Version:  1,
		Repo:     c.Repo,
		Commit:   c.Commit,
		Language: c.Language,
		Order:    order,
		RawOrder: order == "canonical" && c.OrderStable,
		Files:    filesN,
		Sources:  sourcesN,
		Entities: entitiesN,
		Calls:    callsN,
		Hash:     "sha256:" + hex.EncodeToString(sum[:]),
	}
	line, err := json.Marshal(header)
	if err != nil {
		panic(err)
	}
	return section{header: header, line: line, payload: payload.Bytes()}
}

// canonicalize copies parsed with files ordered by path and each file's
// entities in a total order independent of how the parser emitted them. The
// total order is kind, name, receiver, line, column, then the whole record, so
// two entities compare equal only when their serializations are equal.
func canonicalize(parsed []lib.ParsedFile) []lib.ParsedFile {
	out := make([]lib.ParsedFile, len(parsed))
	for i, f := range parsed {
		type decorated struct {
			entity lib.Entity
			record entityRecord
			raw    []byte
		}
		entities := make([]decorated, len(f.Entities))
		for k, e := range f.Entities {
			rec := entityOf(e)
			raw, err := json.Marshal(rec)
			if err != nil {
				panic(err)
			}
			entities[k] = decorated{entity: e, record: rec, raw: raw}
		}
		sort.Slice(entities, func(a, b int) bool {
			return lessRecord(entities[a].record, entities[b].record, entities[a].raw, entities[b].raw)
		})
		sorted := make([]lib.Entity, len(entities))
		for k := range entities {
			sorted[k] = entities[k].entity
		}
		f.Entities = sorted
		out[i] = f
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func lessRecord(a, b entityRecord, aRaw, bRaw []byte) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Recv != b.Recv {
		return a.Recv < b.Recv
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Column != b.Column {
		return a.Column < b.Column
	}
	return bytes.Compare(aRaw, bRaw) < 0
}

// languageOf is the parser language with this name, nil when there is none.
func languageOf(name string) *parser.Language {
	for i := range parser.Languages {
		if parser.Languages[i].Name == name {
			return &parser.Languages[i]
		}
	}
	return nil
}

// entityOf maps one entity to a record with every field the parser filled.
func entityOf(e lib.Entity) entityRecord {
	rec := entityRecord{Record: "entity", Kind: e.Kind().String()}
	p := lib.PosOf(e)
	rec.Line, rec.Column = p.Line, p.Column
	switch x := e.(type) {
	case lib.ImportEntry:
		rec.Name = x.Path
	case lib.TypeEntry:
		rec.Name = x.Name
		for _, f := range x.Fields {
			rec.Fields = append(rec.Fields, f.Name)
		}
		rec.MethodsHash = x.MethodsHash
	case lib.VariableEntry:
		rec.Name = x.Name
	case lib.FunctionEntry:
		fillFunction(&rec, x)
	case lib.MethodEntry:
		fillFunction(&rec, x.FunctionEntry)
		if x.Type != nil {
			rec.Recv = x.Type.Name
		}
	}
	return rec
}

func fillFunction(rec *entityRecord, f lib.FunctionEntry) {
	rec.Name = f.Name
	for _, p := range f.Parameters {
		rec.Parameters = append(rec.Parameters, paramRecord{Name: p.Name, Type: p.Type})
	}
	for _, p := range f.ReturnArgs {
		rec.ReturnArgs = append(rec.ReturnArgs, paramRecord{Name: p.Name, Type: p.Type})
	}
	for _, call := range f.Calls {
		c := callRecord{Expr: call.Expr}
		if call.Ref != nil {
			c.Ref = &refRecord{Path: call.Ref.Path, Name: call.Ref.Name, Recv: call.Ref.Recv}
		}
		rec.Calls = append(rec.Calls, c)
	}
	rec.BodyHash = f.BodyHash
	rec.BodyBytes = f.BodyBytes
}

// goldenBytes lays out the golden: the canonical section always, then the
// source-order section for a corpus whose parser order is stable. A section
// starts at a line whose JSON begins with {"snapshot":.
func goldenBytes(c Corpus, f forms) []byte {
	out := make([]byte, 0, len(f.canonical.line)+len(f.canonical.payload)+len(f.source.line)+len(f.source.payload)+2)
	out = append(out, f.canonical.line...)
	out = append(out, '\n')
	out = append(out, f.canonical.payload...)
	if c.OrderStable {
		out = append(out, f.source.line...)
		out = append(out, '\n')
		out = append(out, f.source.payload...)
	}
	return out
}

// readGolden reads the gzipped golden and splits it into its sections.
func readGolden(name string) ([]section, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	content, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return splitSections(content)
}

// gzipSectionPrefix is the start of every header line. A record line never
// starts with it, so it is what separates one section from the next.
const gzipSectionPrefix = `{"snapshot":`

// splitSections splits a decompressed golden into its header+payload sections.
func splitSections(content []byte) ([]section, error) {
	var out []section
	var current *section
	for len(content) > 0 {
		line := content
		if i := bytes.IndexByte(content, '\n'); i >= 0 {
			line, content = content[:i], content[i+1:]
		} else {
			content = nil
		}
		if bytes.HasPrefix(line, []byte(gzipSectionPrefix)) {
			var header recordHeader
			if err := json.Unmarshal(line, &header); err != nil {
				return nil, fmt.Errorf("golden header is not JSON: %w", err)
			}
			out = append(out, section{header: header, line: append([]byte(nil), line...)})
			current = &out[len(out)-1]
			continue
		}
		if current == nil || len(line) == 0 {
			continue
		}
		current.payload = append(current.payload, line...)
		current.payload = append(current.payload, '\n')
	}
	return out, nil
}

// writeGolden writes the golden as gzip with a fixed header, so regenerating
// an unchanged snapshot produces the same bytes.
func writeGolden(name string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(content); err != nil {
		return err
	}
	return zw.Close()
}

// diffReport names the first differing record and shows a bounded context.
// label says which serialization differed, because the canonical one can match
// while the parser's own order does not.
func diffReport(label string, want, got []byte) string {
	wl := bytes.Split(want, []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	i := 0
	for i < len(wl) && i < len(gl) && bytes.Equal(wl[i], gl[i]) {
		i++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: first difference at record %d of %d (golden %d):\n", label, i+1, len(gl), len(wl))
	from, to := i-1, i+2
	if from < 0 {
		from = 0
	}
	for n := from; n < to; n++ {
		if n < len(wl) {
			fmt.Fprintf(&b, "  golden %d: %s\n", n+1, clip(wl[n]))
		}
		if n < len(gl) {
			fmt.Fprintf(&b, "     got %d: %s\n", n+1, clip(gl[n]))
		}
	}
	return b.String()
}

const clipWidth = 320

func clip(b []byte) string {
	s := string(b)
	if len(s) > clipWidth {
		return s[:clipWidth] + fmt.Sprintf("... (%d bytes)", len(s))
	}
	return s
}

func encode(enc *json.Encoder, v any) {
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
}

func hash8(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func fileSize(name string) int {
	info, err := os.Stat(name)
	if err != nil {
		return 0
	}
	return int(info.Size())
}

// fetchCorpus brings dir to c.Commit. It initializes a blobless, shallow
// checkout when dir is absent and fetches and checks out c.Commit otherwise. It
// never deletes or resets an existing checkout; local changes make it fail.
func fetchCorpus(t *testing.T, c Corpus) {
	t.Helper()
	dir := c.Dir()
	switch _, err := os.Stat(dir); {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "init", "-q")
		git(t, dir, "remote", "add", "origin", c.Repo)
		git(t, dir, "config", "remote.origin.promisor", "true")
		git(t, dir, "config", "remote.origin.partialclonefilter", "blob:none")
	case err != nil:
		t.Fatal(err)
	default:
		if _, err := gitTry(dir, "rev-parse", "--git-dir"); err != nil {
			t.Fatalf("%s exists but is not a git checkout: %v", dir, err)
		}
	}
	if head, err := gitTry(dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(head) == c.Commit {
		t.Logf("%s: already at %s", c.Name, c.Commit)
		return
	}
	if _, err := gitTry(dir, "cat-file", "-e", c.Commit+"^{commit}"); err != nil {
		git(t, dir, "fetch", "--depth", "1", "--filter=blob:none", "origin", c.Commit)
	}
	git(t, dir, "checkout", "--detach", c.Commit)
	head := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	if head != c.Commit {
		t.Fatalf("%s: checked out %s, want %s", c.Name, head, c.Commit)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return out
}

func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
