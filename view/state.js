// The view's place in the address bar (#22): what is selected, the mode, the
// call direction and the two sidebars. A reload comes back to the same view,
// and a link to it opens on the same node. It is written for a person to read
// and edit:
//
//   ?select=citydiff:lib:diff:TestCoolStuff&mode=changes&refs=callers&side=closed
//
// A default is left out, so the bare city has a bare address. The camera is
// not here: restoring the selection flies to it the way a click does.

// The parameters this file owns. Any other parameter (tour, check) is kept.
export const STATE_PARAMS = ["select", "mode", "refs", "side", "tourside"];

// pathOf turns an id into its steps: citydiff/lib/diff is citydiff:lib:diff.
export function pathOf(id) {
  return String(id).split("/").filter(Boolean).join(":");
}

// nodePath is a node spelled from the top of the tree down: the package's
// path, then for a declaration its type (a method's receiver) and its name.
//
//   citydiff:lib:diff                  package
//   citydiff:lib:diff:TestCoolStuff    function
//   citydiff:lib:Parser                type
//   citydiff:lib:Parser:Parse          method
//
// A node is { kind: "package" | "external", id } or { kind: "entity", entity, pkg }
// with pkg the package id.
export function nodePath(node) {
  if (node.kind !== "entity") return pathOf(node.id);
  const { entity } = node;
  const own = entity.kind === "method" && entity.recv ? entity.recv + ":" + entity.name : entity.name;
  return pathOf(node.pkg) + ":" + own;
}

// nameIndex groups nodes by path. When two nodes share one, the first added
// wins a bare path, so packages go in before declarations.
export function nameIndex(nodes) {
  const index = new Map();
  for (const node of nodes) {
    const key = nodePath(node);
    const list = index.get(key);
    if (list) list.push(node);
    else index.set(key, [node]);
  }
  return index;
}

// writeNode is a node's path, with @file when another declaration has the
// same path (two init functions in one package).
export function writeNode(node, index) {
  if (!node) return "";
  const key = nodePath(node);
  const same = index.get(key) || [];
  const file = node.kind === "entity" ? node.entity.file : "";
  return same.length > 1 && file ? key + "@" + file : key;
}

// readNode is writeNode backwards: { kind, id }, with id the package id or the
// entity id, or null when nothing in the scene has that path.
export function readNode(text, index) {
  if (!text) return null;
  const at = text.indexOf("@");
  const key = at < 0 ? text : text.slice(0, at);
  const file = at < 0 ? "" : text.slice(at + 1);
  const list = index.get(key) || [];
  const hit = (file && list.find((node) => node.kind === "entity" && node.entity.file === file)) || list[0];
  if (!hit) return null;
  return hit.kind === "entity" ? { kind: "entity", id: hit.entity.id } : { kind: hit.kind, id: hit.id };
}

// readState reads the view out of a query string. Anything unknown reads as
// the default.
export function readState(search) {
  const params = new URLSearchParams(search);
  return {
    select: params.get("select") || "",
    mode: params.get("mode") === "changes" ? "changes" : "overview",
    refs: params.get("refs") === "callers" ? "callers" : "calls",
    side: params.get("side") !== "closed",
    tourSide: params.get("tourside") !== "closed",
  };
}

// writeState puts the view into a query string, keeping the parameters it
// does not own, and returns it with its leading "?" (or "" when empty).
export function writeState(search, state) {
  const params = new URLSearchParams(search);
  for (const key of STATE_PARAMS) params.delete(key);
  const out = [...params];
  if (state.select) out.push(["select", state.select]);
  if (state.mode === "changes") out.push(["mode", "changes"]);
  if (state.refs === "callers") out.push(["refs", "callers"]);
  if (state.side === false) out.push(["side", "closed"]);
  if (state.tourSide === false) out.push(["tourside", "closed"]);
  if (!out.length) return "";
  return "?" + out.map(([key, value]) => readable(key) + "=" + readable(value)).join("&");
}

// URLSearchParams escapes : / @ and turns a space into +. They are all safe in
// a query, so they stay as typed and the address reads like the node's name.
function readable(text) {
  return encodeURIComponent(text).replace(/%(3A|2F|40|2C)/gi, (all) => decodeURIComponent(all));
}
