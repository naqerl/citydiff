package snapshot

import (
	"os"
	"path/filepath"
)

// Corpus is one pinned real-world codebase.
type Corpus struct {
	// Name is the clone directory and the golden file name, without suffix.
	Name string
	// Language is the language the corpus is chosen for. A checkout may hold a
	// few files of another supported language; the snapshot records all of
	// them, exactly as the mixed pipeline reads them.
	Language string
	// Repo is the public git remote.
	Repo string
	// Commit is the pinned commit SHA, never a branch.
	Commit string
	// Files is the number of files of Language the snapshot yields (the
	// summary's sources field), so a reader knows the corpus size without
	// fetching it. A checkout may hold a few files of another supported
	// language, and those are recorded too; TestSnapshot reports the counts it
	// actually read in the golden summary.
	Files int
	// OrderStable is whether the parser emits entities in an order fixed by
	// the source, so the golden can also pin that order. It is measured, not
	// assumed: TestSnapshotDeterminism parses the corpus several times and
	// fails an order-stable corpus whose order changes. Go, Rust and Swift are
	// stable. JavaScript is not: the parser appends entities synthesized from
	// map iteration (lib/parser/javascript/parser.go, the loops over
	// w.objects and w.owners), so its raw order is not a function of the
	// source. See doc.go.
	OrderStable bool
}

// Corpora are the pinned codebases, one per supported language. Each is public
// and permissively licensed, has no submodule or LFS file under its source,
// and is small enough that a low-resource box can hold one clone at a time.
var Corpora = []Corpus{
	{
		Name:        "tokio",
		Language:    "rust",
		Repo:        "https://github.com/tokio-rs/tokio",
		Commit:      "d4a0daaed93b67cb6c6612d0d88428299e948202",
		Files:       810,
		OrderStable: true,
	},
	{
		Name:        "prometheus",
		Language:    "go",
		Repo:        "https://github.com/prometheus/prometheus",
		Commit:      "6ba9ac65bf1c762d4376a508a6a4568c15855b09",
		Files:       737,
		OrderStable: true,
	},
	{
		Name:        "swift-nio",
		Language:    "swift",
		Repo:        "https://github.com/apple/swift-nio",
		Commit:      "48554ed8a998e9a23ad436e59d1703c69462278c",
		Files:       549,
		OrderStable: true,
	},
	{
		Name:     "eslint",
		Language: "javascript",
		Repo:     "https://github.com/eslint/eslint",
		Commit:   "48d5cc47e43478670ccf95a74f7a73e8408152b5",
		Files:    1453,
		// JavaScript is compared canonically: its entity order is not stable.
		OrderStable: false,
	},
}

// ClonesRoot is the directory holding one checkout per corpus. It is
// CITYDIFF_PARSER_CLONES when set, ~/.cache/citydiff/parser-clones otherwise.
func ClonesRoot() string {
	if root := os.Getenv("CITYDIFF_PARSER_CLONES"); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "citydiff-parser-clones")
	}
	return filepath.Join(home, ".cache", "citydiff", "parser-clones")
}

// Dir is where the corpus is checked out.
func (c Corpus) Dir() string { return filepath.Join(ClonesRoot(), c.Name) }

// Golden is the committed golden for the corpus, relative to this package.
func (c Corpus) Golden() string { return filepath.Join("testdata", c.Name+".jsonl.gz") }

// FetchCommand fetches every corpus at its pinned commit.
func FetchCommand() string {
	return "CITYDIFF_PARSER_FETCH=1 go test ./lib/parser/snapshot -run TestFetchCorpus -v"
}

// UpdateCommand rewrites every golden from the local clones.
func UpdateCommand() string {
	return "CITYDIFF_PARSER_UPDATE=1 go test ./lib/parser/snapshot -run TestSnapshot -v"
}
