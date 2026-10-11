// The action ring: the viewer's keys that have no button and no gesture of
// their own (RING in keys.js), laid round a circle so a mouse or a finger
// reaches them. It lives for one press. Holding the right button on the city
// opens it at the pointer; on a touch screen holding the sheet's grip opens it
// in the middle of the screen, over a blurred city. Moving lights the item
// pointed at, and letting go runs it; letting go in the middle runs nothing.
// An item that cannot act lights at half strength, and the line above the
// ring says why. On a touch screen the top item is Cancel, and the finger
// drives a cursor on the ring the way it drives one on a touchpad.
//
// It works the way radial menus do elsewhere (marking menus, Blender's pie
// menus, Steam Input's radial menus, game weapon wheels): the direction picks
// the item, not the exact position; a mouse's dead middle keeps a small move
// from picking anything and is where letting go cancels; the items keep their
// places, so the hand learns them; and the lit item snaps from one item to the
// next with a little hysteresis, so a pointer on the edge between two items
// does not flicker between them. The lit item is its glyph and word in the
// accent, over its slice shaded a little darker, with an accent rim on the
// slice's outer edge; nothing else on the ring is drawn but the glass.
//
// It is one SVG per kind of pointer, built on its first open. After that an
// open moves it, shows it and says which items are live, and a move only
// swaps which item is lit, when that changes: no frame of it is drawn by
// script, and its colours are the page's, so a skin repaints it for free.

import { RING, RING_TOUCH } from "./keys.js";

const SVG = "http://www.w3.org/2000/svg";
// The dial in its own units (the disc is 240 across, OUTER from the middle to
// its edge): a mouse in the middle, inside GAP, points at nothing, and each
// item's glyph and word stand at CONTENT out from the middle. An item's slice
// runs from GAP to OUTER.
const OUTER = 120;
const GAP = 26;
const CONTENT = 80;
// A finger is not over the ring: it holds the sheet's grip, at the bottom
// of the screen, and the ring is in the middle. So it drives a cursor on the
// ring, the way a finger on a touchpad drives a pointer: the cursor starts
// in the ring's middle, moves as far and the same way as the finger does,
// and stops at the rim, sliding along it, so a finger that goes too far
// never has to come back. Whatever slice the cursor is over is lit, and the
// middle lights Cancel. There is no history in it: the same cursor position
// always lights the same item, and moving back undoes a move exactly.
// CURSOR_RIM is how far out the cursor can go, in the dial's units.
const CURSOR_RIM = 112;
const CURSOR_SIZE = 8;
// The hysteresis: the lit item stays lit until the direction is this far
// (in turns) past its edge, so a cursor on the line between two items does
// not flicker between them.
const SNAP_HOLD = 10 / 360;

// moveCursor moves a cursor at c by (dx, dy) and keeps it within rim of the
// middle: past the rim it stays on it, at the angle the move took it to.
export function moveCursor(c, dx, dy, rim) {
  const x = c[0] + dx;
  const y = c[1] + dy;
  const r = Math.hypot(x, y);
  return r > rim ? [(x / r) * rim, (y / r) * rim] : [x, y];
}

// Each icon is drawn on a 16 by 16 grid, in strokes, like the sidebar's own.
const ICONS = {
  reset: "M2.5 6V2.5H6M10 2.5h3.5V6M13.5 10v3.5H10M6 13.5H2.5V10",
  diff: "M8 2.5v6M5 5.5h6M5 12.5h6",
  forward: "M2.5 8h11M9.5 4l4 4-4 4",
  bird: "M2 3.75l4-1.5 4 1.5 4-1.5v10l-4 1.5-4-1.5-4 1.5zM6 2.25v10M10 3.75v10",
  deselect: "M2.75 2.75h10.5v10.5H2.75z",
  back: "M13.5 8h-11M6.5 4l-4 4 4 4",
  edit: "M2.5 4l4 4-4 4M8 12.5h5.5",
  cancel: "M4 4l8 8M12 4l-8 8",
};

// The items of each layout: the mouse's is RING, a finger's starts with Cancel.
const CANCEL = { id: "cancel", key: "", label: "cancel" };
const LAYOUTS = {
  mouse: RING,
  touch: RING_TOUCH.map((id) => (id === "cancel" ? CANCEL : RING.find((item) => item.id === id))),
};

// ringSector is where item i of n sits: the angles its cell spans and the one
// through its middle, in radians, clockwise from the top. Item 0 is centred
// on the top.
export function ringSector(i, n) {
  const step = (Math.PI * 2) / n;
  const mid = -Math.PI / 2 + i * step;
  return { from: mid - step / 2, to: mid + step / 2, mid };
}

function at(r, angle) {
  return round(Math.cos(angle) * r) + " " + round(Math.sin(angle) * r);
}

// sectorPath is the outline of one slice: the outer arc from `from` to `to`,
// in to the inner edge, and the inner arc back.
export function sectorPath(from, to, outer, inner) {
  const large = to - from > Math.PI ? 1 : 0;
  return "M" + at(outer, from) +
    "A" + outer + " " + outer + " 0 " + large + " 1 " + at(outer, to) +
    "L" + at(inner, to) +
    "A" + inner + " " + inner + " 0 " + large + " 0 " + at(inner, from) + "Z";
}

function round(v) {
  return Math.round(v * 100) / 100;
}

// clampCenter keeps a dial of the given size whole on a screen of width by
// height, with a margin round it: a press by an edge opens the ring as near
// the pointer as it fits.
export function clampCenter(x, y, size, width, height, margin = 8) {
  const half = size / 2 + margin;
  const fit = (v, span) => (span < half * 2 ? span / 2 : Math.min(span - half, Math.max(half, v)));
  return [fit(x, width), fit(y, height)];
}

// freeCenter is the middle of what the sidebars leave of the screen.
export function freeCenter(width, height, left = 0, right = 0) {
  const free = Math.max(0, width - left - right);
  return [left + free / 2, height / 2];
}

function el(name, attrs = {}, parent = null) {
  const node = document.createElementNS(SVG, name);
  for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, String(value));
  if (parent) parent.append(node);
  return node;
}

// pickSector is the item a pointer at (dx, dy) from the middle of the ring
// points at: the cell its direction falls in, once it is out of the empty
// middle (dead), however far out. -1 when it points at nothing.
export function pickSector(dx, dy, n, dead) {
  if (Math.hypot(dx, dy) < dead) return -1;
  // Clockwise from the top, shifted half a cell so item 0 is centred there.
  const turn = (Math.atan2(dy, dx) + Math.PI / 2) / (Math.PI * 2) + 0.5 / n;
  return Math.floor((((turn % 1) + 1) % 1) * n) % n;
}

// snapSector is pickSector with the lit item held: while the direction is
// within hold (in turns) past the edge of the item already lit, that item
// stays, and only past that does the next one take over.
export function snapSector(dx, dy, n, dead, current, hold) {
  const next = pickSector(dx, dy, n, dead);
  if (next < 0 || current < 0 || next === current) return next;
  // How far the direction is from the middle of the lit item, in turns.
  const turn = (Math.atan2(dy, dx) + Math.PI / 2) / (Math.PI * 2);
  let off = Math.abs((((turn - current / n) % 1) + 1) % 1);
  off = Math.min(off, 1 - off);
  return off <= 0.5 / n + hold ? current : next;
}

// A mouse has to move this far from where it was when the ring opened before
// it points at anything, so a dial pushed off the pointer by the screen's
// edge runs nothing it was not moved to.
const ARM_PX = 8;

// mountRing builds nothing until the first open. The ring lives for one press:
// open() when the button or the finger goes down, track() as it moves, which
// lights the item it points at, and release() when it comes up, which runs
// that item, or nothing from the middle or from an item that cannot act.
// state(id) says whether an item is live ({ enabled, on, why }) and run(id)
// does it. onOpen and onClose(reason) let the viewer hold the city still
// under a touch ring; the reason is "run" when an item ran and "cancel"
// otherwise.
export function mountRing({ state, run, onOpen, onClose }) {
  let root = null;
  let why = null;
  // One dial per kind of pointer, each with its items and, for a finger, its
  // cursor: { dial, items, cursor }.
  const dials = {};
  let dial = null;
  let items = [];
  let cursor = null;
  let where = null;
  let hot = -1;
  let press = null;

  function build() {
    root = document.createElement("div");
    root.id = "ring";
    root.hidden = true;
    why = document.createElement("div");
    why.className = "ring-why";
    why.setAttribute("role", "status");
    why.hidden = true;
    root.append(why);
    document.body.append(root);
    // A touch ring that somehow outlives its finger closes on a tap.
    root.addEventListener("click", () => hide("cancel"));
    root.addEventListener("contextmenu", (event) => event.preventDefault());
  }

  function buildDial(kind) {
    const layout = LAYOUTS[kind];
    const box = document.createElement("div");
    box.className = "ring-dial";
    box.setAttribute("role", "menu");
    box.setAttribute("aria-label", "Actions");
    box.hidden = true;
    const art = el("svg", { viewBox: "-120 -120 240 240" }, box);
    const list = layout.map((item, i) => {
      const { from, to, mid } = ringSector(i, layout.length);
      const g = el("g", { class: "ring-item", role: "menuitem", "data-id": item.id }, art);
      g.setAttribute("aria-label", item.key ? item.label + " (" + item.key + ")" : item.label);
      // The slice, drawn only while the item is lit, as a shade on the glass,
      // and the rim along its outer edge, a hair inside the disc's.
      el("path", { class: "ring-cell", d: sectorPath(from, to, OUTER, GAP) }, g);
      // It runs the slice's whole width, edge to edge, and its stroke's outer
      // side lies on the disc's edge.
      const rim = OUTER - 1.5;
      el("path", { class: "ring-rim", d: "M" + at(rim, from) + "A" + rim + " " + rim + " 0 0 1 " + at(rim, to) }, g);
      const cx = round(Math.cos(mid) * CONTENT);
      const cy = round(Math.sin(mid) * CONTENT);
      const ink = el("g", { class: "ring-ink" }, g);
      el("path", { class: "ring-icon", d: ICONS[item.id] || "", transform: "translate(" + (cx - 8) + " " + (cy - 21) + ")" }, ink);
      el("text", { class: "ring-label", x: cx, y: cy + 9 }, ink).textContent = item.label;
      if (item.key) el("text", { class: "ring-key", x: cx, y: cy + 21 }, ink).textContent = item.key;
      return { id: item.id, g, live: true, why: "" };
    });
    // A finger's cursor, a small square like the rest of the page's corners,
    // drawn over the items. A mouse has its own pointer.
    const dot = kind === "touch" ? el("rect", { class: "ring-cursor", width: CURSOR_SIZE, height: CURSOR_SIZE }, art) : null;
    root.insertBefore(box, why);
    return { dial: box, items: list, cursor: dot };
  }

  // quiet lights without the phone's tick: the item the ring opens on.
  function light(index, quiet = false) {
    if (index === hot) return;
    if (hot >= 0) items[hot].g.classList.remove("is-hot");
    hot = index;
    const entry = hot >= 0 ? items[hot] : null;
    if (entry) entry.g.classList.add("is-hot");
    const reason = entry && !entry.live ? entry.why : "";
    why.textContent = reason;
    why.hidden = !reason;
    // A finger feels each snap, where the phone can say so.
    if (entry && !quiet && press && press.touch && navigator.vibrate) navigator.vibrate(6);
  }

  function place() {
    if (!where) return;
    const [x, y] = where();
    const size = dial.offsetWidth || 240;
    const [cx, cy] = clampCenter(x, y, size, window.innerWidth, window.innerHeight);
    dial.style.left = cx + "px";
    dial.style.top = cy + "px";
    // The reason sits right above the ring, or right under it when the ring
    // is too near the top of the screen for a line above it.
    const above = cy - size / 2 > 44;
    why.style.left = cx + "px";
    why.style.top = (above ? cy - size / 2 - 10 : cy + size / 2 + 10) + "px";
    why.classList.toggle("is-below", !above);
    press.center = [cx, cy];
    press.scale = size / 240;
  }

  // open takes where to put the middle of the ring, as a function, so a turn
  // of the phone or a resize puts it back in the same place of the new
  // screen, and from, where the pointer is now. touch blurs what is behind
  // it, and reads the finger as a gesture from where it went down.
  function show({ at: anchor, from, touch = false }) {
    if (!root) build();
    const kind = touch ? "touch" : "mouse";
    if (!dials[kind]) dials[kind] = buildDial(kind);
    if (dial !== dials[kind].dial) {
      light(-1);
      if (dial) dial.hidden = true;
      ({ dial, items, cursor } = dials[kind]);
      dial.hidden = false;
    }
    for (const entry of items) {
      // Cancel is always there to be had, and runs nothing.
      const s = entry.id === "cancel" ? {} : state(entry.id) || {};
      entry.live = s.enabled !== false;
      entry.why = s.why || "";
      entry.g.setAttribute("aria-disabled", entry.live ? "false" : "true");
      entry.g.classList.toggle("on", !!s.on);
    }
    const wasOpen = !root.hidden;
    where = anchor;
    press = { from, last: from, cursor: [0, 0], armed: false, touch, center: [0, 0], scale: 1 };
    press.cancel = items.findIndex((entry) => entry.id === "cancel");
    light(touch ? press.cancel : -1, true);
    if (cursor) placeCursor();
    root.classList.toggle("is-touch", !!touch);
    root.hidden = false;
    place();
    if (!wasOpen && onOpen) onOpen();
  }

  function track(x, y) {
    if (!press) return;
    if (press.touch) {
      const s = press.scale;
      press.cursor = moveCursor(press.cursor, x - press.last[0], y - press.last[1], CURSOR_RIM * s);
      press.last = [x, y];
      placeCursor();
      const next = snapSector(press.cursor[0], press.cursor[1], items.length, GAP * s, hot, SNAP_HOLD);
      light(next >= 0 ? next : press.cancel);
      return;
    }
    if (!press.armed) {
      if (Math.hypot(x - press.from[0], y - press.from[1]) < ARM_PX) return;
      press.armed = true;
    }
    light(snapSector(x - press.center[0], y - press.center[1], items.length, GAP * press.scale, hot, SNAP_HOLD));
  }

  // The cursor's square, centred on the cursor, in the dial's own units.
  function placeCursor() {
    cursor.setAttribute("x", round(press.cursor[0] / press.scale - CURSOR_SIZE / 2));
    cursor.setAttribute("y", round(press.cursor[1] / press.scale - CURSOR_SIZE / 2));
  }

  // The button or the finger came up: run what it points at, if it can act.
  function release() {
    if (!press) return;
    const entry = hot >= 0 ? items[hot] : null;
    const id = entry && entry.live && entry.id !== "cancel" ? entry.id : "";
    hide(id ? "run" : "cancel");
    if (id) run(id);
  }

  function hide(reason = "cancel") {
    if (!root || root.hidden) return;
    light(-1);
    root.hidden = true;
    where = null;
    press = null;
    if (onClose) onClose(reason);
  }

  // Escape lets go of the ring without running anything, and the viewer's own
  // Escape does not fire under it. Every other key does what it always does.
  window.addEventListener("keydown", (event) => {
    if (!root || root.hidden || event.key !== "Escape") return;
    event.preventDefault();
    event.stopImmediatePropagation();
    hide("cancel");
  }, true);
  window.addEventListener("blur", () => hide("cancel"));
  window.addEventListener("resize", () => {
    if (root && !root.hidden) place();
  });

  return {
    open: show,
    track,
    release,
    close: hide,
    isOpen: () => !!root && !root.hidden,
  };
}
