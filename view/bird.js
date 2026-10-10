// Bird view: the city from straight above, merged into districts. Pure data
// and geometry; main.js turns it into meshes once per scene, level and mode.

export const DISTRICT_CAP = 60;
export const DISTRICT_MIN = 0.02;

// chooseDistricts cuts the package tree into at most cap districts. It
// starts from the top-level packages and keeps splitting the biggest
// district that has sub-packages: its sub-packages whose short side is at
// least min × the city's span become districts of their own, as long as the
// count stays at or under cap. Sub-packages smaller than that stay merged in
// their parent. A split parent stays a district for its own declarations
// and the small sub-packages: a base slab under its children. Every package
// maps to the district it lies in.
export function chooseDistricts(laid, packages, { cap = DISTRICT_CAP, min = DISTRICT_MIN } = {}) {
  const blocks = laid.packages;
  const b = laid.bounds;
  const span = Math.max(b.maxX - b.minX, b.maxZ - b.minZ, 1e-9);
  const byId = new Map(blocks.map((x) => [x.id, x]));
  const parentOf = new Map((packages || []).map((p) => [p.id, p.parent]));
  const kids = new Map();
  const roots = [];
  for (const x of blocks) {
    const p = parentOf.get(x.id);
    if (p && byId.has(p)) {
      if (!kids.has(p)) kids.set(p, []);
      kids.get(p).push(x);
    } else roots.push(x);
  }
  const big = (x) => Math.min(x.w, x.d) >= min * span;
  const chosen = new Set(roots.map((x) => x.id));
  const split = new Set();
  let count = chosen.size;
  for (;;) {
    let best = null;
    let bestKids = null;
    for (const id of chosen) {
      if (split.has(id)) continue;
      const own = (kids.get(id) || []).filter(big);
      if (!own.length || count + own.length > cap) continue;
      const x = byId.get(id);
      if (!best || x.w * x.d > best.w * best.d) {
        best = x;
        bestKids = own;
      }
    }
    if (!best) break;
    split.add(best.id);
    for (const k of bestKids) chosen.add(k.id);
    count += bestKids.length;
  }
  const districts = blocks.filter((x) => chosen.has(x.id)).map((x) => ({ ...x, base: split.has(x.id) }));
  const districtOf = new Map();
  for (const x of blocks) {
    let id = x.id;
    while (id && !chosen.has(id)) id = parentOf.get(id);
    if (id) districtOf.set(x.id, id);
  }
  const depth = districts.reduce((m, x) => Math.max(m, x.depth), 0);
  return { depth, districts, districtOf };
}

// districtStatus is the change of a district from the entities in it: all
// added or all removed keeps that colour, any other change is modified,
// none is same. The shares feed the stacked bar on the slab.
export function districtStatus(entities) {
  const n = { added: 0, removed: 0, modified: 0, same: 0 };
  for (const e of entities) {
    const c = e.change === "added" || e.change === "removed" || e.change === "same" ? e.change : e.change ? "modified" : "same";
    n[c]++;
  }
  const total = n.added + n.removed + n.modified + n.same;
  const changed = n.added + n.removed + n.modified;
  let status = "same";
  if (changed) {
    if (n.added === total) status = "added";
    else if (n.removed === total) status = "removed";
    else status = "modified";
  }
  const share = total ? { added: n.added / total, modified: n.modified / total, removed: n.removed / total } : { added: 0, modified: 0, removed: 0 };
  return { status, counts: n, share };
}

// depEdges are the package dependencies the current mode draws: in changes
// mode the added and removed imports, in the overview every internal one
// that still exists.
export function depEdges(packages, mode) {
  const internal = new Set((packages || []).filter((p) => !p.external).map((p) => p.id));
  const out = [];
  for (const p of packages || []) {
    if (p.external) continue;
    for (const dep of p.deps || []) {
      if (!internal.has(dep.to)) continue;
      if (mode === "overlay" ? dep.change !== "added" && dep.change !== "removed" : dep.change === "removed") continue;
      out.push({ from: p.id, to: dep.to, change: dep.change || "same" });
    }
  }
  return out;
}

const RANK = { same: 0, modified: 1, removed: 2, added: 3 };

// aggregateEdges merges package edges into one per district pair. Direction
// is dropped (a pair is unordered) and so are self-edges. count is how many
// package edges the curve stands for; change is the strongest among them.
export function aggregateEdges(edges, districtOf) {
  const pairs = new Map();
  for (const e of edges) {
    const a = districtOf.get(e.from);
    const b = districtOf.get(e.to);
    if (!a || !b || a === b) continue;
    const key = a < b ? a + "\0" + b : b + "\0" + a;
    const prev = pairs.get(key);
    const change = e.change || "same";
    if (!prev) pairs.set(key, { from: a < b ? a : b, to: a < b ? b : a, count: 1, change });
    else {
      prev.count++;
      if ((RANK[change] || 0) > (RANK[prev.change] || 0)) prev.change = change;
    }
  }
  return [...pairs.values()];
}

// labelBox sizes a district's name to its slab: as large as fits 85% of the
// width and 40% of the depth, capped. Glyphs are taken as 0.6 em wide.
export function labelBox(text, w, d, cap) {
  const size = Math.min((w * 0.85) / Math.max(1, text.length * 0.6), d * 0.4, cap);
  return { size, w: size * text.length * 0.6, h: size };
}

// placeLabels is the collision pass, in the ground plane. Labels go in by
// priority (biggest first); one that overlaps a placed label, or one of its
// own obstacles (avoid: the slabs that stand on a base district), shrinks
// to 70% up to twice, and is hidden if it still overlaps or drops under
// minSize.
export function placeLabels(items, minSize) {
  const placed = [];
  const out = new Map();
  const hit = (r) => placed.some((p) => r.x0 < p.x1 && r.x1 > p.x0 && r.z0 < p.z1 && r.z1 > p.z0);
  const order = [...items].sort((a, b) => b.priority - a.priority || (a.id < b.id ? -1 : 1));
  for (const it of order) {
    let k = 1;
    let shown = false;
    for (let i = 0; i < 3; i++, k *= 0.7) {
      const size = it.size * k;
      if (size < minSize) break;
      const hw = (it.w * k) / 2;
      const hh = (it.h * k) / 2;
      const r = { x0: it.x - hw, x1: it.x + hw, z0: it.z - hh, z1: it.z + hh };
      const blocked = (it.avoid || []).some((a) => r.x0 < a.x1 && r.x1 > a.x0 && r.z0 < a.z1 && r.z1 > a.z0);
      if (!blocked && !hit(r)) {
        placed.push(r);
        out.set(it.id, { size, scale: k, hidden: false });
        shown = true;
        break;
      }
    }
    if (!shown) out.set(it.id, { size: 0, scale: 0, hidden: true });
  }
  return out;
}

// BirdToggle remembers the camera on the way in and hands back exactly that
// on the way out.
export class BirdToggle {
  constructor() {
    this.on = false;
    this.saved = null;
  }
  enter(camera) {
    if (this.on) return false;
    this.saved = {
      pos: [...camera.pos],
      target: [...camera.target],
      up: [...camera.up],
      zoom: camera.zoom,
      extra: camera.extra === undefined ? undefined : structuredClone(camera.extra),
    };
    this.on = true;
    return true;
  }
  leave() {
    if (!this.on) return null;
    this.on = false;
    const s = this.saved;
    this.saved = null;
    return s;
  }
}
