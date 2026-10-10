// The view's place in the address bar (#22): what is selected, the mode, the
// call direction and the two sidebars. A reload comes back to the same view,
// and a link to it opens on the same node. It is written for a person to read
// and edit:
//
//   ?select=function:TestCoolStuff&mode=changes&refs=callers&side=closed
//
// A default is left out, so the bare city has a bare address. The camera is
// not here: restoring the selection flies to it the way a click does.

// The parameters this file owns. Any other parameter (tour, check) is kept.
export const STATE_PARAMS = ["select", "mode", "refs", "side", "tourside"];

// declLabel is a declaration's name as the search lists it: a method carries
// its receiver.
export function declLabel(entity) {
  return entity.kind === "method" && entity.recv ? entity.recv + "." + entity.name : entity.name;
}

// nameIndex groups declarations by kind:label, the spelling a node gets in
// the address bar, so a shared name can be told apart by its file.
export function nameIndex(entities) {
  const index = new Map();
  for (const entity of entities) {
    const key = entity.kind + ":" + declLabel(entity);
    const list = index.get(key);
    if (list) list.push(entity);
    else index.set(key, [entity]);
  }
  return index;
}

// writeNode spells a selection as kind:name. A package or an external is its
// id. A declaration is its name, with @file only when another declaration of
// the same kind shares the name.
export function writeNode(selected, entity, index) {
  if (!selected) return "";
  if (selected.kind === "package" || selected.kind === "external") return selected.kind + ":" + selected.id;
  if (!entity) return "";
  const key = entity.kind + ":" + declLabel(entity);
  const same = index.get(key) || [];
  return same.length > 1 && entity.file ? key + "@" + entity.file : key;
}

// readNode is writeNode backwards. A package or an external comes back as its
// id for the caller to check; a declaration comes back as its entity id, or
// null when nothing by that name is in the scene. A name without @file that
// several declarations share picks the first.
export function readNode(text, index) {
  const colon = (text || "").indexOf(":");
  if (colon <= 0) return null;
  const kind = text.slice(0, colon);
  const rest = text.slice(colon + 1);
  if (!rest) return null;
  if (kind === "package" || kind === "external") return { kind, id: rest };
  const at = rest.indexOf("@");
  const label = at < 0 ? rest : rest.slice(0, at);
  const file = at < 0 ? "" : rest.slice(at + 1);
  const list = index.get(kind + ":" + label) || [];
  const hit = (file && list.find((entity) => entity.file === file)) || list[0];
  return hit ? { kind: "entity", id: hit.id } : null;
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
