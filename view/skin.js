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

export function pageVars(skin) {
  const hud = skin.hud || {};
  const steps = (skin.package && skin.package.steps) || [];
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
    "--moved": skin.change && skin.change.moved,
    "--added-text": hud.addedText || (skin.change && skin.change.added),
    "--removed-text": hud.removedText || (skin.change && skin.change.removed),
    "--modified-text": hud.modifiedText || (skin.change && skin.change.modified),
    "--moved-text": hud.movedText || (skin.change && skin.change.moved),
    "--call": skin.call && skin.call.color,
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
  skin.url = record.url;
  return skin;
}
