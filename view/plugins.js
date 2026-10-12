// Plugins: JavaScript modules from the plugins directory that add behaviour on
// top of the viewer's own features (PLUGINS.md). This module is everything
// about them that is not the page: the `city` object each plugin is handed,
// the checks on what a plugin passes in, the lists of what each one added, and
// the wall that keeps a plugin's mistake from becoming the viewer's.
//
// main.js is the host. It answers what is on screen (the selection, the
// mode), does what a user could do (select this, open that diff), and draws
// what plugins added where its own features draw: the commands with its
// commands, the keys in its legend, the ring items on its ring. Everything a
// host hands over or takes back is by id; the nodes a plugin sees are the
// host's frozen copies, so changing one changes nothing.

export const API_VERSION = 1;

const KINDS = ["package", "external", "type", "function", "method", "variable"];
const COMMAND_NAME = /^\/[A-Za-z0-9][A-Za-z0-9_-]*$/;
const MAX_HITS = 12;

// need throws the TypeError a plugin sees for a wrong argument. The message
// names the call, so the note the viewer shows says what to fix.
function need(ok, message) {
  if (!ok) throw new TypeError(message);
}

const isText = (value) => typeof value === "string";
const isFn = (value) => typeof value === "function";

function freezeDeep(value) {
  if (value && typeof value === "object" && !Object.isFrozen(value)) {
    Object.freeze(value);
    for (const key of Object.keys(value)) freezeDeep(value[key]);
  }
  return value;
}

// createPlugins makes the plugin registry over a host. The host is:
//
//   scene                          { packages, module, range }, frozen
//   node(id), callers(id)          frozen nodes, or null and []
//   state()                        { selection: { node, refs }, focus: { node },
//                                    view: { mode, bird }, sidebar: { shown },
//                                    diff: { node } }, nodes by id
//   selection, focus, view,        the acts, one function per API member,
//   sidebar, search, diff, editor  taking and returning ids
//   takenKeys, takenCommands       Sets of what the viewer itself answers to
//   changed(what)                  something plugins added came or went:
//                                  "commands" "keys" "ring" "sections" "search" "diff"
//   report(name, error)            a plugin failed; the viewer carries on
export function createPlugins(host) {
  const added = { commands: [], keys: [], ring: [], sections: [], search: [], diff: [] };
  const listeners = { selection: [], focus: [], view: [], sidebar: [], diff: [] };
  const loaded = [];
  let last = null;
  let seq = 0;

  function report(plugin, err) {
    host.report(plugin.name, err instanceof Error ? err : new Error(String(err)));
  }

  // guard wraps a plugin's callback: once the plugin is off it no longer
  // runs, and what it throws, or the promise it returns rejects with, is
  // reported instead of reaching the viewer's own code.
  function guard(plugin, fn, fallback) {
    return (...args) => {
      if (!plugin.on) return fallback;
      try {
        const out = fn(...args);
        if (out && isFn(out.then)) out.then(null, (err) => report(plugin, err));
        return out;
      } catch (err) {
        report(plugin, err);
        return fallback;
      }
    };
  }

  // track keeps entry in list until the returned undo runs. The plugin keeps
  // every undo too, so disabling it takes all of them back.
  function track(plugin, list, entry, what) {
    list.push(entry);
    if (what) host.changed(what);
    let done = false;
    const undo = () => {
      if (done) return;
      done = true;
      const i = list.indexOf(entry);
      if (i >= 0) list.splice(i, 1);
      if (what) host.changed(what);
    };
    plugin.undo.push(undo);
    return undo;
  }

  // cityFor is the API one plugin is handed (PLUGINS.md). Each member checks
  // what it is passed and then asks the host. Once the plugin is off, every
  // member does nothing: a timer the plugin forgot cannot add a key later.
  function cityFor(plugin) {
    const live = (fn, fallback) => (...args) => (plugin.on ? fn(...args) : fallback);
    const node = (id) => (id == null ? null : host.node(id));
    const noop = () => {};
    const listen = (ns) => live((fn) => {
      need(isFn(fn), `city.${ns}.onChange takes a function`);
      return track(plugin, listeners[ns], { plugin, fn: guard(plugin, fn) }, null);
    }, noop);

    const scene = Object.freeze({
      packages: host.scene.packages,
      module: host.scene.module,
      range: host.scene.range,
      node: (id) => node(String(id)),
      callers: (id) => host.callers(String(id)),
    });

    const selection = Object.freeze({
      get current() { return node(host.state().selection.node); },
      get refs() { return host.state().selection.refs; },
      select: live((id) => host.selection.select(String(id)), false),
      clear: live(() => { host.selection.clear(); }),
      back: live(() => host.selection.back(), false),
      forward: live(() => host.selection.forward(), false),
      setRefs: live((dir) => {
        need(dir === "calls" || dir === "callers", 'city.selection.setRefs takes "calls" or "callers"');
        host.selection.setRefs(dir);
      }),
      onChange: listen("selection"),
    });

    const focus = Object.freeze({
      get current() { return node(host.state().focus.node); },
      open: live((id) => host.focus.open(String(id)), false),
      close: live(() => { host.focus.close(); }),
      onChange: listen("focus"),
    });

    const view = Object.freeze({
      get mode() { return host.state().view.mode; },
      get bird() { return host.state().view.bird; },
      setMode: live((mode) => {
        need(mode === "overview" || mode === "changes", 'city.view.setMode takes "overview" or "changes"');
        host.view.setMode(mode);
      }),
      setBird: live((on) => { host.view.setBird(!!on); }),
      resetCamera: live(() => { host.view.resetCamera(); }),
      onChange: listen("view"),
    });

    const sidebar = Object.freeze({
      get shown() { return host.state().sidebar.shown; },
      show: live((open) => { host.sidebar.show(!!open); }),
      onChange: listen("sidebar"),
      add: live((section) => {
        need(section && isText(section.title) && section.title !== "", "city.sidebar.add needs a title");
        need(isFn(section.render), "city.sidebar.add needs render(node)");
        const kinds = section.kinds == null ? null : section.kinds;
        need(kinds === null || (Array.isArray(kinds) && kinds.every((k) => KINDS.includes(k))),
          "city.sidebar.add: kinds is a list of " + KINDS.join(", "));
        return track(plugin, added.sections, {
          plugin,
          title: section.title,
          kinds: kinds && new Set(kinds),
          render: guard(plugin, section.render, null),
        }, "sections");
      }, noop),
    });

    const search = Object.freeze({
      open: live((query) => host.search.open(query == null ? "" : String(query)), false),
      add: live((source) => {
        need(source && isFn(source.find), "city.search.add needs find(query)");
        return track(plugin, added.search, { plugin, find: guard(plugin, source.find, []) }, "search");
      }, noop),
    });

    const commands = Object.freeze({
      add: live((command) => {
        need(command && isText(command.name) && COMMAND_NAME.test(command.name),
          "city.commands.add needs a name like /name: a slash, then letters, digits, _ and -");
        need(isFn(command.run), "city.commands.add needs run(arg)");
        const name = command.name.toLowerCase();
        need(!host.takenCommands.has(name) && !added.commands.some((c) => c.name === name),
          `city.commands.add: ${name} is taken`);
        return track(plugin, added.commands, {
          plugin,
          name,
          does: isText(command.does) ? command.does : "",
          hidden: !!command.hidden,
          run: guard(plugin, command.run),
        }, "commands");
      }, noop),
    });

    const keys = Object.freeze({
      add: live((binding) => {
        need(binding && isText(binding.key) && binding.key !== "", "city.keys.add needs a key");
        need(isFn(binding.run), "city.keys.add needs run()");
        const key = binding.key;
        need(!host.takenKeys.has(key) && !added.keys.some((k) => k.key === key), `city.keys.add: ${JSON.stringify(key)} is taken`);
        return track(plugin, added.keys, {
          plugin,
          key,
          does: isText(binding.does) ? binding.does : "",
          run: guard(plugin, binding.run),
        }, "keys");
      }, noop),
    });

    const ring = Object.freeze({
      add: live((item) => {
        need(item && isText(item.label) && item.label !== "", "city.ring.add needs a label");
        need(isFn(item.run), "city.ring.add needs run()");
        need(item.can == null || isFn(item.can), "city.ring.add: can is a function");
        return track(plugin, added.ring, {
          plugin,
          id: "plugin-" + ++seq,
          label: item.label,
          run: guard(plugin, item.run),
          can: item.can ? guard(plugin, item.can, plugin.name + " failed") : () => true,
        }, "ring");
      }, noop),
    });

    const diff = Object.freeze({
      get current() { return node(host.state().diff.node); },
      files: live((id) => host.diff.files(String(id)), Promise.resolve([])),
      open: live((id) => host.diff.open(id == null ? null : String(id)), Promise.resolve(false)),
      close: live(() => { host.diff.close(); }),
      onChange: listen("diff"),
      add: live((action) => {
        need(action && isText(action.label) && action.label !== "", "city.diff.add needs a label");
        need(isFn(action.run), "city.diff.add needs run({ node, file, hunk })");
        return track(plugin, added.diff, { plugin, label: action.label, run: guard(plugin, action.run) }, "diff");
      }, noop),
    });

    const editor = Object.freeze({
      open: live((target) => {
        if (target && typeof target === "object") {
          need(isText(target.file) && target.file !== "", "city.editor.open needs a node id or { file, line, col }");
          const out = { file: target.file };
          if (Number.isInteger(target.line) && target.line > 0) out.line = target.line;
          if (Number.isInteger(target.col) && target.col > 0) out.col = target.col;
          return host.editor.open(out);
        }
        return host.editor.open(String(target));
      }, false),
    });

    return Object.freeze({
      version: API_VERSION,
      scene, selection, focus, view, sidebar, search, commands, keys, ring, diff, editor,
    });
  }

  // load imports each named plugin, in order, and runs it. A plugin that
  // cannot be imported, needs a later API, or exports no function is reported
  // and skipped; one whose default export throws is reported and turned off.
  async function load(names, importer) {
    last = host.state();
    for (const name of names) {
      const plugin = { name, on: true, undo: [], dispose: null };
      let mod;
      try {
        mod = await importer(name);
      } catch (err) {
        report(plugin, err);
        continue;
      }
      const api = mod && mod.api;
      if (!Number.isInteger(api) || api < 1) {
        report(plugin, new Error("plugin.js must export api, the API version it needs"));
        continue;
      }
      if (api > API_VERSION) {
        report(plugin, new Error(`needs plugin API ${api}; this viewer has ${API_VERSION}`));
        continue;
      }
      if (!isFn(mod.default)) {
        report(plugin, new Error("plugin.js must export a default function"));
        continue;
      }
      loaded.push(plugin);
      const keep = (fn) => {
        if (!isFn(fn)) return;
        if (plugin.on) plugin.dispose = fn;
        else fn();
      };
      try {
        const out = mod.default(cityFor(plugin));
        // An async default export is let run; the plugin is turned off if
        // it rejects, the same as a throw.
        if (out && isFn(out.then)) {
          out.then(keep, (err) => {
            report(plugin, err);
            disable(plugin);
          });
        } else keep(out);
      } catch (err) {
        report(plugin, err);
        disable(plugin);
      }
    }
  }

  // disable turns a plugin off: its callbacks stop, everything it added is
  // taken back, and its own undo, if it returned one, runs.
  function disable(plugin) {
    if (!plugin.on) return;
    plugin.on = false;
    for (const undo of plugin.undo.splice(0).reverse()) undo();
    const dispose = plugin.dispose;
    plugin.dispose = null;
    if (dispose) {
      try {
        dispose();
      } catch (err) {
        report(plugin, err);
      }
    }
  }

  // publish tells the listeners what changed since the last publish. The host
  // calls it after its own actions settle, so a listener never runs in the
  // middle of one, and a listener that acts in turn is told on the next call.
  function publish() {
    const now = host.state();
    const prev = last;
    last = now;
    if (!prev) return;
    const same = (a, b) => Object.keys(a).every((key) => a[key] === b[key]);
    const event = {
      selection: () => ({ node: host.node(now.selection.node) || null, refs: now.selection.refs }),
      focus: () => ({ node: host.node(now.focus.node) || null }),
      view: () => ({ mode: now.view.mode, bird: now.view.bird }),
      sidebar: () => ({ shown: now.sidebar.shown }),
      diff: () => ({ node: host.node(now.diff.node) || null }),
    };
    for (const ns of Object.keys(event)) {
      if (same(now[ns], prev[ns]) || !listeners[ns].length) continue;
      const e = Object.freeze(event[ns]());
      for (const entry of [...listeners[ns]]) entry.fn(e);
    }
  }

  // hits are the plugins' search hits for query: existing nodes only, each
  // once, in the order the plugins gave them.
  function hits(query) {
    const out = [];
    const seen = new Set();
    for (const source of added.search) {
      const list = source.find(query);
      if (!Array.isArray(list)) continue;
      for (const hit of list) {
        if (out.length >= MAX_HITS) return out;
        if (!hit || !isText(hit.id) || seen.has(hit.id) || !host.node(hit.id)) continue;
        seen.add(hit.id);
        out.push({ id: hit.id, note: isText(hit.note) ? hit.note : "" });
      }
    }
    return out;
  }

  // sections are the plugins' sidebar sections for node, rendered: the ones
  // for its kind whose render gave Markdown.
  function sections(node) {
    if (!node) return [];
    const out = [];
    for (const section of added.sections) {
      if (section.kinds && !section.kinds.has(node.kind)) continue;
      const markdown = section.render(node);
      if (isText(markdown) && markdown.trim()) out.push({ title: section.title, markdown });
    }
    return out;
  }

  // runDiffAction runs a diff panel button with what the plugin is promised:
  // the node, the file's path, and a frozen copy of the hunk.
  function runDiffAction(action, nodeId, file, hunk) {
    action.run(Object.freeze({
      node: host.node(nodeId) || null,
      file,
      hunk: freezeDeep(structuredClone(hunk)),
    }));
  }

  return {
    load,
    publish,
    disable: (name) => {
      const plugin = loaded.find((p) => p.name === name);
      if (plugin) disable(plugin);
    },
    names: () => loaded.filter((p) => p.on).map((p) => p.name),
    commands: () => added.commands,
    keys: () => added.keys,
    key: (key) => added.keys.find((k) => k.key === key) || null,
    ring: () => added.ring,
    diffActions: () => added.diff,
    runDiffAction,
    hits,
    sections,
  };
}

// apiNodes turns a scene document into the nodes plugins read: one frozen
// object per package and declaration, by id, with what the API adds on top
// of scene.json: a kind on every package, the package on every declaration,
// and the id a resolved call goes to. resolve(step) is the host's own lookup
// of a call's target, so the API and the city agree on where a call goes.
export function apiNodes(doc, resolve) {
  const byId = new Map();
  const packages = [];
  for (const pkg of (doc && doc.packages) || []) {
    const entities = (pkg.entities || []).map((e) => ({
      kind: e.kind,
      id: e.id,
      name: e.name || "",
      recv: e.recv || "",
      package: pkg.id,
      file: e.file || "",
      line: e.line || 0,
      col: e.col || 0,
      parent: e.parent || "",
      change: e.change || "same",
      part: e.part || "",
      fields: e.fields || [],
      bodyBytes: e.bodyBytes || 0,
      bodyBytesBefore: e.bodyBytesBefore == null ? null : e.bodyBytesBefore,
      calls: (e.calls || []).map((step) => ({
        expr: step.expr || "",
        change: step.change || "same",
        to: (step.resolved && resolve(step)) || null,
      })),
    }));
    const node = {
      kind: pkg.external ? "external" : "package",
      id: pkg.id,
      name: pkg.name || pkg.id,
      dir: pkg.dir || "",
      parent: pkg.parent || "",
      synthetic: !!pkg.synthetic,
      change: pkg.change || "same",
      entities,
      deps: (pkg.deps || []).map((d) => ({
        to: d.to,
        change: d.change || "same",
        external: !!d.external,
        files: d.files || 0,
      })),
    };
    freezeDeep(node);
    packages.push(node);
    byId.set(node.id, node);
    // A package keeps its id if a declaration ever spells the same one.
    for (const e of node.entities) if (!byId.has(e.id)) byId.set(e.id, e);
  }
  return { packages: Object.freeze(packages), byId };
}
