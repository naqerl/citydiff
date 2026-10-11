// A skin is a JSON object. Each key is one thing the viewer draws.
// A file may set any subset. Omitted keys keep the dark skin.
// Built-ins and skins from $CITYDIFF_SKINS_DIR are served at /skins/<name>/skin.json.
// A directory may ship skin.js. The page runs it once and keeps the JSON it returns.
// The page address takes a skin name. A path or a URL there is refused.

export const ELEMENTS = [
  "background",
  "fog",
  "plane",
  "ground",
  "horizon",
  "light",
  "package",
  "external",
  "type",
  "function",
  "method",
  "entity",
  "change",
  "call",
  "selection",
  "label",
  "dim",
  "hud",
];

const DARK_URL = "/skins/dark/skin.json";

const SKIN_NAME = /^[A-Za-z0-9_-]+$/;

// The page owns the choice: an empty spec is the built-in dark skin, a name
// is a skin the server serves. A path or a URL is not a name.
// cycleSkin is the theme step from current in the menu's list. The list is a
// ring: before the first theme is the last, and after the last the first. A
// current that is not in the list steps from the first. With fewer than two
// themes there is nowhere to go, and the answer is "".
export function cycleSkin(list, current, step) {
  if (!list || list.length < 2 || !step) return "";
  const at = Math.max(0, list.indexOf(current));
  const n = list.length;
  return list[(((at + step) % n) + n) % n];
}

export function skinURL(spec) {
  const name = String(spec ?? "").trim();
  if (!name) return DARK_URL;
  if (!SKIN_NAME.test(name)) return "";
  return "/skins/" + name + "/skin.json";
}

export function assetURL(base, ref) {
  const value = String(ref || "").trim();
  if (!value) return "";
  if (/^[a-z][a-z0-9+.-]*:/i.test(value) || value.startsWith("/")) return value;
  return new URL(value, base || DARK_URL).href;
}

// background.skybox is a gradient ({top, horizon, bottom}), one equirectangular
// image, or six cube faces in +x -x +y -y +z -z order.
export function skyboxKind(skybox) {
  if (typeof skybox === "string") return "equirect";
  if (Array.isArray(skybox)) return "cube";
  if (skybox && typeof skybox === "object") return "gradient";
  return "color";
}

export function mergeSkin(base, overlay) {
  if (Array.isArray(overlay)) return overlay.slice();
  if (overlay && typeof overlay === "object") {
    const out = (base && typeof base === "object" && !Array.isArray(base)) ? { ...base } : {};
    for (const key of Object.keys(overlay)) out[key] = mergeSkin(out[key], overlay[key]);
    return out;
  }
  if (overlay === undefined) return base;
  return overlay;
}

// The root package is the big box the rest of the city stands on. When the
// skin does not name one, it would take the ground's own colour, and the box
// would vanish into the disk under it. Every skin that does not set
// package.root gets a colour with a visible separation from the ground it
// stands on and the plane behind it: the base colour moved in lightness away
// from the ground until the contrast is enough. A skin that names its own
// package.root keeps it, whatever it is.
const ROOT_CONTRAST = 1.6;

function hexChannels(hex) {
  const value = String(hex == null ? "" : hex).replace("#", "");
  if (!/^[0-9a-fA-F]{6}$/.test(value)) return null;
  return [0, 2, 4].map((i) => parseInt(value.slice(i, i + 2), 16));
}

function linearChannel(value) {
  const v = value / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}

function luminance(hex) {
  const rgb = hexChannels(hex);
  if (!rgb) return null;
  return 0.2126 * linearChannel(rgb[0]) + 0.7152 * linearChannel(rgb[1]) + 0.0722 * linearChannel(rgb[2]);
}

function contrastRatio(a, b) {
  const la = luminance(a);
  const lb = luminance(b);
  if (la == null || lb == null) return null;
  const hi = Math.max(la, lb);
  const lo = Math.min(la, lb);
  return (hi + 0.05) / (lo + 0.05);
}

function channelsToHex(rgb) {
  return "#" + rgb.map((v) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, "0")).join("");
}

function channelsToHsl(rgb) {
  const [r, g, b] = rgb.map((v) => v / 255);
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const h = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [h / 6, s, l];
}

function hslToChannels([h, s, l]) {
  if (s === 0) return [l, l, l].map((v) => v * 255);
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const channel = (t) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  return [channel(h + 1 / 3), channel(h), channel(h - 1 / 3)].map((v) => v * 255);
}

// rootColor is the colour of the package the whole city stands on. It is the
// skin's own package.root when it names one, and a derived colour otherwise.
export function rootColor(skin) {
  const pkg = (skin && skin.package) || {};
  if (hexChannels(pkg.root)) return pkg.root;
  const ground = hexChannels(skin && skin.ground && skin.ground.color) ? skin.ground.color : "#18181b";
  const plane = skin && skin.plane && Number(skin.plane.opacity) > 0 ? skin.plane.color : null;
  const targets = plane ? [ground, plane] : [ground];
  const enough = (hex) => targets.every((t) => {
    const ratio = contrastRatio(hex, t);
    return ratio != null && ratio >= ROOT_CONTRAST;
  });
  const base = pkg.synthetic || (Array.isArray(pkg.steps) && pkg.steps[0]) || ground;
  if (enough(base)) return base;
  const channels = hexChannels(base) || hexChannels(ground);
  const [h, s, l] = channelsToHsl(channels);
  const step = (luminance(ground) || 0) < 0.5 ? 0.012 : -0.012;
  let best = base;
  for (let i = 1; i <= 90; i++) {
    const nl = Math.min(1, Math.max(0, l + step * i));
    const hex = channelsToHex(hslToChannels([h, s, nl]));
    best = hex;
    if (enough(hex)) return hex;
    if (nl <= 0 || nl >= 1) break;
  }
  return best;
}

// hoverColor is the colour of the outline on the node under the mouse. It is
// the skin's own selection.hover when it names one. Otherwise it is an accent
// of the skin's own saturation and lightness on the hue furthest round the
// colour wheel from every hue the city already means something by — the
// changes, the calls, the selection's halo and the blocks themselves — so a
// hovered node never reads as one of them. A grey skin has no hue to spare:
// its hover is its text colour, and it stays grey.
const HOVER_GREY = 0.2;
const HOVER_CONTRAST = 4.5;

export function hoverColor(skin) {
  const selection = (skin && skin.selection) || {};
  if (hexChannels(selection.hover)) return selection.hover;
  const hud = (skin && skin.hud) || {};
  const accent = hexChannels(hud.accent) || hexChannels(selection.color);
  if (!accent) return hexChannels(hud.fg) ? hud.fg : "#93c5fd";
  const [h, s, l] = channelsToHsl(accent);
  if (s < HOVER_GREY) return hexChannels(hud.fg) ? hud.fg : channelsToHex(accent);
  const change = (skin && skin.change) || {};
  const pick = (key) => (skin && skin[key] && skin[key].color);
  const taken = [
    change.added, change.removed, change.modified, change.body, change.both, change.moved,
    pick("call"), selection.color, pick("type"), pick("function"), pick("method"),
  ]
    .map(hexChannels)
    .filter(Boolean)
    .map(channelsToHsl)
    .filter(([, sat]) => sat >= HOVER_GREY)
    .map(([hue]) => hue);
  // Hues are turns, 0 to 1. The distance between two is the short way round.
  const apart = (a, b) => {
    const d = Math.abs(a - b) % 1;
    return Math.min(d, 1 - d);
  };
  let hue = h;
  let gap = -1;
  for (let step = 0; step < 360; step++) {
    const at = step / 360;
    const near = taken.length ? Math.min(...taken.map((t) => apart(at, t))) : 0.5;
    // Of two hues as free as each other, the one nearer the skin's accent.
    if (near > gap + 1e-9 || (Math.abs(near - gap) <= 1e-9 && apart(at, h) < apart(hue, h))) {
      hue = at;
      gap = near;
    }
  }
  // The skin's lightness, moved away from the page background until the
  // outline stands off the city as plainly as the page's text does.
  const sat = Math.max(s, 0.6);
  const bg = hexChannels(hud.bg) ? hud.bg : "#09090b";
  const dir = (luminance(bg) || 0) < 0.5 ? 0.01 : -0.01;
  let light = Math.min(0.72, Math.max(0.42, l));
  let hex = channelsToHex(hslToChannels([hue, sat, light]));
  while ((contrastRatio(hex, bg) || 0) < HOVER_CONTRAST && light > 0.2 && light < 0.85) {
    light += dir;
    hex = channelsToHex(hslToChannels([hue, sat, light]));
  }
  return hex;
}

export function pageVars(skin) {
  const hud = skin.hud || {};
  const steps = (skin.package && skin.package.steps) || [];
  const change = skin.change || {};
  // The diff's syntax colours are the skin's own: a skin that names none gets
  // a palette taken from the colours it already has, so a light skin stays
  // legible and an extra skin still looks like itself.
  const syn = skin.syntax || {};
  return {
    "--scheme": hud.scheme,
    "--bg": hud.bg,
    "--elev": hud.elev,
    "--panel": hud.panel,
    "--search": hud.search,
    "--fg": hud.fg,
    "--dim": hud.dim,
    "--muted": hud.muted,
    "--faint": hud.faint,
    "--fainter": hud.fainter,
    "--line": hud.line,
    "--line-strong": hud.lineStrong,
    "--edge": hud.edge,
    "--shadow": hud.shadow,
    "--link": hud.link,
    "--code": hud.code,
    "--accent": hud.accent,
    "--pre": hud.pre,
    "--added": skin.change && skin.change.added,
    "--removed": skin.change && skin.change.removed,
    "--modified": skin.change && skin.change.modified,
    "--body": skin.change && skin.change.body,
    "--both": skin.change && skin.change.both,
    "--moved": skin.change && skin.change.moved,
    "--added-text": hud.addedText || (skin.change && skin.change.added),
    "--removed-text": hud.removedText || (skin.change && skin.change.removed),
    "--modified-text": hud.modifiedText || (skin.change && skin.change.modified),
    "--body-text": hud.bodyText || (skin.change && skin.change.body),
    "--both-text": hud.bothText || (skin.change && skin.change.both),
    "--moved-text": hud.movedText || (skin.change && skin.change.moved),
    "--call": skin.call && skin.call.color,
    "--call-std": skin.call && skin.call.std,
    "--function": skin.function && skin.function.color,
    "--method": skin.method && skin.method.color,
    "--type": skin.type && skin.type.color,
    "--package": steps[1] || (skin.package && skin.package.synthetic),
    "--added-bg": hud.addedBg,
    "--added-fg": hud.addedFg,
    "--removed-bg": hud.removedBg,
    "--removed-fg": hud.removedFg,
    "--added-line": hud.addedLine,
    "--removed-line": hud.removedLine,
    "--modified-line": hud.modifiedLine,
    "--code-bg": hud.codeBg,
    "--drop": hud.drop,
    "--drop-border": hud.dropBorder,
    "--scrim": hud.scrim,
    "--on": hud.on,
    "--current-bg": hud.currentBg,
    "--hover": hoverColor(skin),
    "--syn-keyword": syn.keyword || hud.movedText || change.moved || hud.accent,
    "--syn-string": syn.string || hud.code,
    "--syn-comment": syn.comment || hud.faint,
    "--syn-number": syn.number || (skin.call && skin.call.color),
    "--syn-type": syn.type || hud.dim,
    "--syn-func": syn.func || hud.link,
  };
}

export function applyPage(skin, scheme = true) {
  const root = document.documentElement;
  const vars = pageVars(skin);
  for (const [key, value] of Object.entries(vars)) {
    if (value != null) root.style.setProperty(key, String(value));
  }
  if (scheme && vars["--scheme"]) root.style.colorScheme = String(vars["--scheme"]);
}

// skinFromValue keeps the JSON object a script returns. Functions and
// undefined values do not survive the trip.
export function skinFromValue(value) {
  if (typeof value === "function") value = value();
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("a skin script must return a JSON object");
  }
  return JSON.parse(JSON.stringify(value));
}

async function readSkin(url, fetchImpl) {
  const res = await fetchImpl(url);
  if (res.ok) return { data: await res.json(), url: res.url || url };
  if (res.status !== 404 || !url.endsWith("/skin.json")) {
    throw new Error("Could not read skin " + url + " (" + res.status + ")");
  }
  const jsURL = url.slice(0, -"json".length) + "js";
  let mod;
  try {
    mod = await import(jsURL);
  } catch (err) {
    throw new Error("Could not read skin " + jsURL + ". " + (err && err.message ? err.message : err));
  }
  const value = mod.default;
  const data = typeof value === "function" ? await value() : value;
  return { data: skinFromValue(data), url: jsURL };
}

export async function loadSkin(spec, fetchImpl = globalThis.fetch) {
  const url = skinURL(spec);
  if (!url) throw new Error("the page address takes a skin name");
  const record = await readSkin(url, fetchImpl);
  const overlay = record.data;
  let base = overlay;
  if (url !== DARK_URL) {
    const darkRes = await fetchImpl(DARK_URL);
    if (!darkRes.ok) throw new Error("Could not read skin " + DARK_URL + " (" + darkRes.status + ")");
    base = await darkRes.json();
  }
  const skin = mergeSkin(structuredClone(base), overlay);
  skin.package.root = rootColor(skin);
  skin.selection.hover = hoverColor(skin);
  skin.url = record.url;
  return skin;
}
