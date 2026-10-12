import { test } from "node:test";
import assert from "node:assert/strict";
import { API_VERSION, apiNodes, createPlugins } from "./plugins.js";

const doc = {
  packages: [
    {
      id: "m/cli",
      name: "cli",
      dir: "cli",
      parent: "m",
      change: "modified",
      entities: [
        {
          id: "cli/main.go#function#main",
          kind: "function",
          name: "main",
          file: "cli/main.go",
          line: 3,
          change: "modified",
          part: "body",
          bodyBytes: 40,
          bodyBytesBefore: 30,
          calls: [
            { expr: "run", change: "added", resolved: true, path: "cli/main.go", name: "run" },
            { expr: "fmt.Println", change: "same" },
          ],
        },
        { id: "cli/main.go#function#run", kind: "function", name: "run", file: "cli/main.go", line: 9, change: "added" },
      ],
      deps: [{ to: "fmt", change: "same", external: true, files: 1 }],
    },
    { id: "fmt", name: "fmt", external: true, change: "same" },
  ],
};

// The host's own call lookup, here by name.
const resolve = (step) => "cli/main.go#function#" + step.name;

function setup(mods = {}) {
  const { packages, byId } = apiNodes(doc, resolve);
  const state = {
    selection: { node: null, refs: "calls" },
    focus: { node: null },
    view: { mode: "overview", bird: false },
    sidebar: { shown: true },
    diff: { node: null },
  };
  const log = [];
  const host = {
    scene: { packages, module: "m", range: "a..b" },
    node: (id) => byId.get(id) || null,
    callers: (id) => (id === "cli/main.go#function#run" ? [byId.get("cli/main.go#function#main")] : []),
    state: () => structuredClone(state),
    selection: {
      select(id) {
        if (!byId.has(id)) return false;
        state.selection.node = id;
        return true;
      },
      clear() { state.selection.node = null; },
      back: () => false,
      forward: () => false,
      setRefs(dir) { state.selection.refs = dir; },
    },
    focus: { open: () => false, close() {} },
    view: {
      setMode(mode) { state.view.mode = mode; },
      setBird(on) { state.view.bird = on; },
      resetCamera() { log.push("reset"); },
    },
    sidebar: { show(open) { state.sidebar.shown = open; } },
    search: { open(query) { log.push("search " + query); return true; } },
    diff: { files: async () => [], open: async () => true, close() {} },
    editor: { open(target) { log.push(["edit", target]); return true; } },
    takenKeys: new Set(["/", "b", "?"]),
    takenCommands: new Set(["/skin", "/launch"]),
    changed: (what) => log.push("changed " + what),
    report: (name, err) => log.push("report " + name + ": " + err.message),
  };
  const plugins = createPlugins(host);
  const load = (names) => plugins.load(names, async (name) => {
    if (!(name in mods)) throw new Error("no such module");
    return mods[name];
  });
  return { plugins, host, state, log, load };
}

const plugin = (fn, api = 1) => ({ api, default: fn });

test("a plugin is handed the API, and what it adds shows up in the viewer's lists", async () => {
  let city = null;
  const { plugins, load } = setup({
    hello: plugin((c) => {
      city = c;
      c.commands.add({ name: "/Hello", does: "greet", run: () => {} });
      c.keys.add({ key: "n", does: "next", run: () => {} });
      c.ring.add({ label: "next", run: () => {} });
      c.sidebar.add({ title: "Hello", render: (node) => "Hello, `" + node.name + "`!" });
      c.search.add({ find: () => [{ id: "fmt", note: "outside" }] });
      c.diff.add({ label: "edit here", run: () => {} });
    }),
  });
  await load(["hello"]);
  assert.equal(city.version, API_VERSION);
  assert.equal(city.scene.module, "m");
  assert.equal(city.scene.range, "a..b");
  assert.deepEqual(plugins.names(), ["hello"]);
  // A command's name is matched the way the search box matches: lower case.
  assert.deepEqual(plugins.commands().map((c) => c.name), ["/hello"]);
  assert.equal(plugins.key("n").does, "next");
  assert.equal(plugins.key("x"), null);
  assert.equal(plugins.ring()[0].label, "next");
  assert.match(plugins.ring()[0].id, /^plugin-\d+$/);
  assert.deepEqual(plugins.sections(city.scene.node("m/cli")), [{ title: "Hello", markdown: "Hello, `cli`!" }]);
  assert.deepEqual(plugins.hits("f"), [{ id: "fmt", note: "outside" }]);
  assert.equal(plugins.diffActions()[0].label, "edit here");
});

test("each add gives back an undo, and the viewer hears what came and went", async () => {
  let undo = null;
  const { plugins, log, load } = setup({
    a: plugin((c) => { undo = c.keys.add({ key: "n", does: "next", run: () => {} }); }),
  });
  await load(["a"]);
  assert.ok(plugins.key("n"));
  undo();
  undo();
  assert.equal(plugins.key("n"), null);
  assert.deepEqual(log.filter((line) => line.startsWith("changed")), ["changed keys", "changed keys"]);
});

test("a taken key or command throws, and a throw in the default export turns the plugin off", async () => {
  const { plugins, log, load } = setup({
    greedy: plugin((c) => {
      c.keys.add({ key: "n", does: "next", run: () => {} });
      c.keys.add({ key: "b", does: "mine now", run: () => {} });
    }),
    polite: plugin((c) => {
      assert.throws(() => c.commands.add({ name: "/skin", run: () => {} }), /\/skin is taken/);
      assert.throws(() => c.commands.add({ name: "skin", run: () => {} }), /a slash/);
      c.keys.add({ key: "n", does: "next", run: () => {} });
    }),
  });
  await load(["greedy", "polite"]);
  assert.ok(log.includes('report greedy: city.keys.add: "b" is taken'));
  // greedy is off, so its n was taken back and polite could have it.
  assert.deepEqual(plugins.names(), ["polite"]);
  assert.equal(plugins.keys().length, 1);
});

test("a plugin that cannot run is reported and skipped", async () => {
  const { plugins, log, load } = setup({
    later: plugin(() => {}, API_VERSION + 1),
    noapi: { default: () => {} },
    nofn: { api: 1 },
  });
  await load(["later", "noapi", "nofn", "missing"]);
  assert.deepEqual(plugins.names(), []);
  assert.deepEqual(log, [
    `report later: needs plugin API ${API_VERSION + 1}; this viewer has ${API_VERSION}`,
    "report noapi: plugin.js must export api, the API version it needs",
    "report nofn: plugin.js must export a default function",
    "report missing: no such module",
  ]);
});

test("a callback that throws is reported, and the rest still run", async () => {
  const { plugins, log, load } = setup({
    broken: plugin((c) => {
      c.sidebar.add({ title: "Broken", render: () => { throw new Error("boom"); } });
      c.sidebar.add({ title: "Fine", kinds: ["function"], render: () => "fine" });
      c.ring.add({ label: "x", run: () => {}, can: () => { throw new Error("no"); } });
    }),
  });
  await load(["broken"]);
  const main = { kind: "function", name: "main" };
  assert.deepEqual(plugins.sections(main), [{ title: "Fine", markdown: "fine" }]);
  assert.deepEqual(plugins.sections({ kind: "package" }), []);
  assert.equal(plugins.ring()[0].can(), "broken failed");
  assert.ok(log.includes("report broken: boom"));
  assert.ok(log.includes("report broken: no"));
  assert.deepEqual(plugins.names(), ["broken"]);
});

test("listeners hear a change once, after publish, with the node itself", async () => {
  const heard = [];
  let city = null;
  const { plugins, load } = setup({
    watch: plugin((c) => {
      city = c;
      c.selection.onChange((e) => heard.push(["selection", e.node && e.node.id, e.refs]));
      c.view.onChange((e) => heard.push(["view", e.mode, e.bird]));
    }),
  });
  await load(["watch"]);
  assert.equal(city.selection.select("cli/main.go#function#main"), true);
  assert.equal(city.selection.select("nowhere"), false);
  city.selection.setRefs("callers");
  assert.deepEqual(heard, []);
  plugins.publish();
  assert.deepEqual(heard, [["selection", "cli/main.go#function#main", "callers"]]);
  plugins.publish();
  assert.equal(heard.length, 1);
  city.view.setMode("changes");
  assert.throws(() => city.view.setMode("overlay"), /"overview" or "changes"/);
  plugins.publish();
  assert.deepEqual(heard[1], ["view", "changes", false]);
  assert.equal(city.selection.current.name, "main");
  assert.equal(city.view.mode, "changes");
});

test("disabling a plugin takes back what it added, runs its undo, and leaves its city inert", async () => {
  let city = null;
  let undone = 0;
  const heard = [];
  const { plugins, load } = setup({
    temp: plugin((c) => {
      city = c;
      c.keys.add({ key: "n", does: "next", run: () => {} });
      c.selection.onChange(() => heard.push("heard"));
      return () => { undone++; };
    }),
  });
  await load(["temp"]);
  plugins.disable("temp");
  plugins.disable("temp");
  assert.equal(undone, 1);
  assert.equal(plugins.key("n"), null);
  assert.deepEqual(plugins.names(), []);
  // A timer the plugin forgot cannot come back in.
  city.keys.add({ key: "n", does: "next", run: () => {} });
  assert.equal(plugins.key("n"), null);
  assert.equal(city.selection.select("fmt"), false);
  plugins.publish();
  assert.deepEqual(heard, []);
});

test("an async default export that rejects turns the plugin off", async () => {
  const { plugins, log, load } = setup({
    slow: plugin(async (c) => {
      c.keys.add({ key: "n", does: "next", run: () => {} });
      await Promise.resolve();
      throw new Error("model missing");
    }),
  });
  await load(["slow"]);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.ok(log.includes("report slow: model missing"));
  assert.equal(plugins.key("n"), null);
});

test("nodes are frozen copies with their package and where each call goes", () => {
  const { byId, packages } = apiNodes(doc, resolve);
  const main = byId.get("cli/main.go#function#main");
  assert.equal(main.package, "m/cli");
  assert.deepEqual(main.calls.map((c) => c.to), ["cli/main.go#function#run", null]);
  assert.equal(main.bodyBytesBefore, 30);
  assert.equal(byId.get("cli/main.go#function#run").bodyBytesBefore, null);
  assert.equal(byId.get("fmt").kind, "external");
  assert.deepEqual(byId.get("fmt").entities, []);
  assert.equal(packages[0].kind, "package");
  assert.ok(Object.isFrozen(main) && Object.isFrozen(main.calls) && Object.isFrozen(packages));
  assert.throws(() => { main.name = "other"; }, TypeError);
});

test("search hits are existing nodes, each once", async () => {
  const { plugins, load } = setup({
    a: plugin((c) => c.search.add({ find: (q) => [{ id: "fmt" }, { id: "nowhere" }, { id: q }] })),
    b: plugin((c) => c.search.add({ find: () => [{ id: "fmt", note: "again" }, "junk", null] })),
  });
  await load(["a", "b"]);
  assert.deepEqual(plugins.hits("m/cli"), [{ id: "fmt", note: "" }, { id: "m/cli", note: "" }]);
});

test("a diff button gets the node, the file and a frozen copy of the hunk", async () => {
  const got = [];
  const { plugins, load } = setup({
    d: plugin((c) => c.diff.add({ label: "edit here", run: (e) => got.push(e) })),
  });
  await load(["d"]);
  const hunk = { oldStart: 1, oldLines: 1, newStart: 1, newLines: 2, lines: [{ kind: "add", old: 0, new: 1, text: "x" }] };
  plugins.runDiffAction(plugins.diffActions()[0], "cli/main.go#function#main", "cli/main.go", hunk);
  assert.equal(got[0].node.name, "main");
  assert.equal(got[0].file, "cli/main.go");
  assert.deepEqual(got[0].hunk, hunk);
  assert.notEqual(got[0].hunk, hunk);
  assert.ok(Object.isFrozen(got[0].hunk.lines[0]));
});

test("the editor takes a node id or a file and place", async () => {
  let city = null;
  const { log, load } = setup({ e: plugin((c) => { city = c; }) });
  await load(["e"]);
  city.editor.open("cli/main.go#function#main");
  city.editor.open({ file: "cli/main.go", line: 9, col: 0, extra: true });
  assert.throws(() => city.editor.open({ line: 3 }), /node id or \{ file, line, col \}/);
  assert.deepEqual(log.filter((x) => Array.isArray(x)), [
    ["edit", "cli/main.go#function#main"],
    ["edit", { file: "cli/main.go", line: 9 }],
  ]);
});
