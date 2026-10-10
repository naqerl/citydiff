# citydiff

## Goal

Build a 3D diff. One change can be inspected from the outside inward, at three levels:

1. Cross-module dependencies. Which modules gained or lost a dependency on which other modules.
2. Entity relationships. Which declarations depend on which other declarations, and how those edges changed.
3. Call paths. Inside a function or method, which functions and methods it calls, in order, and how that path changed.

The outer level places the change. The inner level shows what the code does differently. All three levels are views of the same diff.

An import is a cross-module edge even when the imported package is outside the snapshot. The parser records every call. It fills a call ref when the called function or method is declared in the snapshot, and leaves the call unresolved otherwise.

## Snapshot

The parser takes a stream of files. Each item is a file path and its source bytes. The stream is the closed world for that parse.

Resolution does not depend on file order. A declaration in the first file resolves a call in the last file, and a call in the first file resolves to a declaration in the last file. Collect the stream, then fill refs.

Record every call on the enclosing function or method, in source order. Count both branches. Count calls nested in a function literal on that same enclosing declaration. Source order is the order. A call whose target is not declared in the snapshot stays unresolved. The parser keeps that call. The diff decides which calls matter.

A call path is a walk of these direct calls at the diff layer.

Body hashes stay the signal that a body changed beyond its call list.

The diff covers every file in the snapshot. Files pair by path. Declarations inside a file pair by the identity already used for a single file, so a name shared by two files stays two declarations.

Each side of a commit range is a separate stream and a separate parse.

## Sources

A filesystem source reads a tree of files. A git source reads the tree at a ref. Both yield the same stream. The parser does not open directories or repositories.

## CLI

The command line stays tight. It is a small set of flags that name what to read (`-path`,
`-range`) and which view to print or serve (`-json`, `-scene`, `-view`, `-tour`, `-addr`), and
it grows only when a new *input* cannot be expressed any other way.

A new feature does not get a flag. A flag is not free: the usage line, the flag list and the
README are the context an agent reads before it can use the tool at all, and every entry in
that context competes with the work itself. A longer flag list makes the tool harder to hold
in mind, which is exactly the kind of context degradation that turns a correct call into a
wrong one. So:

- **View state belongs to the page.** Which skin is open, which sidebar is showing, what is
  selected, how the camera sits: all of it is the browser's business, and none of it is a
  flag. The skin is a preference, remembered in the browser (localStorage) and reached
  through the viewer's own commands (`/skin` in the search box). Where you are in the diff —
  the selected node, overview or changes, calls or callers, the sidebars — is in the address
  bar, written by the page itself in a form a person can read (`view/state.js`), so a reload
  or a shared link comes back to it. The camera is in neither.
- **Paths and directories belong to the environment.** `CITYDIFF_SKINS_DIR` points at extra
  skins. Configuration like that is an environment variable with a sensible default, not
  another flag to explain.
- **Prefer a command, a key, or a default** over a flag. `citydiff tour validate` is a
  subcommand because it is a different verb, not a mode of `-view`.

When a flag seems necessary, say what the same thing would cost as a command, an environment
variable or a default, and pick the smallest surface that works. A flag added now is context
every future call has to carry.
