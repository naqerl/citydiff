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

// A phone's keyboard has no Escape, no Ctrl and no Alt, and keeps the
// punctuation nvim lives on behind a second page. On a touch screen a bar
// under the terminal has them. Ctrl and Alt hold for the next key, typed on
// the keyboard or pressed on the bar.
export const TOUCH_KEYS = [
  { label: "esc", send: "\x1b" },
  { label: "ctrl", hold: "ctrl" },
  { label: "alt", hold: "alt" },
  { label: "/", send: "/" },
  { label: ":", send: ":" },
  { label: ";", send: ";" },
  { label: "(", send: "(" },
  { label: ")", send: ")" },
];

// withCtrl is what a key typed with Ctrl held sends: a letter is its control
// code, the few punctuation keys that have one send it, and anything else is
// sent as it is.
export function withCtrl(data) {
  if (typeof data !== "string" || data.length !== 1) return data;
  const code = data.toLowerCase().charCodeAt(0);
  if (code >= 97 && code <= 122) return String.fromCharCode(code - 96);
  const punct = { "@": "\x00", " ": "\x00", "[": "\x1b", "\\": "\x1c", "]": "\x1d", "^": "\x1e", "_": "\x1f", "?": "\x7f" };
  return punct[data] ?? data;
}

// withMods is what a key sends with Ctrl or Alt held for it: Ctrl as above,
// and Alt an Escape in front, the way a terminal sends Meta.
export function withMods(data, { ctrl = false, alt = false } = {}) {
  const out = ctrl ? withCtrl(data) : data;
  return alt ? "\x1b" + out : out;
}

// typed applies the held Ctrl and Alt to one key and lets go of them.
function typed(s, data) {
  if (!s.mods.ctrl && !s.mods.alt) return data;
  const out = withMods(data, s.mods);
  s.mods.ctrl = false;
  s.mods.alt = false;
  for (const button of s.modKeys) button.classList.remove("on");
  return out;
}

const touchScreen = () => window.matchMedia("(hover: none) and (pointer: coarse)").matches;

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
  if (s.keep) {
    window.visualViewport?.removeEventListener("resize", s.keep);
    window.visualViewport?.removeEventListener("scroll", s.keep);
  }
  clearTimeout(s.fitTimer);
  try { s.ws?.close(); } catch {}
  try { s.term?.dispose(); } catch {}
  s.root.remove();
  s.onClose?.();
}

// mountKeys puts the bar of missing keys under the terminal, and keeps the
// overlay to the part of the screen the phone's keyboard leaves, so the bar
// sits on top of the keyboard when it is up and on the bottom of the screen
// when it is not. A key acts on the press and never takes the focus: it
// neither brings the keyboard up nor puts it away. The bar goes in before
// the terminal's first fit, so the terminal is fitted round it.
function mountKeys(s) {
  const bar = document.createElement("div");
  bar.className = "editor-keys";
  for (const key of TOUCH_KEYS) {
    const button = document.createElement("button");
    button.type = "button";
    button.tabIndex = -1;
    button.textContent = key.label;
    if (key.hold) s.modKeys.push(button);
    button.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      if (key.hold) {
        s.mods[key.hold] = !s.mods[key.hold];
        button.classList.toggle("on", s.mods[key.hold]);
      } else {
        s.send?.(typed(s, key.send));
      }
    });
    bar.append(button);
  }
  s.root.append(bar);
  const vv = window.visualViewport;
  if (vv) {
    // The keyboard slides, and the viewport reports every step of it; ghostty
    // drops a fit that comes within 50 ms of the last one, so the terminal is
    // fitted once the steps stop, and only the overlay follows each one.
    s.keep = () => {
      s.root.style.top = vv.offsetTop + "px";
      s.root.style.height = vv.height + "px";
      s.root.style.bottom = "auto";
      clearTimeout(s.fitTimer);
      s.fitTimer = setTimeout(() => s.fit?.(), 120);
    };
    vv.addEventListener("resize", s.keep);
    vv.addEventListener("scroll", s.keep);
    s.keep();
  }
}

export async function openEditor(target, onClose) {
  if (session || !target) return;
  const root = document.createElement("div");
  root.id = "editor";
  root.innerHTML = '<div class="editor-term"></div><p class="editor-msg" hidden></p>';
  document.body.append(root);
  const touch = touchScreen();
  const s = { root, onClose, failed: false, mods: { ctrl: false, alt: false }, modKeys: [] };
  session = s;
  if (touch) mountKeys(s);
  const msg = root.querySelector(".editor-msg");
  // A phone has no Escape to close a failed editor with: the message is
  // the way out there.
  const fail = (text) => {
    s.failed = true;
    msg.textContent = text + (touch ? " (tap to close)" : " (esc closes)");
    msg.hidden = false;
  };
  msg.addEventListener("click", () => {
    if (session === s) closeEditor();
  });
  let mod;
  try {
    mod = await load();
  } catch (err) {
    fail("cannot load the terminal: " + err.message);
    return;
  }
  if (session !== s) return;
  // A phone gets a smaller face, so a line of code mostly fits its width.
  const term = new mod.Terminal({ fontSize: touch ? 10 : 14, cursorBlink: false, theme: { background: "#0d1017", foreground: "#d6dbe4" } });
  const fitter = new mod.FitAddon();
  term.loadAddon(fitter);
  term.open(root.querySelector(".editor-term"));
  // ghostty takes the focus as it opens, and once more a moment later. On a
  // phone that would bring the keyboard up unasked: let go of it after both.
  if (touch) setTimeout(() => term.blur(), 60);
  fitter.fit();
  s.term = term;
  s.fit = () => fitter.fit();
  window.addEventListener("resize", s.fit);
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(`${proto}//${location.host}/api/edit?${editQuery(target, term.cols, term.rows)}`);
  ws.binaryType = "arraybuffer";
  s.ws = ws;
  const enc = new TextEncoder();
  const send = (d) => ws.readyState === 1 && ws.send(enc.encode(d));
  s.send = send;
  term.onData((d) => send(typed(s, d)));
  term.onResize(({ cols, rows }) => ws.readyState === 1 && ws.send(JSON.stringify({ type: "resize", cols, rows })));
  ws.onopen = () => {
    ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    // On a phone the focus is the keyboard: it comes up when the terminal is
    // tapped, not when the editor opens.
    if (!touch) term.focus();
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
