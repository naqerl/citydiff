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
