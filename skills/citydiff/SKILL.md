---
name: __SKILL_NAME__
description: Use when the user asks to describe a pull request, explain what changed on a branch, walk through a commit range, review or present a diff, or find who calls what — for Go, Rust or Swift. Citydiff turns the change into a 3D structural diff at three levels — cross-module dependencies, declaration/entity edges, and call paths — and can serve a viewer that plays a written tour of it. Reach for it even when the user never says citydiff, e.g. "what does this PR do", "summarise what I changed this week", "who calls this function", "walk me through the branch", "review my diff".
---

# __SKILL_NAME__ — a 3D code diff you can walk through

## Use this when

- The user asks you to **describe a pull request**, explain what changed on a branch, walk
  through a commit range, review or present a diff, or tell the story of a change.
- The user asks **who calls what**, where a call comes from, or how a path changed.
- You need to see a change structurally — which modules gained or lost a dependency, which
  declarations changed, which call paths are new — rather than read a flat diff.

Read the diff with `git diff` too: citydiff shows the structure, the notes a tour writes
must come from the source. Known limits: **Go, Rust and Swift only**; a commit range needs a
**git repository**; the default `citydiff` (the viewer) is **long-running**; building from
source needs **CGO**.

## What it shows

A diff viewed from the outside inward, at three levels:

1. **Cross-module dependencies** — which modules gained/lost a dependency on which.
2. **Entity relationships** — which declarations depend on which, and how those edges changed.
3. **Call paths** — inside a function/method, which functions it calls, in order, and how that changed.

The three levels are views of one diff. The parser records every call; the diff decides
which matter. The outer level places the change, the inner level shows what the code does
differently.

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
published sha256, replaces the binary, and rewrites this skill file. It also removes the
old `citydiff-tour` skill from any directory a previous install wrote it to.

## Flags

```
__BIN_NAME__ -path FILE|DIR              a file or directory tree to read
           -range A..B | A...B         diff two refs; works with git repos (reads the tree at a ref)
           -json                       machine-readable entries (pipe into jq) and exit
           -scene                      print the 3D scene as JSON (input for the viewer) and exit
           -text                       print the plain text diff (or the file list) and exit
           -tour FILE                  tour script for the viewer to load and play
           -addr 127.0.0.1:8787        listen address for the viewer (default)
           -p, -r                      short forms of -path, -range
```

With **no** `-json`/`-scene`/`-text`, `__BIN_NAME__` **serves the viewer and never
returns**. Naming more than one printing flag, or a printing flag together with `-addr` or
`-tour`, is a usage error. There is no `-view` flag — passing it fails.

Note: the viewer is **long-running** — always launch it in the background, never as a
foreground call (see below).

```
__BIN_NAME__ nodes [-path DIR] [-range A..B] [-changed] [-kind K] [-json]   # nodes a tour can name
__BIN_NAME__ tour validate|serve|schema ...                                 # see Tours
```

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

### The viewer is long-running — launch it in the background

With no output flag, `__BIN_NAME__` **serves the 3D viewer and never returns**. It is a
long-running process, not a command whose output you wait for:

- **Never** run it as a foreground bash call — the call would block forever.
- Launch it with your agent's background / run-in-background capability; if there is none,
  use `nohup … > log 2>&1 &` and keep the pid.
- **Poll** the address (`curl -fsS -o /dev/null http://127.0.0.1:8787/`) instead of blocking on
  the process.
- **Never** pass `-view` to mean this — the flag no longer exists.
- `kill` is how you stop it. If something is already listening on the port, **someone else
  owns that viewer** — check before starting another and pick a fresh `-addr`; do not kill a
  process you did not start.

```sh
# start it in the background, over a whole project
nohup __BIN_NAME__ -path /home/user/src/barse -addr 127.0.0.1:8787 \
  > /tmp/citydiff-view.log 2>&1 &
echo $! > /tmp/citydiff-view.pid

# start it over a commit range (the diff scene)
nohup __BIN_NAME__ -path /home/user/src/barse -range A..B -addr 127.0.0.1:8788 \
  > /tmp/citydiff-range.log 2>&1 &
echo $! > /tmp/citydiff-range.pid

# poll until it is up, then stop it by killing the process
curl -fsS -o /dev/null http://127.0.0.1:8787/ && echo up
kill "$(cat /tmp/citydiff-view.pid)"
```

A one-shot plain-text look at a diff (no browser, safe to wait for):

```sh
__BIN_NAME__ -path /home/user/src/barse -range A..B -text
```

The viewer exposes `GET /scene.json` (the scene graph) and `/` (static assets, embedded in
the binary). Bind to `127.0.0.1` unless remote access is intended — it has no auth.

Inside the page, `/` in the search box lists its commands: `/skin` opens the theme menu over
the city. The left sidebar hides, the city is framed between two diagonal lines, and the top
left counts the place in the list (6/10). `←` / `→` preview the previous and next theme in
place. Letters do too: a short pause, then the first theme whose name starts with what was
typed. The viewer's other keys do nothing while the menu is open. Enter applies the preview:
the diagonal lines move out to the edges, the new theme between them and the old theme
outside. Change after esc does the same. Ignore drops the preview with the lines unmoved.
The choice lives in the browser (localStorage); the process has no say in it, and
`CITYDIFF_SKINS_DIR` is what points at extra skins. Selecting a node shows its calls or its
callers, drawn between the towers that make them. The address bar follows the view —
`?select=citydiff:lib:diff:Name&mode=changes&refs=callers` (the node's path, one `:` per step;
a method is `…:Type:Name`), plus `side=closed` / `tourside=closed` — so a link to the viewer
can open on a given node.

## Keys

The viewer answers to these. `?` opens the same list inside the page, written from the one
source (`view/keys.js`), and the tests check that this table and that list agree.

| keys | does |
| --- | --- |
| `/` | search / commands |
| `wasd` `arrows` | move |
| `q` `e` | orbit |
| `-` `+` | zoom |
| `b` | left sidebar |
| `t` | tour sidebar |
| `0` | reset camera |
| `y` | bird view |
| `m` | overview / changes |
| `1` | overview |
| `2` | changes |
| `c` | calls / callers |
| `drag` | orbit |
| `scroll` | zoom |
| `click` | enters |
| `enter` | opens a function |
| `E` | open in nvim |
| `D` | diff of the change |
| `o` `i` | jump back / forward |
| `esc` | deselect, back to the city |
| `space` | tour play / pause |
| `,` `.` | tour previous / next |
| `?` | legend |

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

## Tours

A tour is a JSON script over the citydiff scene of one commit range. The viewer plays it:
each step moves the camera, lights nodes or a call path, and shows a markdown note in the
tour sidebar on the right. The sidebar holds the tour title and range, the note and code, and
play/pause, prev/next and a step counter at the bottom. Write a tour when the user asks you
to walk through, present or review a change.

### Write a tour

1. **Fix the one range and read the change.** The script's `range` is what the viewer shows.
   Read the diff so the notes can say why, not only where.

   ```sh
   git -C REPO log --oneline BASE..HEAD
   citydiff -path REPO -range BASE..HEAD -json > /tmp/diff.json
   ```

2. **List the nodes** the script can name.

   ```sh
   citydiff nodes -path REPO -range BASE..HEAD -changed          # kind, change, name
   citydiff nodes -path REPO -range BASE..HEAD -changed -json    # [{name, kind, id, package, change, file}]
   citydiff nodes -path REPO -range BASE..HEAD -kind method      # all methods, changed or not
   ```

3. **Write the script**, e.g. `tour.json` (schema below), as a story: see *What a tour must
   say*. 5–12 steps is a good length.

4. **Validate it.** `citydiff tour validate` is the **sole source of truth** for whether a
   tour is correct. It resolves every name against the real scene the viewer builds, so a
   tour it accepts needs no other check — never open the viewer, take a screenshot, or drive
   a browser to confirm one. Validate after every edit; it exits 1 on any problem. Unknown
   names come with suggestions, ambiguous names list the candidates to pick from.

   ```sh
   citydiff tour validate -path REPO tour.json            # range comes from the script
   citydiff tour validate -path REPO tour.json -json      # the resolved script (scene ids, call paths)
   ```

5. **Open it** to watch. The server never exits, so run it in the background.

   ```sh
   nohup citydiff tour serve -path REPO -addr 127.0.0.1:8787 tour.json > /tmp/citydiff-tour.log 2>&1 &
   # then open http://127.0.0.1:8787/
   ```

   Equivalent: `citydiff -path REPO -tour tour.json` (the default serve mode). Either way the
   viewer shows the script's `range`, whatever `-range` says. A running viewer also plays
   `?tour=<url>` (fetched by the browser) or a script dropped onto the page; the server checks
   it the same way.

### What a tour must say

A tour explains a change so a person understands it. It is the diff's narrative: the **why**
and the consequence, not just where the code moved. A good tour descends the three levels —
it opens on the cross-module dependencies, goes through the declarations that changed, and
lands on the call paths that carry the new behaviour. Test every note: if it would read the
same for a different change, rewrite it.

Shape it as a story:

1. **Open wide** in `changes` mode: one step on the whole range — what it does as a whole,
   and which packages are new or touched.
2. **Go package by package**, in dependency order, one step per package that carries the
   change, saying what each gained or lost and why it had to.
3. **Land on the functions**: `focus` the declarations that were added or rewritten, `code`
   the ones whose body is the point.
4. **Follow the call paths that matter**: `path` from the entry point to the effect,
   `highlight` for a set of nodes that changed together.
5. **End on the point**: the last step says what is different now for the user of the code —
   not a recap of the file list.

Choose each field to serve the sentence the note is making:

- `mode` — `changes` for every step that talks about the change; `full` only for an opening
  or a deliberate "here is the plain city".
- `select` — a package and its dependencies; it draws that package's changed arcs.
- `focus` — one function's callees, or one type or variable; `code` — when the reader must see
  the body, especially a line diff.
- `highlight` — a set of nodes that changed together; `path` — when the sentence is "this
  reaches that".
- `camera` / `zoom` — fit the sentence: `top` for a whole package, `close` for one node,
  `overview` for the whole change.

`path` is a call path, not a call graph. It may legitimately pass through unchanged
functions — often the new behaviour runs through an old helper, and that is the point, so use
`nodes -changed` for the cast but let `path` use what it needs. Calls through an interface are
not resolved, so a path can stop at an interface call; if validate reports no path, point it
at a concrete hop or `highlight` both ends instead.

Note style: **2–4 sentences**. Put names in backticks. Say why the change exists and what it
means for the reader. Do not restate the code that is already on screen, and do not sell the
change.

- Weak: "`SelectFinal` was added to the review service."
- Good: "`review.Service.SelectFinal` refuses once the lesson is published, then marks a
  finished run final — a teacher can no longer pick a version that is still generating."

Be honest about the evidence:

- A **low-level file change is not a visible change**: a `modified` package whose declarations
  are all `same` changed only internals or formatting — say so, or leave it out.
- **Unresolved calls are not errors**: the snapshot is the closed world, so a call to
  something outside it simply has no arc.
- **`citydiff tour validate` is the sole source of truth.** A tour it accepts needs no browser
  check: never screenshot or drive the viewer to confirm one, and never call a tour good
  because the page opened.

A tour is **not** a commit log, not a summary of every file in the diff, and not a substitute
for reading the diff. It is the shortest honest path from "what changed" to "what it means".

### Script

```json
{
  "version": 1,
  "title": "What this range does",
  "range": "dbdafc00..061e9aed",
  "steps": [
    { "title": "The whole change", "note": "Markdown **note**.", "mode": "changes", "camera": "overview" },
    { "title": "The generator", "select": "service/flashcard/generator" },
    { "title": "Handler to view", "path": { "from": "handler.handlePostSelectFinal", "to": "meetings/view.versionBar" } },
    { "title": "Picking the final", "focus": "review.Service.SelectFinal", "code": "review.Service.SelectFinal", "duration": 10 },
    { "title": "Storage", "highlight": ["db.Queries.SetGenerationFinal", "generator.FinalVersion"], "dim": true }
  ]
}
```

Top level: `version` (must be `1`), `title`, `range` (`base..head` as for `-range`;
abbreviated hashes and refs are fine, they are compared by commit), `steps` (one or more).

Step fields (only `title` is required). Each step starts from a clean view; nothing carries
over from the step before except the mode.

| field | type | effect |
| --- | --- | --- |
| `title` | string | Note heading. |
| `note` | markdown | Note panel text: paragraphs, `**bold**`, `*italic*`, `` `code` ``, fenced code, `-`/`1.` lists, `#` headings, http(s) links. No raw HTML. |
| `duration` | seconds | How long autoplay stays on the step. Default 8. |
| `mode` | `"changes"` \| `"full"` | `changes` colours the diff and draws dependency arcs; `full` is the plain city. Sticks until a later step changes it. |
| `select` | package name | Selects a package (or external import) and flies to it; its changed calls or dependencies are drawn as arcs. |
| `focus` | any name | A function or method opens its call focus (callees fanned out); a type or variable is selected; a package is selected. Not with `select`. |
| `highlight` | names | Lights these nodes and shades the rest; calls between them are drawn as arcs. |
| `path` | `{from, to}` | Lights the shortest resolved call path between two functions/methods, with an arc per hop. |
| `dim` | bool | With `highlight`/`path`: `false` keeps the rest of the city bright. Default `true`. |
| `camera` | `"overview"` \| `"top"` \| `"fit"` \| `"close"` | `overview` frames the city, `top` looks down on the step's nodes, `fit` frames them, `close` moves in. Default: the select/focus camera, else `fit` when the step names nodes, else `overview`. |
| `zoom` | number > 0 | Multiplies the camera distance after the camera move: `0.5` is twice as close, `1.5` farther. |
| `code` | declaration | Shows its source under the note: a line diff when it changed, the new or old body when added or removed. |

### Names

Names are matched against the scene the way you would write them:

- Package: its path, `barse/service/flashcard/generator`, or any unique tail, `flashcard/generator`, `generator`.
- Go declaration: `pkg/path.Func`, `pkg.Type.Method`, `Type.Method`, `Func`. Any unique tail of whole segments works.
- Rust: `crate::module::func`, `module::Type::method`, `<T as Trait>::m`, `<T as fmt::Display>::fmt`. A trait method is also found as `T::m` when nothing else matches.
- Swift: `Module.Type.method`, `Type.method`, `Module.func`, nested `Outer.Inner.method`; `init`, `deinit`, `subscript` and computed properties are methods of their type. Members of `extension T` and `extension T: P` are both found under `T`. Each overload is its own node with the same name, so `Type.init` with several initializers is ambiguous: name another node, or use the scene id `citydiff nodes` prints.
- A scene id (`file#kind#Name` from `nodes -json`) always works.
- Case is ignored only if nothing matches exactly.

A name that matches several nodes is an error, never a guess: qualify it
with more segments (`gen.New` instead of `New`). `select` takes packages
only, `path` takes functions and methods only, and `code` takes
declarations only.

The JSON Schema is printed by `citydiff tour schema`, and lives at
https://raw.githubusercontent.com/naqerl/citydiff/main/lib/tour/tour.schema.json.
A full example over a real range is
https://github.com/naqerl/citydiff/blob/main/examples/barse-flashcard-versions.tour.json.

### Player

| key | action |
| --- | --- |
| `space` | play / pause (autoplay uses each step's duration) |
| `,` `.` | previous / next step |
| `t` | hide / show the tour sidebar (`b` does the left one) |
| `?` | the legend, with every key the viewer answers to |

Dragging or clicking in the city pauses autoplay. The × in the sidebar
header closes the tour. Loading a tour switches the viewer to the tour's
range, so the left sidebar shows
that range's changes; a range that does not resolve is reported in the
tour sidebar.

## Repo map (for questions)

| Path | What |
| --- | --- |
| `AGENTS.md` | design intent and invariants of the tool |
| `cmd/cli/main.go` | flags, JSON shapes, the viewer's HTTP server |
| `lib/parser.go`, `lib/parser/` | Go source → Entry values (tree-sitter) |
| `lib/diff/` | comparing two parses at all three levels |
| `lib/git/` | git source: reads trees at refs via go-git |
| `lib/files/` | filesystem source |
| `lib/tour/` | tour schema, name resolution, validation, call paths |
| `view/` | embedded browser viewer (main.js, layout.js, skin.js, changes.js, tour.js, keys.js) |
| `.github/workflows/release.yml` | CI: vet, test, build+smoke, release on `v*` tags |
