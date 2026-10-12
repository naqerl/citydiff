# Plugins

## Overview

A plugin is a JavaScript module that adds behaviour on top of the viewer's existing features. It can add a command to the search box, a key to the legend, an item to the action ring, a section to the sidebar, or a button to the diff panel. It can also drive the viewer the way a user does: select, search, open call focus, switch modes, open a diff or the editor. It never draws or recolours anything; skins do that.

Plugins live in `~/.config/citydiff/plugins/<name>/plugin.js`, or under `CITYDIFF_PLUGINS_DIR` when it is set. They load only from that folder, never from the repository being diffed. The viewer imports each plugin once the scene is ready and passes it `city`, the object documented below.

Every other file in a plugin's folder is served next to its `plugin.js`, except dot files, so a plugin can import its own modules and load its own data: `import("./match.js")`, `new URL("./model.bin", import.meta.url)`. The files are served to be revalidated: a reload asks again, and an unchanged file comes back as a 304, so a large one is not downloaded on every reload.

A plugin added while the viewer runs loads on the next reload of the page. The plugins directory itself has to exist when citydiff starts. What a plugin gets wrong is shown in the status line under the search box and logged to the browser console; the viewer carries on.

The smallest plugin that does something greets whatever you select:

```js
// ~/.config/citydiff/plugins/hello/plugin.js

export const api = 1;

export default function hello(city) {
  city.sidebar.add({
    title: "Hello",
    render: (node) => `Hello, \`${node.name}\`!`,
  });
}
```

Select any package or declaration, and its sidebar detail has a **Hello** section with its name.

<details>
<summary>Example: a complete plugin</summary>

```js
// ~/.config/citydiff/plugins/review/plugin.js
export const api = 1;

export default function review(city) {
  const changed = city.scene.packages
    .flatMap((pkg) => pkg.entities)
    .filter((e) => e.change !== "same");
  let at = -1;

  const next = () => {
    if (!changed.length) return;
    at = (at + 1) % changed.length;
    city.selection.select(changed[at].id);
  };

  city.keys.add({ key: "n", does: "next change", run: next });
  city.ring.add({
    label: "next change",
    can: () => (changed.length ? true : "nothing changed in this range"),
    run: next,
  });
  city.diff.add({
    label: "edit here",
    run({ file, hunk }) {
      city.diff.close();
      city.editor.open({ file, line: hunk.newStart });
    },
  });
}
```

</details>

## Index

- [Plugin file](#plugin-file)
- [Conventions](#conventions)
- [city.version](#cityversion)
- [city.scene](#cityscene): [packages](#cityscenepackages) · [module](#cityscenemodule) · [range](#cityscenerange) · [node(id)](#cityscenenodeid) · [callers(id)](#cityscenecallersid)
- [city.selection](#cityselection): [current](#cityselectioncurrent) · [refs](#cityselectionrefs) · [select(id)](#cityselectionselectid) · [clear()](#cityselectionclear) · [back()](#cityselectionback) · [forward()](#cityselectionforward) · [setRefs(dir)](#cityselectionsetrefsdir) · [onChange(fn)](#cityselectiononchangefn)
- [city.focus](#cityfocus): [current](#cityfocuscurrent) · [open(id)](#cityfocusopenid) · [close()](#cityfocusclose) · [onChange(fn)](#cityfocusonchangefn)
- [city.view](#cityview): [mode](#cityviewmode) · [bird](#cityviewbird) · [setMode(mode)](#cityviewsetmodemode) · [setBird(on)](#cityviewsetbirdon) · [resetCamera()](#cityviewresetcamera) · [onChange(fn)](#cityviewonchangefn)
- [city.sidebar](#citysidebar): [shown](#citysidebarshown) · [show(open)](#citysidebarshowopen) · [onChange(fn)](#citysidebaronchangefn) · [add(section)](#citysidebaraddsection)
- [city.search](#citysearch): [open(query)](#citysearchopenquery) · [add(source)](#citysearchaddsource)
- [city.commands](#citycommands): [add(command)](#citycommandsaddcommand)
- [city.keys](#citykeys): [add(binding)](#citykeysaddbinding)
- [city.ring](#cityring): [add(item)](#cityringadditem)
- [city.diff](#citydiff): [current](#citydiffcurrent) · [files(id)](#citydifffilesid) · [open(id)](#citydiffopenid) · [close()](#citydiffclose) · [onChange(fn)](#citydiffonchangefn) · [add(action)](#citydiffaddaction)
- [city.editor](#cityeditor): [open(target)](#cityeditoropentarget)
- [Types](#types): [Node](#node) · [Package](#package) · [Entity](#entity) · [Dep](#dep) · [CallStep](#callstep) · [Change](#change) · [Kind](#kind) · [DiffFile](#difffile) · [Hunk](#hunk) · [Line](#line)
- [Not yet](#not-yet)

## Plugin file

```ts
export const api: number
export default function (city: City): void | (() => void)
```

`api` is the lowest API version the plugin needs. A viewer older than that skips the plugin and says so, instead of running it half-broken.

The default export runs once, when the scene is ready. Everything a plugin adds through `city` is removed when the plugin is disabled. If the plugin set something up on its own, such as a timer or a listener on `window`, it returns a function that undoes it, and the viewer calls that on disable.

Every plugin starts from this skeleton:

```js
// ~/.config/citydiff/plugins/<name>/plugin.js

export const api = 1;

export default function plugin(city) {
  return () => {};
}
```

<details>
<summary>Example</summary>

```js
export const api = 1;

export default function plugin(city) {
  const close = () => city.diff.close();
  window.addEventListener("blur", close);
  return () => window.removeEventListener("blur", close);
}
```

</details>

## Conventions

- **Reads are snapshots.** Every value `city` returns is a frozen copy. Changing it changes nothing in the viewer.
- **Everything added can be undone.** Each `add` and `onChange` returns a function that removes what it added. Disabling a plugin calls all of them.
- **Listeners take one object.** Each `onChange` listener receives a single object, so later versions can add fields without breaking plugins. A listener fires only when something actually changed, whether the user or a plugin changed it.
- **Errors stay inside the plugin.** If a `run`, `render`, `find`, `can` or listener throws, the error is reported and the viewer carries on. If the default export throws, the plugin is disabled.
- **The API only grows.** Members are added, never removed, renamed or changed in meaning. `city.version` goes up when members are added.

## city.version

```ts
city.version: number
```

The API version this viewer provides. The viewer has already checked it against the plugin's `api` export, so a plugin only reads it to use members a later version added.

<details>
<summary>Example</summary>

```js
export const api = 1;

export default function plugin(city) {
  if (city.version >= 2) {
    // use what version 2 added; version 1 viewers skip this
  }
}
```

</details>

## city.scene

The diff the city is drawn from: the packages, their imports, their declarations, and the calls inside each function. It doesn't change while the page is open.

### city.scene.packages

```ts
city.scene.packages: Package[]
```

Every package in the city, including external import targets (`kind: "external"`) and synthetic module roots. A package's declarations are in its `entities`.

<details>
<summary>Example</summary>

```js
// Every function or method whose body changed.
const bodies = city.scene.packages
  .flatMap((pkg) => pkg.entities)
  .filter((e) => e.part === "body" || e.part === "both");
```

</details>

### city.scene.module

```ts
city.scene.module: string
```

The module the city's root package is, e.g. `"citydiff"`. The root package's id is the same string. Empty when the range renames the module and the city has no single root.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/root",
  does: "select the root package",
  run: () => city.selection.select(city.scene.module),
});
```

</details>

### city.scene.range

```ts
city.scene.range: string
```

The commit range the city shows, as passed to `-range`, e.g. `"main..feature"`. Empty when citydiff was run on a single snapshot; then every `change` in the scene is `"same"`.

<details>
<summary>Example</summary>

```js
export default function plugin(city) {
  if (!city.scene.range) return; // nothing to review in a single snapshot
  // …
}
```

</details>

### city.scene.node(id)

```ts
city.scene.node(id: string): Node | null
```

Looks up a package, external import or declaration by its id. Returns `null` when no node has that id.

<details>
<summary>Example</summary>

```js
// The declaration main calls first.
const main = city.scene.node("cmd/cli/main.go#function#main");
const step = main?.calls.find((call) => call.to);
const callee = step ? city.scene.node(step.to) : null;
```

</details>

### city.scene.callers(id)

```ts
city.scene.callers(id: string): Entity[]
```

The functions and methods that call the declaration `id`. Returns an empty array for anything that isn't a function or method.

<details>
<summary>Example</summary>

```js
city.sidebar.add({
  title: "Changed callers",
  kinds: ["function", "method"],
  render(node) {
    const changed = city.scene.callers(node.id).filter((c) => c.change !== "same");
    return changed.length ? changed.map((c) => `- \`${c.name}\``).join("\n") : null;
  },
});
```

</details>

## city.selection

The selected node, and which of its arcs are drawn. The user changes it with a click, a search result, `esc`, `o` / `i` and `c`. A change made through this namespace lands in jump history and the address bar, exactly like the user's.

### city.selection.current

```ts
city.selection.current: Node | null
```

The selected node, or `null` when nothing is selected.

<details>
<summary>Example</summary>

```js
city.keys.add({
  key: "p",
  does: "select the selection's package",
  run() {
    const node = city.selection.current;
    if (node?.package) city.selection.select(node.package);
  },
});
```

</details>

### city.selection.refs

```ts
city.selection.refs: "calls" | "callers"
```

Which arcs the selection draws. `"calls"` shows what it uses: a function's calls, a package's imports. `"callers"` shows what uses it.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "flip arcs",
  can: () => (city.selection.current ? true : "select something first"),
  run: () => city.selection.setRefs(city.selection.refs === "calls" ? "callers" : "calls"),
});
```

</details>

### city.selection.select(id)

```ts
city.selection.select(id: string): boolean
```

Selects the node and flies the camera to it, the same as choosing a search result. Selecting a function or method also opens its call focus, the same as a click. Returns `false`, and changes nothing, when no node has that id.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/main",
  does: "go to main",
  run: () => city.selection.select("cmd/cli/main.go#function#main"),
});
```

</details>

### city.selection.clear()

```ts
city.selection.clear(): void
```

Deselects, closes call focus, and goes back to the whole city, the same as `esc`.

<details>
<summary>Example</summary>

```js
city.view.onChange(({ bird }) => {
  if (bird) city.selection.clear();
});
```

</details>

### city.selection.back()

```ts
city.selection.back(): boolean
```

Goes back one selection in jump history, the same as `o`. Returns `false` at the start of history.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/first",
  does: "back to the first selection",
  run() {
    while (city.selection.back()) {}
  },
});
```

</details>

### city.selection.forward()

```ts
city.selection.forward(): boolean
```

Goes forward one selection in jump history, the same as `i`. Returns `false` at the end of history.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/last",
  does: "forward to the latest selection",
  run() {
    while (city.selection.forward()) {}
  },
});
```

</details>

### city.selection.setRefs(dir)

```ts
city.selection.setRefs(dir: "calls" | "callers"): void
```

Switches which arcs the selection draws, the same as `c`.

<details>
<summary>Example</summary>

```js
// A removed declaration is interesting for who still called it.
city.selection.onChange(({ node }) => {
  if (node?.change === "removed") city.selection.setRefs("callers");
});
```

</details>

### city.selection.onChange(fn)

```ts
city.selection.onChange(fn: (e: { node: Node | null; refs: "calls" | "callers" }) => void): () => void
```

Calls `fn` after the selection or its refs change. Returns a function that stops the calls.

<details>
<summary>Example</summary>

```js
const stop = city.selection.onChange(({ node, refs }) => {
  console.log(node ? `${node.id} (${refs})` : "nothing selected");
});
// later, to stop listening:
stop();
```

</details>

## city.focus

Call focus is the view a function or method opens: the function and the calls it makes, in source order, each marked with what changed. The user opens it with a click or `enter` on a function and leaves it with `esc`.

### city.focus.current

```ts
city.focus.current: Entity | null
```

The function or method whose call focus is open, or `null`.

<details>
<summary>Example</summary>

```js
city.sidebar.add({
  title: "Unresolved calls",
  kinds: ["function", "method"],
  render(node) {
    if (city.focus.current?.id !== node.id) return null;
    const open = node.calls.filter((call) => !call.to);
    return open.length ? open.map((call) => `- \`${call.expr}\``).join("\n") : null;
  },
});
```

</details>

### city.focus.open(id)

```ts
city.focus.open(id: string): boolean
```

Selects the function or method and opens its call focus, the same as `enter`. Returns `false` when `id` isn't a function or method.

<details>
<summary>Example</summary>

```js
city.keys.add({
  key: "j",
  does: "into the first call that changed",
  run() {
    const step = city.focus.current?.calls.find((call) => call.to && call.change !== "same");
    if (step) city.focus.open(step.to);
  },
});
```

</details>

### city.focus.close()

```ts
city.focus.close(): void
```

Closes call focus. The function stays selected.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "leave focus",
  can: () => (city.focus.current ? true : "no call focus is open"),
  run: () => city.focus.close(),
});
```

</details>

### city.focus.onChange(fn)

```ts
city.focus.onChange(fn: (e: { node: Entity | null }) => void): () => void
```

Calls `fn` after call focus opens, closes or moves to another function. `node` is `null` when it closed. Returns a function that stops the calls.

<details>
<summary>Example</summary>

```js
// Show the diff right away for a function whose body changed.
city.focus.onChange(({ node }) => {
  if (node?.part === "body") city.diff.open(node.id);
});
```

</details>

## city.view

How the city is shown: overview or changes, bird view, and the camera. The user's keys for it are `1` / `2` / `m`, `y` and `0`.

### city.view.mode

```ts
city.view.mode: "overview" | "changes"
```

`"overview"` is the plain city. `"changes"` colours the diff and draws the dependency arcs.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "changes",
  can: () => (city.view.mode === "overview" ? true : "already showing changes"),
  run: () => city.view.setMode("changes"),
});
```

</details>

### city.view.bird

```ts
city.view.bird: boolean
```

Whether bird view is on: the city seen from straight above, merged into districts.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/map",
  does: "toggle bird view",
  run: () => city.view.setBird(!city.view.bird),
});
```

</details>

### city.view.setMode(mode)

```ts
city.view.setMode(mode: "overview" | "changes"): void
```

Switches between the plain city and the diff, the same as `1` and `2`.

<details>
<summary>Example</summary>

```js
export default function plugin(city) {
  // Open every range straight on its changes.
  if (city.scene.range) city.view.setMode("changes");
}
```

</details>

### city.view.setBird(on)

```ts
city.view.setBird(on: boolean): void
```

Turns bird view on or off, the same as `y`.

<details>
<summary>Example</summary>

```js
city.selection.onChange(({ node }) => {
  if (node) city.view.setBird(false);
});
```

</details>

### city.view.resetCamera()

```ts
city.view.resetCamera(): void
```

Frames the whole city again, the same as `0`. The selection stays.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/zoomout",
  does: "see the whole city, keep the selection",
  run: () => city.view.resetCamera(),
});
```

</details>

### city.view.onChange(fn)

```ts
city.view.onChange(fn: (e: { mode: "overview" | "changes"; bird: boolean }) => void): () => void
```

Calls `fn` after the mode or bird view changes. Returns a function that stops the calls.

<details>
<summary>Example</summary>

```js
city.view.onChange(({ bird }) => city.sidebar.show(!bird));
```

</details>

## city.sidebar

The left sidebar, which shows the city's or the selected node's detail. The user shows and hides it with `b`.

### city.sidebar.shown

```ts
city.sidebar.shown: boolean
```

Whether the sidebar is showing.

<details>
<summary>Example</summary>

```js
city.focus.onChange(({ node }) => {
  if (node && !city.sidebar.shown) city.sidebar.show(true);
});
```

</details>

### city.sidebar.show(open)

```ts
city.sidebar.show(open: boolean): void
```

Shows or hides the sidebar, the same as `b`.

<details>
<summary>Example</summary>

```js
city.keys.add({ key: "B", does: "hide the sidebar", run: () => city.sidebar.show(false) });
```

</details>

### city.sidebar.onChange(fn)

```ts
city.sidebar.onChange(fn: (e: { shown: boolean }) => void): () => void
```

Calls `fn` after the sidebar is shown or hidden. Returns a function that stops the calls.

<details>
<summary>Example</summary>

```js
city.sidebar.onChange(({ shown }) => {
  if (!shown) city.view.resetCamera();
});
```

</details>

### city.sidebar.add(section)

```ts
city.sidebar.add(section: Section): () => void

interface Section {
  title: string;                       // the section heading
  kinds?: Kind[];                      // node kinds it appears for; every kind when omitted
  render(node: Node): string | null;   // Markdown for this node, or null to leave the section out
}
```

Adds a section to the selected node's detail. `render` runs when the detail is drawn for a node of one of `kinds`. Its Markdown goes through the same renderer as tour notes: paragraphs, bold, italic, code, lists, headings and http(s) links. Anything else is shown as plain text.

<details>
<summary>Example</summary>

```js
city.sidebar.add({
  title: "Calls changed",
  kinds: ["function", "method"],
  render(node) {
    const added = node.calls.filter((call) => call.change === "added").length;
    const removed = node.calls.filter((call) => call.change === "removed").length;
    return added || removed ? `**+${added}** added, **−${removed}** removed` : null;
  },
});
```

</details>

## city.search

The search box, which matches packages and declarations by name. Queries that start with `/` are commands and never reach a search source.

### city.search.open(query)

```ts
city.search.open(query?: string): boolean
```

Opens the search box with `query` already typed, the same as pressing `/` and typing it. The results are ranked exactly as for a typed query: the viewer's own matches together with every plugin's search sources. The user picks one, or keeps typing. Without `query`, the box opens empty. A query that starts with `/` lists the matching commands.

This is how a plugin hands the user a query that came from somewhere other than the keyboard, such as speech, when it can't pick the one right node itself.

Returns `false`, and opens nothing, while the diff panel or the editor is open.

<details>
<summary>Example</summary>

```js
// f lists everything named like the selection, across all packages.
city.keys.add({
  key: "f",
  does: "search for the selection's name",
  run() {
    const node = city.selection.current;
    if (node) city.search.open(node.name);
  },
});
```

</details>

### city.search.add(source)

```ts
city.search.add(source: SearchSource): () => void

interface SearchSource {
  find(query: string): Hit[];   // runs on every keystroke, so it must be fast and synchronous
}

interface Hit {
  id: string;      // an existing node
  note?: string;   // shown beside the hit, where the package path usually is
}
```

Adds hits to the search box. They're listed with the viewer's own matches, and choosing one selects the node like any other result.

<details>
<summary>Example</summary>

```js
// Typing a file name lists the declarations in it.
const decls = city.scene.packages.flatMap((pkg) => pkg.entities);

city.search.add({
  find: (query) =>
    decls
      .filter((e) => e.file.endsWith(query))
      .slice(0, 8)
      .map((e) => ({ id: e.id, note: e.file })),
});
```

</details>

## city.commands

Commands in the search box: typing `/` lists them, `↑` / `↓` and `tab` walk the list, and `enter` runs one.

### city.commands.add(command)

```ts
city.commands.add(command: Command): () => void

interface Command {
  name: string;              // starts with "/"; a name already in use throws
  does: string;              // shown beside the name in the list
  run(arg: string): void;    // arg is the text typed after the name, trimmed; "" when none
  hidden?: boolean;          // left out of the list and run only when typed in full, like /launch
}
```

Adds a command. Everything typed after the name and a space is its argument.

<details>
<summary>Example</summary>

```js
// /callers openBrowser selects openBrowser and shows who calls it.
const decls = city.scene.packages.flatMap((pkg) => pkg.entities);

city.commands.add({
  name: "/callers",
  does: "select a declaration by name and show its callers",
  run(arg) {
    const decl = decls.find((e) => e.name === arg);
    if (decl && city.selection.select(decl.id)) city.selection.setRefs("callers");
  },
});
```

</details>

## city.keys

The viewer's keyboard, and the `?` legend that lists it.

### city.keys.add(binding)

```ts
city.keys.add(binding: Binding): () => void

interface Binding {
  key: string;     // a KeyboardEvent.key value, e.g. "n" or "N"; a key already in use throws
  does: string;    // the line in the ? legend
  run(): void;
}
```

Adds a key. It works whenever the viewer's own keys do: not while typing in the search box, and not while the diff panel or the editor is open. It's listed in the `?` legend.

<details>
<summary>Example</summary>

```js
city.keys.add({
  key: "g",
  does: "edit what the focused function calls first",
  run() {
    const step = city.focus.current?.calls.find((call) => call.to);
    if (step) city.editor.open(step.to);
  },
});
```

</details>

## city.ring

The action ring: hold the right mouse button on the city, or the sheet's handle on a touch screen. It holds the actions that have no button or gesture of their own, so they're reachable without a keyboard.

### city.ring.add(item)

```ts
city.ring.add(item: RingItem): () => void

interface RingItem {
  label: string;
  run(): void;
  can?(): true | string;   // true when the item can act; otherwise the reason, shown above the ring
}
```

Adds an item to the ring. An item that can't act lights at half strength, and the line above the ring says why. Adding a plugin's key action here as well is what makes it work on a phone.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "callers",
  can: () => (city.selection.current ? true : "select something first"),
  run: () => city.selection.setRefs("callers"),
});
```

</details>

## city.diff

The diff panel: `D` opens the git diff of the selected node over the city. For a declaration it shows only that declaration's own hunks, from its line to the next declaration in the file. For a package it shows the files in its directory. Each hunk has its `@@` line, then the code with old and new line numbers, `+` / `−` and syntax highlighting. `esc` closes it, and the city's keys are off while it's open.

### city.diff.current

```ts
city.diff.current: Node | null
```

The node whose diff panel is open, or `null` when it's closed.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "close diff",
  can: () => (city.diff.current ? true : "no diff is open"),
  run: () => city.diff.close(),
});
```

</details>

### city.diff.files(id)

```ts
city.diff.files(id: string): Promise<DiffFile[]>
```

The diff of a node without opening the panel: the same files and hunks `D` would show. For a package, at most 8 files. Rejects when there's no range or the server can't build the diff.

<details>
<summary>Example</summary>

```js
// /biggest selects the declaration in the selected package with the most added lines.
city.commands.add({
  name: "/biggest",
  does: "select the biggest addition in this package",
  async run() {
    const pkg = city.selection.current;
    if (pkg?.kind !== "package") return;
    let best = null;
    let most = 0;
    for (const e of pkg.entities.filter((e) => e.change !== "same")) {
      const files = await city.diff.files(e.id);
      const added = files
        .flatMap((f) => f.hunks)
        .flatMap((h) => h.lines)
        .filter((line) => line.kind === "add").length;
      if (added > most) [best, most] = [e, added];
    }
    if (best) city.selection.select(best.id);
  },
});
```

</details>

### city.diff.open(id)

```ts
city.diff.open(id?: string): Promise<boolean>
```

Opens the diff panel, the same as `D`. Without `id`, it opens for the selection. Resolves to `true` once the panel is up, and to `false` when there's nothing to open: nothing selected, or an external import.

<details>
<summary>Example</summary>

```js
city.commands.add({
  name: "/pkgdiff",
  does: "diff of the whole package of the selection",
  run() {
    const node = city.selection.current;
    if (node) city.diff.open(node.package ?? node.id);
  },
});
```

</details>

### city.diff.close()

```ts
city.diff.close(): void
```

Closes the diff panel, the same as `esc` while it's open.

<details>
<summary>Example</summary>

```js
export default function plugin(city) {
  const close = () => city.diff.close();
  window.addEventListener("blur", close);
  return () => window.removeEventListener("blur", close);
}
```

</details>

### city.diff.onChange(fn)

```ts
city.diff.onChange(fn: (e: { node: Node | null }) => void): () => void
```

Calls `fn` after the diff panel opens or closes. `node` is `null` when it closed. Returns a function that stops the calls.

<details>
<summary>Example</summary>

```js
// Remember which diffs were read, and say so in the sidebar.
const read = new Set();
city.diff.onChange(({ node }) => {
  if (node) read.add(node.id);
});
city.sidebar.add({
  title: "Diff",
  render: (node) => (read.has(node.id) ? "diff read" : null),
});
```

</details>

### city.diff.add(action)

```ts
city.diff.add(action: DiffAction): () => void

interface DiffAction {
  label: string;
  run(e: { node: Node; file: string; hunk: Hunk }): void;   // file is the path of the hunk's file
}
```

Adds a button to each hunk's `@@` line in the diff panel.

<details>
<summary>Example</summary>

```js
city.diff.add({
  label: "edit here",
  run({ file, hunk }) {
    city.diff.close();
    city.editor.open({ file, line: hunk.newStart });
  },
});
```

</details>

## city.editor

The editor: `E` opens nvim in a terminal over the city.

### city.editor.open(target)

```ts
city.editor.open(target: string | { file: string; line?: number; col?: number }): boolean
```

Opens nvim, the same as `E`. A node id opens a declaration at its name, or a package at its first file. A declaration the range removed opens its file at the top. `{ file, line, col }` opens that file at that place. Returns `false` when there's nothing on disk to open, such as an external import.

<details>
<summary>Example</summary>

```js
city.ring.add({
  label: "edit",
  can: () => (city.selection.current ? true : "select something first"),
  run: () => city.editor.open(city.selection.current.id),
});
```

</details>

## Types

### Node

```ts
type Node = Package | Entity
```

Anything that can be selected. `kind` tells the two apart: `"package"` and `"external"` are packages, everything else is a declaration.

<details>
<summary>Example</summary>

```js
const isPackage = (node) => node.kind === "package" || node.kind === "external";
```

</details>

### Package

```ts
interface Package {
  kind: "package" | "external";
  id: string;           // the import path, e.g. "citydiff/lib/diff"
  name: string;
  dir: string;          // its directory in the snapshot; "" for an external import
  parent: string;       // the id of the package it stands on; "" for the root
  synthetic: boolean;   // a module root that only holds other packages
  change: Change;
  entities: Entity[];   // its declarations; empty for an external import
  deps: Dep[];          // its imports
}
```

### Entity

```ts
interface Entity {
  kind: "type" | "function" | "method" | "variable";
  id: string;                        // "file#kind#name", e.g. "cmd/cli/browser.go#function#openBrowser"
  name: string;
  recv: string;                      // a method's receiver type; "" otherwise
  package: string;                   // the id of its package
  file: string;
  line: number;                      // where the name is written; 0 when unknown
  col: number;
  parent: string;                    // a method's type, when that type is in the same package
  change: Change;
  part: "signature" | "body" | "both" | "";   // what changed in a modified function or method
  fields: string[];                  // a type's fields
  bodyBytes: number;                 // body size after the change
  bodyBytesBefore: number | null;    // body size before; null when it didn't exist
  calls: CallStep[];                 // the calls in its body, in source order
}
```

### Dep

```ts
interface Dep {
  to: string;          // the id of the imported package
  change: Change;      // whether the range added, removed or kept this import
  external: boolean;   // the imported package is outside the snapshot
  files: number;       // how many of the package's files import it
}
```

### CallStep

```ts
interface CallStep {
  expr: string;         // the call as written, without arguments, e.g. "git.Patch"
  change: Change;       // whether the range added, removed or kept this call
  to: string | null;    // the called declaration's id; null when it isn't in the snapshot
}
```

### Change

```ts
type Change = "same" | "added" | "removed" | "modified"
```

### Kind

```ts
type Kind = "package" | "external" | "type" | "function" | "method" | "variable"
```

### DiffFile

```ts
interface DiffFile {
  path: string;
  lang: string;       // the language the highlighter uses, e.g. "go"
  binary: boolean;    // a binary file has no hunks
  hunks: Hunk[];
  omitted: number;    // hunks in this file that belong to other declarations
}
```

### Hunk

```ts
interface Hunk {
  oldStart: number;   // first line in the old file
  oldLines: number;
  newStart: number;   // first line in the new file
  newLines: number;
  lines: Line[];
}
```

### Line

```ts
interface Line {
  kind: "context" | "add" | "del";
  old: number;    // line number in the old file; 0 for an added line
  new: number;    // line number in the new file; 0 for a deleted line
  text: string;
}
```

## Not yet

- **Refreshing a section.** A sidebar section is drawn again only when the detail is, on the next selection for one. A plugin whose own state changed, like a review checklist, cannot ask for a redraw yet.
- **Turning a plugin off.** A plugin is on while its folder is in the plugins directory. There is no command in the page to turn one off.
