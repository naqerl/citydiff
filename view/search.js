// rankMatches picks packages and declarations for the search box.
// A name that equals the query wins, then a name that starts with it,
// then a name that contains it, then an id or package path that contains it.
export function rankMatches(items, query, limit = 12) {
  const q = String(query || "").trim().toLowerCase();
  if (!q) return [];
  const scored = [];
  for (const item of items) {
    const name = String(item.name || "").toLowerCase();
    const id = String(item.id || "").toLowerCase();
    const extra = String(item.extra || "").toLowerCase();
    let score = 0;
    if (name === q) score = 100;
    else if (name.startsWith(q)) score = 80;
    else if (name.includes(q)) score = 60;
    else if (id.includes(q) || extra.includes(q)) score = 40;
    else continue;
    if (item.kind === "package") score += 6;
    else if (item.kind === "function" || item.kind === "method") score += 3;
    else if (item.kind === "type" || item.kind === "variable") score += 2;
    scored.push({ item, score });
  }
  scored.sort((a, b) => b.score - a.score || String(a.item.name).localeCompare(String(b.item.name)));
  return scored.slice(0, limit).map((row) => row.item);
}
