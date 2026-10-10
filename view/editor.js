// E opens the selected node in nvim, in a terminal over the city. The
// terminal is ghostty-web, loaded on first use, talking to /api/edit over a
// WebSocket: binary frames are keystrokes and output, text frames are resize
// requests and, from the server, an error to show instead of the editor.

// editTarget is what the server should open for a selection: an entity's
// file at its position, or a package's directory with its first file as a
// fallback. A removed entity has no position on the working tree, so it
// opens its file at the top.
export function editTarget(selected, byEntity, byPackage) {
  if (!selected) return null;
  if (selected.kind === "entity") {
    const found = byEntity.get(selected.id);
    if (!found) return null;
    const e = found.entity;
    const out = { file: e.file };
    if (e.change !== "removed" && e.line > 0) {
      out.line = e.line;
      if (e.col > 0) out.col = e.col;
    }
    return out;
  }
  if (selected.kind === "package") {
    const pkg = byPackage.get(selected.id);
    if (!pkg || pkg.external) return null;
    const first = (pkg.entities || []).find((e) => e.change !== "removed") || (pkg.entities || [])[0];
    const out = { dir: pkg.dir || "." };
    if (first) out.file = first.file;
    return out;
  }
  return null;
}

export function editQuery(target, cols, rows) {
  const q = new URLSearchParams();
  for (const k of ["file", "dir", "line", "col"]) if (target[k] !== undefined) q.set(k, String(target[k]));
  q.set("cols", String(cols));
  q.set("rows", String(rows));
  return q.toString();
}

let session = null;

// editorActive is true while the overlay is up; the viewer's keys stay off.
export function editorActive() {
  return session !== null;
}

let ghostty = null;
async function load() {
  if (!ghostty) {
    ghostty = import("./vendor/ghostty/ghostty-web.js").then(async (mod) => {
      await mod.init();
      return mod;
    });
  }
  return ghostty;
}

export function closeEditor() {
  if (!session) return;
  const s = session;
  session = null;
  window.removeEventListener("resize", s.fit);
  try { s.ws?.close(); } catch {}
  try { s.term?.dispose(); } catch {}
  s.root.remove();
  s.onClose?.();
}

export async function openEditor(target, onClose) {
  if (session || !target) return;
  const root = document.createElement("div");
  root.id = "editor";
  root.innerHTML = '<div class="editor-term"></div><p class="editor-msg" hidden></p>';
  document.body.append(root);
  const s = { root, onClose, failed: false };
  session = s;
  const msg = root.querySelector(".editor-msg");
  const fail = (text) => {
    s.failed = true;
    msg.textContent = text + " (esc closes)";
    msg.hidden = false;
  };
  let mod;
  try {
    mod = await load();
  } catch (err) {
    fail("cannot load the terminal: " + err.message);
    return;
  }
  if (session !== s) return;
  const term = new mod.Terminal({ fontSize: 14, cursorBlink: false, theme: { background: "#0d1017", foreground: "#d6dbe4" } });
  const fitter = new mod.FitAddon();
  term.loadAddon(fitter);
  term.open(root.querySelector(".editor-term"));
  fitter.fit();
  s.term = term;
  s.fit = () => fitter.fit();
  window.addEventListener("resize", s.fit);
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/api/edit?${editQuery(target, term.cols, term.rows)}`);
  ws.binaryType = "arraybuffer";
  s.ws = ws;
  const enc = new TextEncoder();
  term.onData((d) => ws.readyState === 1 && ws.send(enc.encode(d)));
  term.onResize(({ cols, rows }) => ws.readyState === 1 && ws.send(JSON.stringify({ type: "resize", cols, rows })));
  ws.onopen = () => {
    ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    term.focus();
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") {
      try {
        const m = JSON.parse(ev.data);
        if (m.type === "error") fail(m.message);
      } catch {}
      return;
    }
    term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (session === s && !s.failed) closeEditor();
  };
  ws.onerror = () => {
    if (session === s && !s.failed) fail("the editor connection failed");
  };
}
