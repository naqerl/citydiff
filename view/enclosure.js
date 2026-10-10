// Enclosure: whether a block can be read. A block is a package's plinth in
// the laid-out city. One of its sides is closed when other blocks (not its
// ancestors or descendants, which it sits on or carries) come within `gap`
// of that side along at least `cover` of the side's length. A block is
// enclosed when all four sides are closed: there is no street to look in
// from, so its labels and buildings hide behind its neighbours.
//
// gap 1.0 is "tight": wider than the 0.55 inner padding that separates
// sibling blocks today, narrower than a one-building street (a building is
// about 1.05 wide). cover 0.5: a side more than half open still shows the
// label and most of the buildings along it.

export const ENCLOSED_GAP = 1.0;
export const ENCLOSED_COVER = 0.5;

function covered(intervals, lo, hi) {
  const parts = intervals
    .map(([a, b]) => [Math.max(a, lo), Math.min(b, hi)])
    .filter(([a, b]) => b > a)
    .sort((p, q) => p[0] - q[0]);
  let sum = 0;
  let end = lo;
  for (const [a, b] of parts) {
    if (b <= end) continue;
    sum += b - Math.max(a, end);
    end = b;
  }
  return sum;
}

// enclosedBlocks lists the ids of enclosed blocks. parentOf maps a package
// id to its parent's id.
export function enclosedBlocks(blocks, parentOf, { gap = ENCLOSED_GAP, cover = ENCLOSED_COVER } = {}) {
  const lineage = new Map();
  const ancestors = (id) => {
    if (lineage.has(id)) return lineage.get(id);
    const set = new Set();
    for (let p = parentOf.get(id); p && !set.has(p); p = parentOf.get(p)) set.add(p);
    lineage.set(id, set);
    return set;
  };
  const eps = 1e-6;
  const out = [];
  for (const b of blocks) {
    if (b.w <= eps || b.d <= eps) continue;
    const mine = ancestors(b.id);
    const sides = { left: [], right: [], near: [], far: [] };
    for (const n of blocks) {
      if (n === b || mine.has(n.id) || ancestors(n.id).has(b.id)) continue;
      const zs = [n.z, n.z + n.d];
      const xs = [n.x, n.x + n.w];
      const zOver = n.z < b.z + b.d - eps && n.z + n.d > b.z + eps;
      const xOver = n.x < b.x + b.w - eps && n.x + n.w > b.x + eps;
      if (zOver) {
        const e = n.x + n.w;
        if (e <= b.x + eps && e >= b.x - gap) sides.left.push(zs);
        if (n.x >= b.x + b.w - eps && n.x <= b.x + b.w + gap) sides.right.push(zs);
      }
      if (xOver) {
        const e = n.z + n.d;
        if (e <= b.z + eps && e >= b.z - gap) sides.near.push(xs);
        if (n.z >= b.z + b.d - eps && n.z <= b.z + b.d + gap) sides.far.push(xs);
      }
    }
    const closedZ = (list) => covered(list, b.z, b.z + b.d) >= cover * b.d;
    const closedX = (list) => covered(list, b.x, b.x + b.w) >= cover * b.w;
    if (closedZ(sides.left) && closedZ(sides.right) && closedX(sides.near) && closedX(sides.far)) out.push(b.id);
  }
  return out;
}

// cityMetrics: the footprint is the bounding area of every block; density
// is the buildings' footprint over it, so streets only pay off when the
// buildings do not shrink to make room for them.
export function cityMetrics(laid) {
  let minX = Infinity, minZ = Infinity, maxX = -Infinity, maxZ = -Infinity, built = 0;
  for (const p of laid.packages) {
    minX = Math.min(minX, p.x); minZ = Math.min(minZ, p.z);
    maxX = Math.max(maxX, p.x + p.w); maxZ = Math.max(maxZ, p.z + p.d);
    for (const e of p.entities) if (e.kind !== "method") built += e.w * e.d;
  }
  const footprint = (maxX - minX) * (maxZ - minZ);
  return { footprint, density: built / footprint };
}
