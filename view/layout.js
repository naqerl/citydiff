// Layout places packages as terraces. A package sits on its parent's top
// face, so the root module is the plaza and the packages inside it step up.
// Coordinates are y-up. A box is its minimum corner plus its size.
// The front of a terrace is +z, which is the side the opening camera looks from.

const GAP = 0.34;

export function towerHeight(bytes) {
  const n = Math.max(0, Number(bytes) || 0);
  return 1.25 + Math.log2(n + 1) * 1.3;
}

// A method is laid on the type's full roof. The view draws that roof shorter
// until the camera is close. Drop the method by the height the type has not
// grown yet, so its base stays on the roof that is actually drawn.
export function methodDrawY(layoutY, typeLayoutH, typeDrawH) {
  return layoutY - typeLayoutH + typeDrawH;
}

export function layoutCity(packages, options = {}) {
  const size = options.size ?? 150;
  const internals = (packages || []).filter((pkg) => !pkg.external);
  const byId = new Map(internals.map((pkg) => [pkg.id, pkg]));
  const childIds = new Map();
  const roots = [];
  for (const pkg of internals) {
    if (pkg.parent && byId.has(pkg.parent)) {
      const list = childIds.get(pkg.parent) || [];
      list.push(pkg.id);
      childIds.set(pkg.parent, list);
    } else {
      roots.push(pkg.id);
    }
  }

  const weightOf = (id) => {
    const pkg = byId.get(id);
    let weight = 4;
    for (const entity of pkg.entities || []) {
      if (entity.kind === "type" || entity.kind === "function" || entity.kind === "method") weight += 1.7;
    }
    for (const child of childIds.get(id) || []) weight += weightOf(child);
    return weight;
  };
  const makeNode = (id) => ({
    pkg: byId.get(id),
    weight: weightOf(id),
    children: (childIds.get(id) || []).map(makeNode).sort((a, b) => b.weight - a.weight || a.pkg.id.localeCompare(b.pkg.id)),
  });
  const rootNodes = roots.map(makeNode).sort((a, b) => a.pkg.id.localeCompare(b.pkg.id));

  const placed = [];
  if (rootNodes.length === 1) {
    placePackage(rootNodes[0], -size / 2, -size / 2, size, size, 0, 0, placed);
  } else if (rootNodes.length > 1) {
    const span = size * Math.max(1, Math.sqrt(rootNodes.length));
    const cells = treemap(rootNodes.map((node) => ({ weight: node.weight, node })), -span / 2, -size / 2, span, size);
    for (const cell of cells) {
      placePackage(cell.node, cell.x, cell.z, cell.w, cell.d, 0, 0, placed);
    }
  }

  let minX = Infinity, minY = Infinity, minZ = Infinity;
  let maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
  const grow = (x, y, z, w, h, d) => {
    minX = Math.min(minX, x); minY = Math.min(minY, y); minZ = Math.min(minZ, z);
    maxX = Math.max(maxX, x + w); maxY = Math.max(maxY, y + h); maxZ = Math.max(maxZ, z + d);
  };
  for (const pkg of placed) {
    grow(pkg.x, pkg.y, pkg.z, pkg.w, pkg.h, pkg.d);
    for (const entity of pkg.entities) grow(entity.x, entity.y, entity.z, entity.w, entity.h, entity.d);
  }
  if (!Number.isFinite(minX)) {
    minX = -size / 2; minY = 0; minZ = -size / 2;
    maxX = size / 2; maxY = 1; maxZ = size / 2;
  }
  const bounds = { minX, minY, minZ, maxX, maxY, maxZ };
  const cx = (minX + maxX) / 2;
  const cz = (minZ + maxZ) / 2;
  const radius = Math.max(maxX - minX, maxZ - minZ) * 0.62 + 16;
  const externals = (packages || []).filter((pkg) => pkg.external).sort((a, b) => a.id.localeCompare(b.id));
  const externalBoxes = externals.map((pkg, index) => {
    const angle = (index / Math.max(1, externals.length)) * Math.PI * 2 - Math.PI / 2;
    return { id: pkg.id, x: cx + Math.cos(angle) * radius, y: 0.6, z: cz + Math.sin(angle) * radius, w: 1.4, h: 2.4, d: 1.4 };
  });
  return { packages: placed, externals: externalBoxes, bounds };
}

function placePackage(node, x, z, w, d, y, depth, out) {
  const locals = blocksOf(node.pkg);
  const pad = Math.min(w, d) * 0.05 + 0.55;
  const ix = x + pad;
  const iz = z + pad;
  const iw = Math.max(1, w - pad * 2);
  const id = Math.max(1, d - pad * 2);
  const plinthH = 3.1 + Math.log2(locals.length + 1) * 1.55;
  const kids = node.children || [];

  let entityRect = null;
  let childRect = null;
  if (kids.length === 0) {
    entityRect = { x: ix, z: iz, w: iw, d: id };
  } else if (locals.length === 0) {
    childRect = { x: ix, z: iz, w: iw, d: id };
  } else {
    const share = clamp((locals.length * 3.4) / (iw * id), 0.2, 0.56);
    const band = Math.max(2.2, id * share);
    const gap = Math.min(1.4, id * 0.05);
    entityRect = { x: ix, z: iz + id - band, w: iw, d: band };
    childRect = { x: ix, z: iz, w: iw, d: Math.max(1, id - band - gap) };
  }

  const record = {
    id: node.pkg.id,
    name: node.pkg.name || node.pkg.id,
    synthetic: !!node.pkg.synthetic,
    x, y, z, w, h: plinthH, d, depth,
    entities: [],
  };
  if (entityRect && locals.length) {
    record.entities = placeEntities(locals, entityRect, y + plinthH);
  }
  out.push(record);
  if (childRect && kids.length) {
    const cells = treemap(kids.map((child) => ({ weight: child.weight, node: child })), childRect.x, childRect.z, childRect.w, childRect.d);
    for (const cell of cells) placePackage(cell.node, cell.x, cell.z, cell.w, cell.d, y + plinthH, depth + 1, out);
  }
}

function blocksOf(pkg) {
  const entities = (pkg.entities || []).filter((entity) => entity.kind === "type" || entity.kind === "function" || entity.kind === "method");
  const methods = new Map();
  for (const entity of entities) {
    if (entity.kind !== "method" || !entity.parent) continue;
    const list = methods.get(entity.parent) || [];
    list.push(entity);
    methods.set(entity.parent, list);
  }
  const items = [];
  const parked = new Set();
  for (const entity of entities) {
    if (entity.kind !== "type") continue;
    const owned = methods.get(entity.id) || [];
    for (const method of owned) parked.add(method.id);
    const cols = Math.max(1, Math.ceil(Math.sqrt(owned.length || 1)));
    const rows = Math.max(1, Math.ceil((owned.length || 1) / cols));
    const mw = 1.05;
    const md = 1.05;
    const footprintW = owned.length ? cols * mw + (cols - 1) * GAP + 0.85 : 2.15;
    const footprintD = owned.length ? rows * md + (rows - 1) * GAP + 0.85 : 2.15;
    items.push({ entity, kind: "type", w: footprintW, d: footprintD, methods: owned, cols, mw, md });
  }
  for (const entity of entities) {
    if (entity.kind === "method" && parked.has(entity.id)) continue;
    if (entity.kind === "type") continue;
    items.push({ entity, kind: entity.kind, w: 1.05, d: 1.05, methods: [] });
  }
  return items;
}

function displayBytes(entity) {
  if (entity.change === "removed") return entity.bodyBytesBefore || 0;
  return entity.bodyBytes || 0;
}

function placeEntities(items, rect, y) {
  const packed = shelfPack(items, rect.x, rect.z, rect.w, rect.d);
  const out = [];
  for (const item of packed) {
    if (item.kind === "type") {
      const typeH = 1.4;
      out.push({ id: item.entity.id, kind: "type", x: item.x, y, z: item.z, w: item.w, h: typeH, d: item.d });
      item.methods.forEach((method, index) => {
        const col = index % item.cols;
        const row = Math.floor(index / item.cols);
        const sx = item.w / item.baseW;
        const sz = item.d / item.baseD;
        const mw = item.mw * sx;
        const md = item.md * sz;
        out.push({
          id: method.id,
          kind: "method",
          x: item.x + 0.42 * sx + col * (item.mw + GAP) * sx,
          y: y + typeH,
          z: item.z + 0.42 * sz + row * (item.md + GAP) * sz,
          w: Math.max(0.35, mw),
          h: towerHeight(displayBytes(method)),
          d: Math.max(0.35, md),
        });
      });
      continue;
    }
    out.push({
      id: item.entity.id,
      kind: item.kind,
      x: item.x,
      y,
      z: item.z,
      w: item.w,
      h: towerHeight(displayBytes(item.entity)),
      d: item.d,
    });
  }
  return out;
}

function shelfPack(items, x, z, w, d) {
  const packAt = (scale, limitW) => {
    const gap = GAP * scale;
    let cx = 0;
    let cz = 0;
    let rowD = 0;
    const out = [];
    for (const item of items) {
      const iw = item.w * scale;
      const id = item.d * scale;
      if (cx > 0 && cx + iw > limitW) {
        cz += rowD + gap;
        cx = 0;
        rowD = 0;
      }
      out.push({ ...item, x: cx, z: cz, w: iw, d: id, baseW: item.w, baseD: item.d });
      cx += iw + gap;
      rowD = Math.max(rowD, id);
    }
    const usedW = out.reduce((max, item) => Math.max(max, item.x + item.w), 0);
    const usedD = out.reduce((max, item) => Math.max(max, item.z + item.d), 0);
    return { out, usedW, usedD };
  };
  let packed = packAt(1, Math.max(w, 0.1));
  if (packed.usedW > w || packed.usedD > d) {
    let scale = Math.min(w / Math.max(packed.usedW, 0.001), d / Math.max(packed.usedD, 0.001));
    packed = packAt(Math.max(scale, 0.08), Math.max(w, 0.1));
    if (packed.usedD > d || packed.usedW > w) {
      scale *= Math.min(w / Math.max(packed.usedW, 0.001), d / Math.max(packed.usedD, 0.001)) * 0.98;
      packed = packAt(Math.max(scale, 0.05), Math.max(w, 0.1));
    }
  }
  const ox = x + Math.max(0, (w - packed.usedW) / 2);
  const oz = z + Math.max(0, (d - packed.usedD) / 2);
  return packed.out.map((item) => ({ ...item, x: item.x + ox, z: item.z + oz }));
}

function treemap(items, x, z, w, d) {
  const out = [];
  if (!items.length || w <= 0 || d <= 0) return out;
  const total = items.reduce((sum, item) => sum + Math.max(item.weight, 0.001), 0);
  const scale = (w * d) / total;
  const rows = items.map((item) => ({ ...item, area: Math.max(item.weight, 0.001) * scale }));
  squarify(rows, [], x, z, w, d, out);
  return out;
}

function squarify(children, row, x, z, w, d, out) {
  if (!children.length) {
    if (row.length) layoutRow(row, x, z, w, d, out);
    return;
  }
  const short = Math.min(w, d);
  if (!row.length || worst(row, short) >= worst(row.concat([children[0]]), short)) {
    squarify(children.slice(1), row.concat([children[0]]), x, z, w, d, out);
    return;
  }
  const next = layoutRow(row, x, z, w, d, out);
  squarify(children, [], next.x, next.z, next.w, next.d, out);
}

function worst(row, w) {
  const sum = row.reduce((total, item) => total + item.area, 0);
  let rmax = 0;
  let rmin = Infinity;
  for (const item of row) {
    rmax = Math.max(rmax, item.area);
    rmin = Math.min(rmin, item.area);
  }
  const w2 = w * w;
  const s2 = sum * sum;
  return Math.max((w2 * rmax) / s2, s2 / (w2 * rmin));
}

function layoutRow(row, x, z, w, d, out) {
  const sum = row.reduce((total, item) => total + item.area, 0);
  if (w >= d) {
    const thickness = sum / Math.max(d, 0.001);
    let offset = z;
    for (const item of row) {
      const len = item.area / Math.max(thickness, 0.001);
      out.push({ ...item, x, z: offset, w: thickness, d: len });
      offset += len;
    }
    return { x: x + thickness, z, w: Math.max(0, w - thickness), d };
  }
  const thickness = sum / Math.max(w, 0.001);
  let offset = x;
  for (const item of row) {
    const len = item.area / Math.max(thickness, 0.001);
    out.push({ ...item, x: offset, z, w: len, d: thickness });
    offset += len;
  }
  return { x, z: z + thickness, w, d: Math.max(0, d - thickness) };
}

function clamp(value, min, max) {
  return Math.max(min, Math.min(max, value));
}

// Distance from `look` along `dir` at which every point falls inside the
// perspective frame. `margin` is the NDC inset, so 1 is flush with the edge.
// The basis matches PerspectiveCamera.lookAt with up = (0, 1, 0): backward is
// eye − target, right is up × backward.
export function fitDistance(points, look, dir, fovDeg, aspect, margin) {
  const len = Math.hypot(dir.x, dir.y, dir.z);
  if (!points.length || len < 1e-8 || !(aspect > 0)) return 10;
  const dx = dir.x / len;
  const dy = dir.y / len;
  const dz = dir.z / len;
  const fits = (dist) => {
    const eye = { x: look.x + dx * dist, y: look.y + dy * dist, z: look.z + dz * dist };
    for (const point of points) {
      const ndc = projectPoint(point, eye, look, fovDeg, aspect);
      if (!ndc || Math.abs(ndc.x) > margin || Math.abs(ndc.y) > margin) return false;
    }
    return true;
  };
  let hi = 1;
  while (hi < 1e7 && !fits(hi)) hi *= 1.7;
  let lo = 0.25;
  for (let i = 0; i < 32; i++) {
    const mid = (lo + hi) / 2;
    if (fits(mid)) hi = mid;
    else lo = mid;
  }
  return hi;
}

function projectPoint(point, eye, target, fovDeg, aspect) {
  const zx = eye.x - target.x;
  const zy = eye.y - target.y;
  const zz = eye.z - target.z;
  const zl = Math.hypot(zx, zy, zz);
  if (zl < 1e-8) return null;
  const zaxis = { x: zx / zl, y: zy / zl, z: zz / zl };
  let xx = zaxis.z;
  let xz = -zaxis.x;
  const xl = Math.hypot(xx, xz);
  if (xl < 1e-8) return null;
  xx /= xl;
  xz /= xl;
  const yx = zaxis.y * xz;
  const yy = zaxis.z * xx - zaxis.x * xz;
  const yz = -zaxis.y * xx;
  const px = point.x - eye.x;
  const py = point.y - eye.y;
  const pz = point.z - eye.z;
  const camX = px * xx + pz * xz;
  const camY = px * yx + py * yy + pz * yz;
  const camZ = px * zaxis.x + py * zaxis.y + pz * zaxis.z;
  if (camZ >= -1e-4) return null;
  const tanHalf = Math.tan((fovDeg * Math.PI / 180) / 2);
  return {
    x: (camX / -camZ) / (aspect * tanHalf),
    y: (camY / -camZ) / tanHalf,
  };
}
