// Package-level arcs for the overview.

const RANK = { same: 0, modified: 1, removed: 2, added: 3 };

// packageCallEdges folds changed calls into one edge per pair of packages.
// Each call is { from, to, change } with package ids. Calls inside one
// package and unchanged calls are left out. Added and deleted calls between
// the same two packages read as changed.
export function packageCallEdges(calls) {
  const byPair = new Map();
  for (const call of calls) {
    if (!call.from || !call.to || call.from === call.to) continue;
    if (!call.change || call.change === "same") continue;
    const key = call.from + "\0" + call.to;
    const prev = byPair.get(key);
    if (!prev) {
      byPair.set(key, { from: call.from, to: call.to, change: call.change, changes: new Set([call.change]) });
      continue;
    }
    prev.changes.add(call.change);
    if ((RANK[call.change] || 0) > (RANK[prev.change] || 0)) prev.change = call.change;
  }
  const out = [];
  for (const edge of byPair.values()) {
    const change = edge.changes.has("added") && edge.changes.has("removed") ? "modified" : edge.change;
    out.push({ from: edge.from, to: edge.to, change });
  }
  return out;
}
