// D shows the git diff of the selected node over the city, drawn in the
// page's own colours. The hunks come from /api/diff, which builds them with
// go-git from the range the scene is showing; the highlighting is the page's,
// so it follows the skin. There is no terminal and no pager in the way.

import { highlighter } from "./highlight.js";

let panel = null;

// diffActive is true while the overlay is up; the viewer's keys stay off.
export function diffActive() {
  return panel !== null;
}

// diffTarget is what to ask the server for: an entity's file and the lines it
// covers, or a package's directory. A removed entity has no lines on the new
// side, so it asks for its file without them.
export function diffTarget(selected, byEntity, byPackage) {
  if (!selected) return null;
  if (selected.kind === "entity") {
    const found = byEntity.get(selected.id);
    if (!found || !found.entity.file) return null;
    const e = found.entity;
    const out = { file: e.file, label: label(e), where: e.file, change: e.change, part: e.part };
    if (e.line > 0) {
      // A removed declaration only has lines on the old side of the diff.
      out.line = e.line;
      out.side = e.change === "removed" ? "old" : "new";
      const end = nextDeclaration(byEntity, e);
      if (end) out.end = end;
    }
    return out;
  }
  if (selected.kind === "package") {
    const pkg = byPackage.get(selected.id);
    if (!pkg || pkg.external) return null;
    return { dir: pkg.dir || ".", label: pkg.name || pkg.id, where: pkg.id, change: pkg.change };
  }
  return null;
}

// nextDeclaration is the line after the node: the next declaration of the same
// file, which is where the node ends as far as the diff is concerned. Without
// it a hunk at the node's last line would be counted as someone else's.
function nextDeclaration(byEntity, entity) {
  let next = 0;
  for (const found of byEntity.values()) {
    const other = found.entity;
    if (other === entity || other.file !== entity.file || !(other.line > entity.line)) continue;
    if (!next || other.line < next) next = other.line;
  }
  return next;
}

// partWord is what changed in a function or method, for the header: the diff
// is of a body, a signature or both, and the page says which.
function partWord(change, part) {
  if (change !== "modified") return "";
  if (part === "body") return "body changed";
  if (part === "signature") return "signature changed";
  if (part === "both") return "signature and body changed";
  return "";
}

function label(entity) {
  if (entity.kind === "method" && entity.recv) return entity.recv + "." + entity.name;
  return entity.name || entity.id;
}

function query(target) {
  const q = new URLSearchParams();
  for (const key of ["file", "dir", "line", "end", "side"]) if (target[key] !== undefined) q.set(key, String(target[key]));
  return q.toString();
}

// closeDiff takes the overlay down and hands the city its keyboard back.
export function closeDiff() {
  if (!panel) return;
  const p = panel;
  panel = null;
  document.removeEventListener("keydown", p.onKey);
  p.root.remove();
  p.onClose?.();
}

// openDiff asks for the node's hunks and draws them. It resolves when the
// overlay is up or the request failed, which is what the caller waits for.
export async function openDiff(target, onClose) {
  if (panel || !target) return;
  const root = document.createElement("div");
  root.id = "diff";
  root.tabIndex = -1;
  root.innerHTML =
    '<header id="diff-head">' +
    '<span class="diff-title"></span>' +
    '<span class="diff-where"></span>' +
    '<span class="diff-note" hidden></span>' +
    '<span class="diff-spacer"></span>' +
    '<span class="diff-hint">esc closes</span>' +
    "</header>" +
    '<div id="diff-body"></div>';
  document.body.append(root);
  const p = { root, onClose, onKey: null };
  panel = p;
  root.querySelector(".diff-title").textContent = target.label || target.file || target.dir || "";
  const part = partWord(target.change, target.part);
  root.querySelector(".diff-where").textContent = [target.where, part].filter(Boolean).join(" · ");
  root.addEventListener("click", (event) => {
    if (event.target === root) closeDiff();
  });
  p.onKey = (event) => {
    if (event.key !== "Escape") return;
    // The viewer listens on the window, which sees this event after the
    // document does: by then the overlay is gone and its own guard is stale,
    // so Escape would close the diff and go back in the city at once.
    event.preventDefault();
    event.stopPropagation();
    closeDiff();
  };
  document.addEventListener("keydown", p.onKey);
  root.focus();

  const body = root.querySelector("#diff-body");
  const note = root.querySelector(".diff-note");
  let extraNote = "";
  let reply;
  try {
    const res = await fetch("/api/diff?" + query(target));
    reply = await res.json();
  } catch (err) {
    fail(body, "cannot read the diff: " + (err && err.message ? err.message : err));
    return;
  }
  if (panel !== p) return;
  if (reply.problems && reply.problems.length) {
    fail(body, reply.problems[0].message);
    return;
  }
  const files = (reply.files || []).filter((file) => (file.hunks || []).length || file.binary);
  if (!files.length) {
    fail(body, (reply.files || []).length ? "no lines of this change are inside the node" : "this node has no change in " + (reply.range || "the range"));
    return;
  }
  const shown = files.reduce((n, file) => n + (file.hunks || []).length, 0);
  const omitted = files.reduce((n, file) => n + (file.omitted || 0), 0) + (reply.more || 0);
  extraNote = files.map((file) => file.note).filter(Boolean)[0] || "";
  note.textContent = [shown + (shown === 1 ? " hunk" : " hunks"), reply.range, omitted ? omitted + " not shown" : "", extraNote].filter(Boolean).join(" · ");
  note.hidden = false;
  for (const file of files) body.append(fileBlock(file));
  body.scrollTop = 0;
}

function fail(body, message) {
  const p = document.createElement("p");
  p.className = "diff-error";
  p.textContent = message;
  body.append(p);
}

// fileBlock is one file: its name, then each hunk with its header and lines.
function fileBlock(file) {
  const wrap = document.createElement("section");
  wrap.className = "diff-file";
  const head = document.createElement("h3");
  head.className = "diff-file-head";
  head.textContent = file.path;
  wrap.append(head);
  if (file.binary) {
    const line = document.createElement("p");
    line.className = "diff-error";
    line.textContent = "binary file";
    wrap.append(line);
    return wrap;
  }
  const draw = highlighter(file.lang || "");
  for (const hunk of file.hunks || []) {
    const block = document.createElement("div");
    block.className = "diff-hunk";
    const header = document.createElement("div");
    header.className = "diff-hunk-head";
    header.textContent = "@@ -" + hunk.oldStart + "," + hunk.oldLines + " +" + hunk.newStart + "," + hunk.newLines + " @@";
    block.append(header);
    for (const line of hunk.lines || []) {
      const row = document.createElement("div");
      row.className = "diff-line " + line.kind;
      const old = document.createElement("span");
      old.className = "diff-no";
      old.textContent = line.old ? String(line.old) : "";
      const now = document.createElement("span");
      now.className = "diff-no";
      now.textContent = line.new ? String(line.new) : "";
      const sign = document.createElement("span");
      sign.className = "diff-sign";
      sign.textContent = line.kind === "add" ? "+" : line.kind === "del" ? "−" : " ";
      const code = document.createElement("code");
      code.innerHTML = draw(line.text);
      row.append(old, now, sign, code);
      block.append(row);
    }
    wrap.append(block);
  }
  return wrap;
}
