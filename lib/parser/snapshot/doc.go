// Package snapshot pins the parsed index of one real-world codebase per
// supported language and compares it, byte for byte, with a golden stored in
// this directory.
//
// It records today's parser output so a change to the parser can be proved
// equivalent to it. It does not change the parser and it is not a unit test of
// any single declaration: every golden is the whole parsed snapshot of a whole
// checkout, run through the real pipeline — lib/files.Tree reads the tree and
// parser.New().Parse fills the entities, imports, calls and their resolved
// refs.
//
// # Corpora
//
// One public, permissively licensed checkout per language, each pinned to a
// commit:
//
//	tokio        d4a0daaed93b67cb6c6612d0d88428299e948202  rust        810 files
//	prometheus   6ba9ac65bf1c762d4376a508a6a4568c15855b09  go          737 files
//	swift-nio    48554ed8a998e9a23ad436e59d1703c69462278c  swift       549 files
//	eslint       48d5cc47e43478670ccf95a74f7a73e8408152b5  javascript 1453 files
//
// The file count is how many files of the corpus language the snapshot yields;
// a checkout may also hold a few files of another supported language and the
// snapshot records those too, exactly as the mixed pipeline reads them. Module
// manifests such as Cargo.toml, go.mod, Package.swift and package.json are read
// for module metadata and yield no file record of their own.
//
// # Clones
//
// Clones live outside the repository, one directory per corpus, under
// CITYDIFF_PARSER_CLONES (default ~/.cache/citydiff/parser-clones). The
// comparison never clones and never needs the network; it skips when a clone
// is absent. Fetch or update every clone to its pinned commit with:
//
//	CITYDIFF_PARSER_FETCH=1 go test ./lib/parser/snapshot -run TestFetchCorpus -v
//
// A clone is blobless and shallow: one commit is fetched, and the source blobs
// arrive lazily when the tree is checked out. Repos are fetched one at a time.
// An existing checkout is never deleted; it is fetched and checked out at the
// pinned commit, and a checkout with local changes fails rather than being
// forced.
//
// # Regenerating the goldens
//
// After a deliberate parser change, rewrite the goldens in testdata and review
// the diff:
//
//	CITYDIFF_PARSER_UPDATE=1 go test ./lib/parser/snapshot -run TestSnapshot -v
//
// go test -run TestSnapshot -update does the same thing.
//
// # What is compared
//
// The golden is gzipped JSON Lines with up to two sections. A section starts
// at a summary line — repo, pinned commit, language, its order, file, source,
// entity and call counts, and the sha256 of the bytes that follow — and
// continues with one JSON record per file and per entity until the next
// summary. A record holds every field the parser fills, including the ones the
// JSON API hides (BodyHash, BodyBytes, MethodsHash) and the source position,
// so the comparison is stricter than the wire format. File paths are relative
// to the checkout, so no absolute path, timestamp or cache location enters the
// golden. JSON field order is fixed by the structs and calls keep the parser's
// order.
//
// The first section is the canonical projection and is always compared. It
// orders files by path and each file's entities by kind, name, receiver, line,
// column and finally the whole record, so it does not depend on the order the
// parser happened to emit them in. The second section is written only for an
// order-stable language and pins the parser's own entity order, so a change to
// that order is a failure rather than something canonicalization would hide.
// A corpus is order-stable when its parser's order is a function of the
// source; TestSnapshotDeterminism measures that, and a corpus that is not
// order-stable is compared canonically only.
//
// # The JavaScript finding
//
// The JavaScript parser's raw entity order is not a function of the source.
// It appends entities that are synthesized while iterating maps — the loops
// over w.objects and w.owners in lib/parser/javascript/parser.go — so the
// order of those synthesized types depends on Go's randomized map iteration.
// The eslint corpus happened to be identical over twenty parses, but a
// four-line source with three assignment owners produced three distinct orders
// over three hundred parses, so JavaScript is treated as order-unstable and
// its golden pins the canonical projection only. This is a pre-existing parser
// behaviour, not something the snapshot hides by sorting, and it is not fixed
// on this branch.
//
// # Determinism
//
// CITYDIFF_PARSER_DETERMINISM=1 go test ./lib/parser/snapshot -run TestSnapshotDeterminism -v
//
// parses each corpus five times in one process (override the count with
// CITYDIFF_PARSER_DETERMINISM_RUNS) and fails when the canonical output, or an
// entity, call or file count, changes. For an order-stable corpus it also
// fails when the parser's own entity order changes; for JavaScript it records
// the change as the known finding above. A canonical hash that is identical
// across runs with identical counts is what says the JavaScript variation is
// order only and cannot change content.
//
// # Default test runs
//
// TestSnapshot skips when the clone is absent and in -short mode, so a plain
// go test ./... neither clones nor fails on a box without the corpus.
// TestFetchCorpus only runs under CITYDIFF_PARSER_FETCH=1.
// TestSnapshotDeterminism only runs under CITYDIFF_PARSER_DETERMINISM=1.
package snapshot
