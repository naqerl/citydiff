---
name: __SKILL_NAME__
description: 3D structural diff of Go code ("citydiff") — cross-module dependency changes, declaration/entity edges and call-path changes across a commit range. Use when asked what a commit or range changed structurally, who calls what, or how a call path changed.
---

# __SKILL_NAME__ — citydiff, a 3D code diff

A diff viewed from the outside inward, at three levels:

1. **Cross-module dependencies** — which modules gained/lost a dependency on which.
2. **Entity relationships** — which declarations depend on which, and how those edges changed.
3. **Call paths** — inside a function/method, which functions it calls, in order, and how that changed.

The three levels are views of one diff.

To walk someone through a range in the viewer, write a tour: see the
`citydiff-tour` skill installed next to this one (`citydiff nodes`,
`citydiff tour validate`, `citydiff tour serve`).

## Source of truth

| | |
| --- | --- |
| Repository | https://github.com/__REPO__ |
| Release | https://github.com/__REPO__/releases (tags `v*`) |
| Installer | https://raw.githubusercontent.com/__REPO__/main/install.sh |
| Module | `citydiff`, Go 1.27, **requires CGO** (tree-sitter C bindings) |
| Installed as | `__BIN_DIR__/__BIN_NAME__` (release installed here on __DATE__) |
| Release used | __VERSION__ |

For any question about behaviour, internals or bugs: **the answer lives in the repository
above, not in this file.** Read `AGENTS.md` in the repo for the design intent, `lib/parser`
for how declarations and calls are recorded, `lib/diff` for how two parses are compared,
`cmd/cli/main.go` for flags, and `view/` for the browser UI. When this file and the code
disagree, the code wins — and this file should be updated.

## Install / update

```sh
curl -fsSL https://raw.githubusercontent.com/__REPO__/main/install.sh | sh          # latest release
curl -fsSL https://raw.githubusercontent.com/__REPO__/main/install.sh | sh -s -- --tag=v0.0.1
TAG=v0.0.2 curl -fsSL https://raw.githubusercontent.com/__REPO__/main/install.sh | sh
UNINSTALL=1 curl -fsSL https://raw.githubusercontent.com/__REPO__/main/install.sh | sh   # remove
```

Re-running the installer upgrades in place: it resolves the newest release, verifies the
published sha256, replaces the binary, and rewrites this skill file.

## Flags

```
__BIN_NAME__ -path FILE|DIR              parse a file or directory tree
           -range A..B | A...B         diff two refs; works with git repos (reads the tree at a ref)
           -json                       machine-readable entries (pipe into jq)
           -scene                      print the 3D scene as JSON (input for the viewer)
           -view                       serve the 3D viewer over HTTP
           -addr 127.0.0.1:8787        listen address for -view (default)
           -p, -r                      short forms of -path, -range
```

Note: `-view` is **long-running** — always launch it in the background (see below).
Note: for a commit range the path must be a **git repository**, not a plain directory;
go-git reads the trees at the two refs, so the working tree does not need to be checked out.

## Recipes

Parse a single file or a project tree (one-shot, prints JSON):

```sh
__BIN_NAME__ -path /home/user/src/barse -json | jq '.[] | {path, decls: (.entries | length)}'
```

Diff a commit range — the main use:

```sh
cd /home/user/src/barse
__BIN_NAME__ -path . -range 6ca8b06..3aff57d -json > /tmp/diff.json
jq '[.[] | select(.action=="modified") | {path, changes: [.changes[] | {action, name: (.left.entry.name // .right.entry.name)}]}]' /tmp/diff.json
```

Diff the last N commits of the current branch:

```sh
cd "__REPO_DIR__"
base=$(git rev-parse HEAD~20)
__BIN_NAME__ -path . -range "$base..HEAD" -json > /tmp/range.json
```

### Launch the 3D viewer in the background

It serves on 127.0.0.1:8787 by default and never exits, so never call it in the foreground.

```sh
# long-lived viewer over a whole project
nohup __BIN_NAME__ -path /home/user/src/barse -view -addr 127.0.0.1:8787 \
  > /tmp/citydiff-view.log 2>&1 &
echo $! > /tmp/citydiff-view.pid

# viewer over a commit range (the diff scene)
nohup __BIN_NAME__ -path /home/user/src/barse -range A..B -view -addr 127.0.0.1:8788 \
  > /tmp/citydiff-range.log 2>&1 &
echo $! > /tmp/citydiff-range.pid

# check / stop
curl -fsS http://127.0.0.1:8787/scene.json > /dev/null && echo up
kill "$(cat /tmp/citydiff-view.pid)"
```

The viewer exposes `GET /scene.json` (the scene graph) and `/` (static assets, embedded in
the binary). Bind to `127.0.0.1` unless remote access is intended — it has no auth.

Inside the page, `/` in the search box lists its commands: `/skin` opens the theme panel in
the right sidebar, where a skin is previewed on click and kept with Save. The choice lives in
the browser (localStorage); the process has no say in it, and `CITYDIFF_SKINS_DIR` is what
points at extra skins. Selecting a node shows its calls or its callers, drawn between the
towers that make them.

## Building from source (no release available)

```sh
git clone git@github.com:__REPO__ && cd citydiff
make vet
go build -o __BIN_NAME__ ./cmd/cli
```

CGO is mandatory: `CGO_ENABLED=0` fails with *"build constraints exclude all Go files"* in
`tree-sitter-go/bindings/go`. Release CI therefore builds natively per runner
(linux/amd64 on ubuntu, darwin/arm64 on macos) instead of cross-compiling.

## Reading the output

**Plain parse (`-json` without `-range`)** — one object per file:

```json
[{"path": "pkg/x.go", "entries": [{"name": "...", "kind": "...", "calls": [...]}]}]
```

**Diff (`-range`)** — one object per file, `action` = `added` / `modified` / `deleted`, and
`changes[]` holds one entry per declaration change, each with its own `action` and a `left`
and/or `right` side:

```json
[{"path": "db/flashcard.sql.go", "action": "modified",
  "changes": [{"action": "added",
               "right": {"kind": "method",
                         "entry": {"name": "ClearGenerationFinal",
                                   "parameters": [{"name": "ctx", "type": "context.Context"}],
                                   "returnArgs": [{"type": "error"}],
                                   "calls": [{"expr": "q.db.ExecContext"}]},
                         "bodyHash": "67a9fdd5…"}}]}]
```

- `entry.kind` — the declaration kind (func, method, type, …).
- `entry.calls` — direct calls recorded on that declaration, in source order, including
  calls inside nested function literals. `expr` is the call expression; a `ref` is filled in
  when the callee is declared inside the same snapshot. A call whose target lies outside the
  snapshot stays **unresolved** and is kept.
- `bodyHash` — the signal that a body changed beyond its call list.
- Unresolved calls are not errors: the snapshot is the closed world for that parse, and the
  diff decides which calls matter.

The viewer consumes the scene graph `{module, root, diff, packages}`; `GET /scene.json`
returns exactly that for whatever the process was launched with.

## Repo map (for questions)

| Path | What |
| --- | --- |
| `AGENTS.md` | design intent and invariants of the tool |
| `cmd/cli/main.go` | flags, JSON shapes, the `-view` HTTP server |
| `lib/parser.go`, `lib/parser/` | Go source → Entry values (tree-sitter) |
| `lib/diff/` | comparing two parses at all three levels |
| `lib/git/` | git source: reads trees at refs via go-git |
| `lib/files/` | filesystem source |
| `view/` | embedded browser viewer (main.js, layout.js, skin.js, changes.js, tour.js, keys.js) |
| `.github/workflows/release.yml` | CI: vet, test, build+smoke, release on `v*` tags |
