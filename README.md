# CityDiff

![thumb](https://s3.naqerl.com/public/demo/citydiff.png)

**Higher order** diff tool that helps humans to control architecture and agents to generate interactive tours over their changes.

The city is the code. Packages are buildings; a package's sub-packages stand on its roof.
A **type** is a wide pale block holding its methods. A **function** or **method** is a tower
whose **height is its body size**. A **call** is an arc from the top of the caller to
the top of what it calls.

## Install

Just ask your agent to install and paste a link to this repo

```
Install citydiff, read added skills and create a demo tour on whatever project I'm currently working on. https://raw.githubusercontent.com/naqerl/citydiff/refs/heads/main/install.sh
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

The same viewer runs in Docker. From this repository, `make run-docker` builds
the image, mounts the project at `/work`, and diffs the two latest commits:

```sh
make run-docker DOCKER_PATH=~/src/myrepo DOCKER_RANGE=main~5..main
```

then open `http://localhost:8787` (`PORT` changes the host port). The page reads
the scene from `GET /scene.json`; the viewer assets are embedded in the binary.

## Command line

```
citydiff [-json | -scene | -view [-tour file]] -path file-or-directory [-range A..B]
citydiff nodes [-path dir] [-range A..B] [-changed] [-kind k] [-json]
citydiff tour validate|serve [-path dir] [-range A..B] [-json] [-addr host:port] tour.json
citydiff tour schema
```

| Flag | Meaning |
| --- | --- |
| `-path`, `-p` | a Go, Rust or Swift file, or a directory tree to walk. Required. |
| `-range`, `-r` | `A..B` compares those two commits; `A...B` compares their merge base with `B` (how `git diff` treats a three-dot range). An empty side means `HEAD`. |
| `-json` | print machine-readable entries instead of text. |
| `-scene` | print the scene graph the viewer draws, instead of text. |
| `-view` | serve the 3D viewer over HTTP (long-running). |
| `-addr` | listen address for `-view` (default `127.0.0.1:8787`). |
| `-tour` | with `-view`, a [tour](#tours) script the viewer loads and plays. |

The command line stays this small on purpose: every flag is context a model has to read
before it can use the tool, and a long flag list costs more than it explains. View state
belongs to the page, and paths belong to the environment — `CITYDIFF_SKINS_DIR` for the
[skins](#skins) directory, not a flag. See [AGENTS.md](AGENTS.md).

Notes:

- With `-range`, `-path` must live inside a **git checkout**: the nearest parent directory
  containing `.git` is the repository, and the tree at each ref is read straight from the
  object database with [go-git](https://github.com/go-git/go-git) — the working tree does not
  need to be checked out at either ref.
- Without `-range`, the filesystem tree is parsed and there is no diff. The viewer says so.
- Only Go, Rust and Swift source files are parsed. A commit that touches only other files (`*.js`,
  `*.sql`, …) produces an empty diff.
- `-json`, `-scene` and `-view` are mutually exclusive; the first one given wins.

## The 3D view

Two modes, toggled by the `Overview` / `Changes` buttons, `m`, or keys `1` and `2`:

- **Overview** — the city as it is: packages, their sub-packages, types and towers, and the
  cross-module dependency arcs between packages.
- **Changes** — the same city with the diff laid on top: added and deleted towers appear,
  changed ones take the change colour, and the calls the range changed are drawn between the
  towers that make them. Added and removed module dependencies stay at module level, drawn
  between the packages.
  A changed function or method is a darker yellow when only its body changed, the usual
  yellow when only its signature changed, and a brighter yellow when both changed.

Selecting a package descends into it; selecting a tower opens its call path. The sidebar
shows the change for whatever is selected, with the resolved calls in source order.
Under the selected node the sidebar offers **Calls** / **Callers** — a node with nothing to show
(a package with no declarations, a type with no methods) gets neither — and the `c` key flips
them, picking which way the arcs run: **Calls** draws the functions a tower calls, with the
particles leaving it, **Callers** draws the functions that call it. Callers use the same arcs,
with the particles running back toward the node. Only one of the two is on at a time.

A package is shown the same way, from the towers: **Calls** draws the arcs its own declarations
make, each one starting at the function or method that writes the call, and **Callers** draws
the calls that land on those declarations, each one starting at the tower that makes it. A
package outside the tree has no declarations to draw from, so it keeps the dependency fan
between package blocks. A type is read through the methods it declares.

The viewer keeps a **vim-style jump list** of every node you select. Press `o` to step to
an older selection and `i` to step to a newer one — the same directions as vim's `<C-o>`
and `<C-i>`. Selecting a new node after going back truncates the forward tail, exactly as
vim does. The sidebar note shows your position (`jump 2/5`) once the list has more than one
entry.

The address bar follows the view, so a reload — or a link you send — opens on the same
place. It is written to be read and edited by hand:

```
?select=citydiff:lib:diff:TestCoolStuff&mode=changes&refs=callers&side=closed&tourside=closed
```

`select` is the node's path from the top of the tree down, one `:` per step: a package is
`citydiff:lib:diff`, a function in it `citydiff:lib:diff:TestCoolStuff`, a type
`citydiff:lib:Parser` and its method `citydiff:lib:Parser:Parse`. When two declarations share
a path (two `init` functions in one package), the file follows: `citydiff:cmd:init@cmd/a.go`.
`mode=changes`, `refs=callers`, `side=closed` and `tourside=closed` are only there when they
differ from the default (overview, calls, both sidebars open). The camera is not stored; a
restored selection is flown to the way a click would. When a tour is loaded and the address names a view, the
tour waits instead of replacing it: play, `.` or a roadmap click starts it.

| Key | Action |
| --- | --- |
| `/` | search for a package or function |
| `w a s d`, arrows | move the camera |
| `q` `e` | orbit |
| `-` `+` | zoom |
| `drag` / `scroll` | orbit / zoom with the mouse |
| `click` | enter a package, open a tower |
| `c` | show calls or callers |
| `enter` | open a function |
| `E` | open the selected node in nvim |
| `esc` | deselect and fly back to the city |
| `o` `i` | jump back / forward through the selection history |
| `b` | show / hide the left sidebar |
| `t` | show / hide the tour sidebar |
| `0` | reset the view |
| `m`, `1`, `2` | overview / changes |
| `space` | tour: play / pause |
| `,` `.` | tour: previous / next step |
| `?` | legend |

## Open in nvim

With a node selected, `E` (shift-e) opens a terminal over the city running
`nvim` on the machine that serves the viewer. A function, method, type or
variable opens its file at the line and column of its name; a package opens
its directory (or its first file). Quitting nvim closes the terminal. In a
range scene the working-tree file is opened; a file that is not in the working
tree, as one deleted in the range, shows a message instead (`esc` closes it).
Paths outside the served directory are refused. The terminal is
[ghostty-web](https://github.com/coder/ghostty-web), vendored under
`view/vendor/ghostty` and loaded on first use, over a WebSocket to a PTY.

## Skins

A skin is a JSON file that recolours the viewer. Each key is one drawn element. A file may set any subset, and omitted keys keep the [dark](skins/dark/skin.json) skin. [light](skins/light/skin.json) is the built-in light skin. Skins that are not in the binary live in a directory of your own.

Type `/` in the search box to see the commands — `↑` / `↓` and `tab` / `shift-tab` walk the completions — and `/skin` to open the theme panel in the right sidebar. It lists the themes, previews one the moment you click it (or walk them with the arrows), and **Save** remembers it and closes the panel. The choice lives in the browser (localStorage), so a reload comes back to the theme you saved, and closing the panel without saving drops the preview. Nothing about it reaches the command line or the address bar.

The fields, the skybox shapes, and how to point the viewer at a skins directory are in [SKINS.md](SKINS.md).

```sh
CITYDIFF_SKINS_DIR=~/skins citydiff -path . -view
```

## Tours

A tour is a versioned JSON script over the scene of one commit range: an ordered list of
steps the viewer plays. It is meant to be written by an agent (see the `citydiff-tour`
[skill](skills/citydiff-tour/SKILL.md)) or by hand. The schema is
[`lib/tour/tour.schema.json`](lib/tour/tour.schema.json) (`citydiff tour schema` prints it), and
[`examples/barse-flashcard-versions.tour.json`](examples/barse-flashcard-versions.tour.json)
is a full example.

```json
{
  "version": 1,
  "title": "Flashcard versions",
  "range": "dbdafc00..061e9aed",
  "steps": [
    { "title": "The whole change", "note": "Markdown **note**.", "mode": "changes", "camera": "overview" },
    { "title": "The generator", "select": "service/flashcard/generator" },
    { "title": "Picking the final", "focus": "review.Service.SelectFinal", "code": "review.Service.SelectFinal" },
    { "title": "Handler to view", "path": { "from": "handler.handlePostSelectFinal", "to": "meetings/view.versionBar" } },
    { "title": "Storage", "highlight": ["db.Queries.SetGenerationFinal", "generator.FinalVersion"], "dim": false }
  ]
}
```

| Step field | Effect |
| --- | --- |
| `title` | required; the roadmap entry and note heading |
| `note` | markdown shown in the note panel (top right) |
| `duration` | seconds autoplay stays on the step (default 8) |
| `mode` | `changes` or `full` |
| `select` | select a package or external import and fly to it |
| `focus` | open a function's or method's call focus; select a type, variable or package |
| `highlight` | light several nodes and the calls between them |
| `path` | `{from, to}`: light the shortest resolved call path between two functions, with its arcs |
| `dim` | with `highlight`/`path`, `false` keeps the rest of the city bright |
| `camera` | `overview`, `top`, `fit` or `close` |
| `zoom` | multiply the camera distance (`0.5` is twice as close) |
| `code` | show a declaration's source under the note, as a line diff when it changed |

Names are what you would write: a package path or a unique tail of it (`barse/db`, `db`), a Go
declaration (`pkg/path.Func`, `pkg.Type.Method`, `Type.Method`), a Rust item
(`crate::mod::f`, `<T as Trait>::m`), a Swift declaration (`Module.Type.method`,
`Type.method`, `Module.func`), or a scene id. A name matching several nodes is an error
that lists the candidates; an unknown name comes with suggestions.

```sh
citydiff nodes -path ~/src/barse -range dbdafc00..061e9aed -changed       # the names to use
citydiff tour validate -path ~/src/barse tour.json                         # resolve every name; exit 1 on problems
citydiff tour validate -path ~/src/barse tour.json -json                   # the resolved script
citydiff tour serve -path ~/src/barse -addr 127.0.0.1:8787 tour.json       # validate, then serve the viewer with it
```

`-range` defaults to the script's own `range`; ranges are compared by commit, so abbreviated
hashes and refs match. A running viewer also plays `?tour=<url>` (fetched by the browser) and a
script dropped onto the page; the server resolves those through `POST /api/tour` with the same
checks as the CLI.

The tour plays in a **right sidebar** that mirrors the left one: the tour title and range,
the step's note and code, the roadmap (click a step to jump; the current one is marked), and
the player controls at the bottom. **space** plays and pauses (autoplay uses the durations),
**,** and **.** step back and forward, and **t** hides and shows the sidebar the way **b** does
the left one. Dragging or clicking in the city pauses autoplay. The camera centres and fits in
the space between the two sidebars, and re-centres when either one is shown or hidden.

Loading a tour moves the viewer to the tour's `range`: `tour serve` starts on it, `-view -tour`
switches to it at startup, and a `?tour=` or dropped script makes the server rebuild the scene
for that range (when it differs, compared by commit) and the page reload into it, so the left
sidebar shows the tour's changes. A range that does not resolve in the repository is reported
in the tour sidebar and leaves the current scene alone.

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

Supported languages today: **Go**, **Rust** and **Swift** (`lib/parser/go`, `lib/parser/rust`,
`lib/parser/swift`), all via [tree-sitter](https://tree-sitter.github.io/).

Swift modules come from `Package.swift`: each target (`Sources/<Target>`, `Tests/<Target>`, or
its `path:`) is one module named after the target, sharing one namespace across its files.
Without a manifest the top folder is the module, as in an Xcode project. `.build/` and
`DerivedData/` are skipped. Members of `extension T` belong to `T`; members of a conformance
extension (`extension T: P`) are kept apart as `<T as P>` in the parser, like a Rust trait
impl, and filed under `T` in the city. Calls resolve within the module and the package
modules a file imports: free functions, `Type(...)` as an initializer, `Type.f`, `self.m()`
and implicit self, supertypes and protocol extensions, and methods on locals whose type is
known from `let x = Type(...)`, an annotation, a parameter, a return type or a property.
Overloads are told apart by argument labels; a call that still matches several
declarations, or a struct's implicit memberwise initializer, stays unresolved. Each
overload is its own building, but a resolved call is drawn to the first overload of that
name in its file. Adding a language means adding a parser package
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
| `lib/parser/go`, `lib/parser/rust`, `lib/parser/swift` | per-language parsers |
| `lib/diff/` | comparing two parses at all three levels |
| `lib/scene/` | folding a diff into the viewer's scene graph |
| `lib/git/` | git source: reads trees at refs via go-git |
| `lib/files/` | filesystem source |
| `view/` | embedded browser viewer (three.js) |
| `skins/` | built-in viewer skins, `dark` and `light` |
| `SKINS.md` | how to write a viewer skin |
| `lib/tour/` | tour scripts: schema, name resolution, validation, call paths, code snippets |
| `cmd/cli/tour.go` | the `nodes` and `tour` subcommands and the tour HTTP endpoints |
| `skills/citydiff-tour/` | the agent skill for writing tours |
| `examples/` | example tours |
| `install.sh` | one-command install of the binary and the agent skills |

## Known limitations

- The git source uses go-git and does not follow a **linked worktree** `.git` file. Run
  `-range` from the main checkout (a normal checkout whose `.git` is a directory).
- Only Go, Rust and Swift files are parsed; other file types are invisible to the diff.
