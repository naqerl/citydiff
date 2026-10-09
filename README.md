# citydiff

**A 3D diff for Go and Rust.** Read one change from the outside inward, at three levels:

1. **Cross-module dependencies** — which packages gained or lost an import edge.
2. **Entity relationships** — which declarations changed, and how.
3. **Call paths** — which calls a function or method gained, lost, or kept, in source order.

The outer level *places* the change. The inner level shows what the code *does* differently.
All three are views of the same diff.

```
        overview                    changes                      a function
   ┌───────────────┐           ┌───────────────┐           ┌───────────────┐
   │  ▢   ▢   ▢    │           │  ▣   ▢   ▣    │           │  ▢   ▣   ▢    │
   │ ▢  ▣ ▢  ▢  ▢  │  ───────▶ │ ▣  ▣ ▢  ▢  ▣  │  ───────▶ │ ▢  ▣ ▢  ▢  ▢  │
   │▢ ▢  ▢ ▢  ▢ ▢  │           │▢ ▣  ▢ ▣  ▢ ▣  │           │▢ ▢ ╱▣ ╲▢ ▢ ▢  │
   └───────────────┘           └───────────────┘           └───────────────┘
      the city,                   the diff laid                arcs: what it
      unchanged                   on top of it                 calls, in order
```

The city is the code. Packages are buildings; a package's sub-packages stand on its roof.
A **type** is a wide pale block holding its methods. A **function** or **method** is a tower
whose **height is its body size**. A **call** is one smooth arc from the top of the caller to
the top of what it calls. Colour marks change: green is added, red is deleted, yellow is
modified; arcs that stayed are cyan.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/naqerl/citydiff/main/install.sh | sh
```

The installer resolves the latest [release](https://github.com/naqerl/citydiff/releases),
verifies its published `sha256`, installs the binary to `~/.local/bin`, and writes an agent
skill to `~/.agents/skills/citydiff`. It also supports env vars and flags:

```sh
# a specific tag
curl -fsSL https://raw.githubusercontent.com/naqerl/citydiff/main/install.sh | sh -s -- --tag=v0.0.2

# choose where things go
BIN_DIR=~/.local/bin SKILLS_DIR=~/.agents/skills AGENTS_MD=~/AGENTS.md \
  sh install.sh

# skip the agent skill / don't touch PATH
sh install.sh --no-skills --no-path

# remove binary, skill and AGENTS.md pointer
sh install.sh --uninstall
```

## Quick start

Parse a file or a project tree (no diff — one snapshot):

```sh
citydiff -path ./service/flashcard/flashcard.go -json
citydiff -path . -json | jq '.[] | {path, decls: (.entries | length)}'
```

Diff a commit range inside a git repository:

```sh
cd ~/src/myrepo
citydiff -path . -range main~5..main            # human-readable
citydiff -path . -range main~5..main -json      # machine-readable
citydiff -path . -range v0.0.1...v0.0.2         # three-dot: merge-base vs right
```

Serve the 3D viewer (`-view` never exits — run it in a terminal or in the background):

```sh
citydiff -path ~/src/myrepo -range main~5..main -view -addr 0.0.0.0:8787
```

then open `http://localhost:8787`. The page reads the scene from `GET /scene.json`; the
viewer assets are embedded in the binary.

## Command line

```
citydiff [-json | -scene | -view] -path file-or-directory [-range A..B]
```

| Flag | Meaning |
| --- | --- |
| `-path`, `-p` | a Go or Rust file, or a directory tree to walk. Required. |
| `-range`, `-r` | `A..B` compares those two commits; `A...B` compares their merge base with `B` (how `git diff` treats a three-dot range). An empty side means `HEAD`. |
| `-json` | print machine-readable entries instead of text. |
| `-scene` | print the scene graph the viewer draws, instead of text. |
| `-view` | serve the 3D viewer over HTTP (long-running). |
| `-addr` | listen address for `-view` (default `127.0.0.1:8787`). |

Notes:

- With `-range`, `-path` must live inside a **git checkout**: the nearest parent directory
  containing `.git` is the repository, and the tree at each ref is read straight from the
  object database with [go-git](https://github.com/go-git/go-git) — the working tree does not
  need to be checked out at either ref.
- Without `-range`, the filesystem tree is parsed and there is no diff. The viewer says so.
- Only Go and Rust source files are parsed. A commit that touches only other files (`*.js`,
  `*.sql`, …) produces an empty diff.
- `-json`, `-scene` and `-view` are mutually exclusive; the first one given wins.

## The 3D view

Two modes, toggled by the `Overview` / `Changes` buttons, `m`, or keys `1` and `2`:

- **Overview** — the city as it is: packages, their sub-packages, types and towers, and the
  cross-module dependency arcs between packages.
- **Changes** — the same city with the diff laid on top: added and deleted towers appear,
  changed ones take the change colour, and changed cross-module calls are drawn.

Selecting a package descends into it; selecting a tower opens its call path. The sidebar
shows the change for whatever is selected, with the resolved calls in source order.

| Key | Action |
| --- | --- |
| `/` | search for a package or function |
| `w a s d`, arrows | move the camera |
| `q` `e` | orbit |
| `-` `+` | zoom |
| `drag` / `scroll` | orbit / zoom with the mouse |
| `click` | enter a package, open a tower |
| `enter` | open a function |
| `esc` | go back |
| `b` | show / hide the sidebar |
| `0` | reset the view |
| `m`, `1`, `2` | overview / changes |
| `?` | legend |

## JSON output

**A parsed snapshot** (`-json`, no `-range`) — one object per file:

```json
[{"path": "lib/entity.go", "package": "lib", "importPath": "citydiff/lib", "module": "citydiff",
  "entries": [{"kind": "function", "entry": {"name": "…", "calls": [{"expr": "q.db.ExecContext"}]}}]}]
```

**A diff** (`-json -range`) — one object per file, `action` = `added` / `modified` / `deleted`,
each with one entry per declaration change:

```json
[{"path": "db/flashcard.sql.go", "action": "modified",
  "changes": [
    {"action": "added",
     "right": {"kind": "method", "entry": {"name": "ClearGenerationFinal",
               "parameters": [{"name": "ctx", "type": "context.Context"}],
               "returnArgs": [{"type": "error"}],
               "calls": [{"expr": "q.db.ExecContext"}]}}},
    {"action": "modified", "kind": "function", "name": "serve",
     "edits": [{"field": "bodyHash", "left": "9373749d…", "right": "26138d04…"},
               {"field": "bodyBytes", "left": 410, "right": 408}]}
  ]}]
```

- `kind` — `import`, `type`, `variable`, `function`, or `method`.
- `entry.calls` — direct calls recorded on the declaration, in source order, including calls
  inside nested function literals. `expr` is the callee as written. A `ref` is filled in when
  the callee is declared inside the same snapshot; a call to something outside it stays
  **unresolved** and is kept — the diff decides which calls matter.
- `bodyHash` / `methodsHash` — the signal that a body changed beyond its call list.
  `bodyBytes` says whether it grew or shrank.
- `edits[].field` — one of `fields`, `parameters`, `returnArgs`, `receiver`, `bodyHash`,
  `bodyBytes`, `calls`, `methodsHash`.

`-scene` prints the viewer's own graph (`{module, root, diff, packages}`), and `-view` serves
that same document at `/scene.json`. A package is a `{id, name, dir, parent, change, entities[],
deps[]}` node; an entity carries `kind`, `change`, `bodyBytes`, and aligned `calls[]` steps.

## How it works

The core is small and language-agnostic. A parser turns a **stream of files** into `Entity`
values: imports, types, variables, functions, methods, each with its direct calls in source
order. That stream is the **closed world** for the parse — resolution never depends on file
order, and a declaration in the first file can resolve a call in the last. Collect the stream,
then fill refs.

Two sources produce the same stream:

- **filesystem** — walks a file or directory tree (`lib/files`);
- **git** — reads the tree at each ref of a range with go-git (`lib/git`).

Each side of a range is a separate stream and a separate parse. `lib/diff` pairs files by path
and declarations by the same identity used for a single file, so a name shared by two files
stays two declarations. Body hashes stay the signal that a body changed beyond its call list.
`lib/scene` folds the result into the city the viewer draws.

Supported languages today: **Go** and **Rust** (`lib/parser/go`, `lib/parser/rust`), both via
[tree-sitter](https://tree-sitter.github.io/). Adding a language means adding a parser package
that yields the same `Entity` values — nothing downstream changes.

## Build from source

Requires **Go 1.27+** and a C toolchain: the parsers link the tree-sitter C bindings, so
**CGO is mandatory** (`CGO_ENABLED=0` fails with *"build constraints exclude all Go files"*).

```sh
git clone git@github.com:naqerl/citydiff.git
cd citydiff
make vet
CGO_ENABLED=1 go build -o citydiff ./cmd/cli
./citydiff -path . -json > /dev/null
```

`make vet` runs `go vet ./...`; `go fmt` via `make fmt`. The release workflow builds natively
per runner (linux/amd64, darwin/arm64) instead of cross-compiling, because of CGO.

## Releases

`.github/workflows/release.yml` runs vet + build + a smoke test on every push and PR to `main`.
On a `v*` tag it also publishes a GitHub Release with
`citydiff-<tag>-<os>-<arch>.tar.gz` per platform plus `checksums.txt`.

## Repository map

| Path | What |
| --- | --- |
| `AGENTS.md` | design intent and invariants |
| `cmd/cli/main.go` | flags, JSON shapes, the `-view` HTTP server |
| `lib/parser.go`, `lib/parser/` | source → `Entity` values (tree-sitter) |
| `lib/parser/go`, `lib/parser/rust` | per-language parsers |
| `lib/diff/` | comparing two parses at all three levels |
| `lib/scene/` | folding a diff into the viewer's scene graph |
| `lib/git/` | git source: reads trees at refs via go-git |
| `lib/files/` | filesystem source |
| `view/` | embedded browser viewer (three.js) |
| `install.sh` | one-command install of the binary and the agent skill |

## Known limitations

- The git source uses go-git and does not follow a **linked worktree** `.git` file. Run
  `-range` from the main checkout (a normal checkout whose `.git` is a directory).
- Only Go and Rust files are parsed; other file types are invisible to the diff.
