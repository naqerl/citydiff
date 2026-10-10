import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { deletedFirst } from "./changes.js";
import { layoutCity, drawnSize, drawnBox, oldBodyBox, fitDistance } from "./layout.js";
import { rankMatches } from "./search.js";
import { flyStep } from "./fly.js";
import { mountTour } from "./tourui.js";
import { insets, viewOffsetX, viewOffsetY, freeFov, fitPose, boxOf } from "./viewport.js";
import { BirdToggle, chooseDistricts, districtStatus, depEdges, aggregateEdges, labelBox, placeLabels } from "./bird.js";
import { KEYBINDS, birdAction, editAction, diffAction } from "./keys.js";
import { nameIndex, writeNode, readNode, readState, writeState } from "./state.js";
import { editTarget, openEditor, closeEditor, editorActive } from "./editor.js";
import { diffTarget, openDiff, closeDiff, diffActive } from "./diff.js";
import { applyPage, loadSkin } from "./skin.js";
import { dress, loadShade } from "./shade.js";
import { applyGradient, applySky } from "./sky.js";

let theme = null;
let themeReady = false;
let themeFade = null;
let skinWarning = "";
const THEME_FADE_MS = 450;
const THEME_REVEAL_MS = 800;
const paint = {
  added: new THREE.Color(),
  removed: new THREE.Color(),
  modified: new THREE.Color(),
  body: new THREE.Color(),
  both: new THREE.Color(),
  moved: new THREE.Color(),
  same: new THREE.Color(),
  call: new THREE.Color(),
  std: new THREE.Color(),
  type: new THREE.Color(),
  function: new THREE.Color(),
  method: new THREE.Color(),
  selection: new THREE.Color(),
};

function syncPaint(next) {
  paint.added.set(next.change.added);
  paint.removed.set(next.change.removed);
  paint.modified.set(next.change.modified);
  paint.body.set(next.change.body || next.change.modified);
  paint.both.set(next.change.both || next.change.modified);
  paint.moved.set(next.change.moved);
  paint.same.set(next.change.same);
  paint.call.set(next.call.color);
  paint.std.set(next.call.std);
  paint.type.set(next.type.color);
  paint.function.set(next.function.color);
  paint.method.set(next.method.color);
  paint.selection.set(next.selection.color);
}

function useTheme(next) {
  theme = next;
  syncPaint(next);
  applyPage(next);
  applySky(scene, next);
  renderer.toneMappingExposure = next.light.exposure;
  renderer.setClearColor(next.background.color);
  scene.fog.color.set(next.fog.color);
  ring.material.color.set(next.selection.ring);
  ring.material.opacity = next.selection.ringOpacity;
  haloMaterial.uniforms.uColor.value.copy(paint.selection);
  themeReady = true;
  document.documentElement.classList.add("theme-ready");
}

// The theme is a browser preference: it is remembered in localStorage, never
// in the address, and the process has no say in it.
async function readTheme() {
  const spec = storedSkin();
  try {
    const skin = await loadSkin(spec);
    await loadShade(skin);
    if (skin.shadeWarning) skinWarning = skin.shadeWarning;
    return skin;
  } catch (err) {
    if (spec !== "dark") {
      try {
        skinWarning = "Could not read skin " + spec + ". " + err.message;
        const skin = await loadSkin("dark");
        await loadShade(skin);
        return skin;
      } catch { /* the dark skin failed too */ }
    }
    skinWarning = "Could not read the skin. " + err.message;
    return null;
  }
}

function shadeKey(spec, skin) {
  const src = spec || {};
  const maps = src.maps && typeof src.maps === "object"
    ? Object.keys(src.maps).sort().map((key) => key + ":" + src.maps[key]).join(",")
    : "";
  const fog = (skin && skin.fog && skin.fog.fragmentSource) || "";
  return [src.vertexSource || "", src.fragmentSource || "", src.map || "", maps, fog].join("\0");
}

function rememberShade(mat, spec) {
  mat.userData.shadeKey = shadeKey(spec, theme);
  return mat;
}

function parseCSSColor(value) {
  const text = String(value || "").trim();
  if (text === "transparent") return [0, 0, 0, 0];
  const hex = text.replace("#", "");
  if (/^[0-9a-fA-F]{6}$/.test(hex)) {
    return [parseInt(hex.slice(0, 2), 16), parseInt(hex.slice(2, 4), 16), parseInt(hex.slice(4, 6), 16), 1];
  }
  const match = text.match(/rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)\s*(?:,\s*([\d.]+)\s*)?\)/i);
  if (match) return [Number(match[1]), Number(match[2]), Number(match[3]), match[4] == null ? 1 : Number(match[4])];
  return [0, 0, 0, 1];
}

function formatCSSColor(color) {
  if (color[3] <= 0.001) return "transparent";
  const rgb = color.slice(0, 3).map((channel) => Math.round(channel));
  if (color[3] >= 0.999) return "#" + rgb.map((channel) => channel.toString(16).padStart(2, "0")).join("");
  return "rgba(" + rgb.join(", ") + ", " + color[3].toFixed(3) + ")";
}

function mixCSS(from, to, k) {
  const a = parseCSSColor(from);
  const b = parseCSSColor(to);
  return formatCSSColor(a.map((channel, index) => channel + (b[index] - channel) * k));
}

function skyColors(skin) {
  const box = skin.background && skin.background.skybox;
  if (!box || typeof box !== "object" || Array.isArray(box) || !box.top) return null;
  return { top: box.top, horizon: box.horizon, bottom: box.bottom };
}

function swapMaterial(mesh, spec, skin, factory) {
  if (!mesh) return null;
  const key = shadeKey(spec, skin);
  const old = mesh.material;
  if (old && old.userData.shadeKey === key) return old;
  const created = factory();
  if (old) {
    if (old.color && created.color) created.color.copy(old.color);
    if (old.emissive && created.emissive) created.emissive.copy(old.emissive);
    if (typeof old.emissiveIntensity === "number" && typeof created.emissiveIntensity === "number") created.emissiveIntensity = old.emissiveIntensity;
    if (typeof old.opacity === "number") created.opacity = old.opacity;
    if (typeof old.metalness === "number") created.metalness = old.metalness;
    if (typeof old.roughness === "number") created.roughness = old.roughness;
    created.transparent = old.transparent;
    created.depthWrite = old.depthWrite;
    created.side = old.side;
    if (old.vertexColors) created.vertexColors = true;
    old.dispose();
  }
  created.userData.shadeKey = key;
  mesh.material = created;
  return created;
}

function adoptShaders(next) {
  if (planeMesh) {
    const segs = next.plane.vertexSource ? (next.plane.segments || 64) : 1;
    const params = planeMesh.geometry.parameters;
    if (params.widthSegments !== segs) {
      planeMesh.geometry.dispose();
      planeMesh.geometry = new THREE.PlaneGeometry(params.width, params.height, segs, segs);
    }
  }
  swapMaterial(planeMesh, next.plane, next, () => {
    const mat = new THREE.MeshLambertMaterial();
    tintEmissive(mat);
    return dress(mat, next.plane, next);
  });
  swapMaterial(groundMesh, next.ground, next, () => {
    const mat = new THREE.MeshLambertMaterial();
    tintEmissive(mat);
    return dress(mat, next.ground, next);
  });
  swapMaterial(horizonMesh, next.horizon, next, () => {
    const mat = new THREE.MeshBasicMaterial({ side: THREE.DoubleSide, transparent: true });
    return dress(mat, next.horizon, next);
  });
  for (const plinth of plinths) {
    if (plinth.external) {
      plinth.material = swapMaterial(plinth.mesh, next.external, next, () => dress(new THREE.MeshStandardMaterial(), next.external, next));
    } else {
      plinth.material = swapMaterial(plinth.mesh, next.package, next, () => {
        const mat = new THREE.MeshLambertMaterial();
        tintEmissive(mat);
        return dress(mat, next.package, next);
      });
    }
  }
  if (solidMesh && next.entity.vertexSource && next.entity.vertexSource.includes("aHouse") && !solidMesh.geometry.getAttribute("aHouse")) {
    solidMesh.geometry.setAttribute("aHouse", houseAttribute(solidMesh.userData.slots));
  }
  if (solidMesh) {
    solidMat = swapMaterial(solidMesh, next.entity, next, () => {
      const mat = new THREE.MeshLambertMaterial({ vertexColors: true });
      tintEmissive(mat);
      return dress(mat, next.entity, next);
    });
  }
}

function captureLook() {
  const mats = [];
  const instances = [];
  const attrs = [];
  scene.traverse((obj) => {
    const list = !obj.material ? [] : (Array.isArray(obj.material) ? obj.material : [obj.material]);
    for (const mat of list) {
      if (!mat || mat.isShaderMaterial) continue;
      const rec = { mat };
      if (mat.color && mat.color.isColor) rec.color = mat.color.clone();
      if (mat.emissive && mat.emissive.isColor) rec.emissive = mat.emissive.clone();
      if (typeof mat.emissiveIntensity === "number") rec.emissiveIntensity = mat.emissiveIntensity;
      if (typeof mat.opacity === "number") rec.opacity = mat.opacity;
      if (typeof mat.metalness === "number") rec.metalness = mat.metalness;
      if (typeof mat.roughness === "number") rec.roughness = mat.roughness;
      mats.push(rec);
    }
    if (obj.instanceColor) instances.push({ attr: obj.instanceColor, colors: Float32Array.from(obj.instanceColor.array) });
    const painted = obj.geometry && obj.geometry.getAttribute && obj.geometry.getAttribute("aColor");
    if (painted) attrs.push({ attr: painted, colors: Float32Array.from(painted.array) });
  });
  const clear = new THREE.Color();
  renderer.getClearColor(clear);
  return {
    mats,
    instances,
    attrs,
    hemi: hemiLight && { color: hemiLight.color.clone(), ground: hemiLight.groundColor.clone(), intensity: hemiLight.intensity },
    key: keyLight && { color: keyLight.color.clone(), intensity: keyLight.intensity },
    rim: rimLight && { color: rimLight.color.clone(), intensity: rimLight.intensity },
    fog: { color: scene.fog.color.clone(), density: scene.fog.density },
    exposure: renderer.toneMappingExposure,
    clear,
    halo: haloMaterial.uniforms.uColor.value.clone(),
  };
}

function mixNumber(from, to, k) {
  return from + (to - from) * k;
}

function mixLook(from, to, k) {
  const count = Math.min(from.mats.length, to.mats.length);
  for (let i = 0; i < count; i++) {
    const start = from.mats[i];
    const end = to.mats[i];
    const mat = end.mat;
    if (start.mat !== mat) continue;
    if (start.color && end.color) mat.color.copy(start.color).lerp(end.color, k);
    if (start.emissive && end.emissive) mat.emissive.copy(start.emissive).lerp(end.emissive, k);
    if (start.emissiveIntensity != null && end.emissiveIntensity != null) mat.emissiveIntensity = mixNumber(start.emissiveIntensity, end.emissiveIntensity, k);
    if (start.opacity != null && end.opacity != null) mat.opacity = mixNumber(start.opacity, end.opacity, k);
    if (start.metalness != null && end.metalness != null) mat.metalness = mixNumber(start.metalness, end.metalness, k);
    if (start.roughness != null && end.roughness != null) mat.roughness = mixNumber(start.roughness, end.roughness, k);
  }
  mixBuffers(from.instances, to.instances, k);
  mixBuffers(from.attrs, to.attrs, k);
  if (from.hemi && to.hemi) {
    hemiLight.color.copy(from.hemi.color).lerp(to.hemi.color, k);
    hemiLight.groundColor.copy(from.hemi.ground).lerp(to.hemi.ground, k);
    hemiLight.intensity = mixNumber(from.hemi.intensity, to.hemi.intensity, k);
  }
  if (from.key && to.key) {
    keyLight.color.copy(from.key.color).lerp(to.key.color, k);
    keyLight.intensity = mixNumber(from.key.intensity, to.key.intensity, k);
  }
  if (from.rim && to.rim) {
    rimLight.color.copy(from.rim.color).lerp(to.rim.color, k);
    rimLight.intensity = mixNumber(from.rim.intensity, to.rim.intensity, k);
  }
  scene.fog.color.copy(from.fog.color).lerp(to.fog.color, k);
  scene.fog.density = mixNumber(from.fog.density, to.fog.density, k);
  renderer.toneMappingExposure = mixNumber(from.exposure, to.exposure, k);
  renderer.setClearColor(from.clear.clone().lerp(to.clear, k));
  haloMaterial.uniforms.uColor.value.copy(from.halo).lerp(to.halo, k);
}

function mixBuffers(from, to, k) {
  const count = Math.min(from.length, to.length);
  for (let i = 0; i < count; i++) {
    if (from[i].attr !== to[i].attr) continue;
    const start = from[i].colors;
    const end = to[i].colors;
    const dest = to[i].attr.array;
    const n = Math.min(start.length, end.length, dest.length);
    for (let c = 0; c < n; c++) dest[c] = mixNumber(start[c], end[c], k);
    to[i].attr.needsUpdate = true;
  }
}

function repaintScene() {
  paintPlinths();
  if (planeMesh) {
    planeMesh.material.color.set(theme.plane.color);
    planeMesh.material.opacity = theme.plane.opacity ?? 1;
    // A surface you can see writes depth; one that is faded out does not.
    planeMesh.material.depthWrite = (theme.plane.opacity ?? 1) >= 1;
  }
  if (groundMesh) {
    groundMesh.material.color.set(theme.ground.color);
    groundMesh.material.opacity = theme.ground.opacity ?? 1;
    groundMesh.material.depthWrite = (theme.ground.opacity ?? 1) >= 1;
  }
  if (horizonMesh) {
    horizonMesh.material.color.set(theme.horizon.color);
    horizonMesh.material.opacity = theme.horizon.opacity;
  }
  if (hemiLight) {
    const light = theme.light;
    hemiLight.color.set(light.hemiSky);
    hemiLight.groundColor.set(light.hemiGround);
    hemiLight.intensity = light.hemiIntensity;
    keyLight.color.set(light.key);
    keyLight.intensity = light.keyIntensity;
    rimLight.color.set(light.rim);
    rimLight.intensity = light.rimIntensity;
  }
  renderer.toneMappingExposure = theme.light.exposure;
  renderer.setClearColor(theme.background.color);
  scene.fog.color.set(theme.fog.color);
  if (laid) scene.fog.density = theme.fog.falloff / citySpan();
  ring.material.color.set(theme.selection.ring);
  ring.material.opacity = theme.selection.ringOpacity;
  updateHalo();
}

function fadeToTheme(next) {
  if (theme && theme.uTime) next.uTime = theme.uTime;
  adoptShaders(next);
  const from = captureLook();
  const fromLabel = theme.label.text;
  const fromBg = theme.label.background;
  const fromSky = skyColors(theme);
  theme = next;
  syncPaint(next);
  repaintScene();
  const to = captureLook();
  const glideSky = fromSky && skyColors(next);
  mixLook(from, to, 0);
  redrawPlates(fromLabel, fromBg);
  if (glideSky) applyGradient(scene, fromSky);
  else applySky(scene, next);
  skinWarning = next.shadeWarning || "";
  applyPage(next, false);
  updateHUD();
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (reduce) {
    mixLook(from, to, 1);
    redrawPlates(next.label.text, next.label.background);
    if (glideSky) applyGradient(scene, glideSky);
    if (next.hud && next.hud.scheme) document.documentElement.style.colorScheme = next.hud.scheme;
    updateHalo();
    viewDirty = true;
    requestFrame();
    return;
  }
  themeFade = {
    from,
    to,
    fromLabel,
    toLabel: next.label.text,
    fromBg,
    toBg: next.label.background,
    fromSky: glideSky ? fromSky : null,
    toSky: glideSky,
    scheme: next.hud && next.hud.scheme,
    schemeSet: false,
    t0: performance.now(),
    ms: THEME_FADE_MS,
  };
  viewDirty = true;
  requestFrame();
}

function tickThemeFade(now) {
  if (!themeFade) return false;
  const fade = themeFade;
  const k = Math.min(1, (now - fade.t0) / fade.ms);
  const s = k * k * (3 - 2 * k);
  mixLook(fade.from, fade.to, s);
  redrawPlates(mixCSS(fade.fromLabel, fade.toLabel, s), mixCSS(fade.fromBg, fade.toBg, s));
  if (fade.fromSky) {
    applyGradient(scene, {
      top: mixCSS(fade.fromSky.top, fade.toSky.top, s),
      horizon: mixCSS(fade.fromSky.horizon, fade.toSky.horizon, s),
      bottom: mixCSS(fade.fromSky.bottom, fade.toSky.bottom, s),
    });
  }
  if (!fade.schemeSet && s >= 0.5 && fade.scheme) {
    fade.schemeSet = true;
    document.documentElement.style.colorScheme = fade.scheme;
  }
  if (k >= 1) {
    if (fade.scheme) document.documentElement.style.colorScheme = fade.scheme;
    themeFade = null;
    paintPlinths();
    redrawPlates(fade.toLabel, fade.toBg);
    updateHalo();
  }
  return true;
}

const scratch = {
  pos: new THREE.Vector3(),
  quat: new THREE.Quaternion(),
  scale: new THREE.Vector3(),
  mat: new THREE.Matrix4(),
  a: new THREE.Vector3(),
  b: new THREE.Vector3(),
  c: new THREE.Vector3(),
  proj: new THREE.Vector3(),
};

const hud = {
  title: document.querySelector("#title"),
  note: document.querySelector("#note"),
  detailHead: document.querySelector("#detail-head"),
  detailBody: document.querySelector("#detail-body"),
  tag: document.querySelector("#tag"),
  overview: document.querySelector("#mode-overview"),
  changes: document.querySelector("#mode-changes"),
  search: document.querySelector("#search"),
  results: document.querySelector("#results"),
  legend: document.querySelector("#legend"),
  side: document.querySelector("#side"),
  collapse: document.querySelector("#side-toggle"),
  help: document.querySelector("#side-help"),
  logo: document.querySelector("#side-logo"),
};

const loadUI = {
  root: document.querySelector("#load"),
  label: document.querySelector("#load-label"),
  percent: document.querySelector("#load-percent"),
  bar: document.querySelector("#load-bar"),
};

let sceneDoc = null;
let laid = null;
let mode = "overview";
let selected = null;
// Vim-style jump list over the nodes the user has selected. `o` steps to the
// older entry (vim's <C-o>), `i` to the newer one (<C-i>). Replaying an entry
// must not record itself, so noteJump is a no-op while jumping. Esc back to
// the bare city detaches the cursor instead of pushing the city, so the next
// older step restores the node just left.
let jumps = [];
let jumpIndex = -1;
let jumping = false;
let jumpDetached = false;
// The scroll offsets of the jump being restored, applied after its render.
let pendingSidebarScroll = null;
let sidebarScrollFrame = 0;
let entered = null;
let focus = null;
let cityPose = null;
let pointerDown = null;
// What took part in the diff, worked out once per scene for the Changes
// view's fading. Null until a scene has been indexed.
let overlayTint = null;

const byEntity = new Map();
const byPackage = new Map();
const byDecl = new Map();
// entity id → Map of caller id → { target, change, step }
const callersOf = new Map();
const catalog = [];
let searchHits = [];
let commandHits = [];
let searchCursor = 0;
const marchStops = new Set();

function stopMarches() {
  for (const stop of marchStops) stop();
  marchStops.clear();
}

function clipText(label) {
  const clip = document.createElement("span");
  clip.className = "clip";
  const track = document.createElement("span");
  track.className = "marquee-track";
  const text = document.createElement("span");
  text.className = "marquee";
  text.textContent = label;
  track.append(text);
  clip.append(track);
  return clip;
}

// A name that does not fit its clip marches right to left while the pointer is
// on the row. A second copy follows a gap behind the first, and the track moves
// left by exactly one copy plus that gap before it wraps — at that point the
// copy sits where the first began, so the loop has no jump and there is always
// a space to read between the end of the name and its next pass.
const MARQUEE_GAP = 24;   // px of daylight between the name and the next copy
const MARQUEE_SPEED = 45; // px per second

function attachMarquee(host, clip) {
  const track = clip.querySelector(".marquee-track");
  if (!track) return;
  let frame = 0;
  const stop = () => {
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
    for (const copy of track.querySelectorAll(".marquee.copy")) copy.remove();
    track.style.transform = "";
    marchStops.delete(stop);
  };
  host.addEventListener("mouseenter", () => {
    stop();
    const first = track.querySelector(".marquee");
    if (!first) return;
    const nameWidth = Math.ceil(first.getBoundingClientRect().width);
    if (nameWidth <= clip.clientWidth) return;
    const copy = first.cloneNode(true);
    copy.classList.add("copy");
    copy.setAttribute("aria-hidden", "true");
    copy.style.marginLeft = MARQUEE_GAP + "px";
    first.after(copy);
    const span = nameWidth + MARQUEE_GAP;
    const started = performance.now();
    const step = (now) => {
      const x = (((now - started) / 1000) * MARQUEE_SPEED) % span;
      track.style.transform = "translateX(" + -x + "px)";
      frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    marchStops.add(stop);
  });
  host.addEventListener("mouseleave", stop);
}

function setSide(open) {
  hud.side.classList.toggle("is-collapsed", !open);
  if (open) sheetPane = "side";
  syncSheet();
  requestAnimationFrame(resizeView);
  syncURL();
}

// A touch screen (a phone or a tablet) gets its own layout; desktop never
// matches. Held upright, the two sidebars are one bottom sheet. Held
// sideways they stay sidebars.
const touchUI = window.matchMedia("(hover: none) and (pointer: coarse)");
const sheetMode = window.matchMedia("(hover: none) and (pointer: coarse) and (orientation: portrait)");

// The bottom sheet works the way the iOS and Android ones do. It rests at
// one of three detents: the peek (its title row), half the screen, or full.
// It moves by a transform, so its content never reflows while it slides.
// A drag anywhere on it moves it, and on release it settles on the detent the
// drag was heading for: a flick carries it on to the next one. Below full, a
// drag on the content moves the sheet instead of scrolling; at full the
// content scrolls, and pulling down from the top of it takes the sheet down.
// The peek is the sidebar's closed state (side=closed in the address).
// With a tour, the tabs under the sheet pick which sidebar it shows.
const sheetTabs = {
  side: document.querySelector("#tab-side"),
  tour: document.querySelector("#tab-tour"),
};
const tourSideEl = document.querySelector("#tour-side");
const sheetPanes = [hud.side, tourSideEl];
let sheetPane = "side";
let sheetLevel = "half";
let tourWasOpen = false;
let sheetDrag = null;
const SHEET_SETTLE = "transform 0.42s cubic-bezier(0.32, 0.72, 0, 1)";

function tourOpen() {
  return !tourSideEl.hidden && !tourSideEl.classList.contains("is-collapsed");
}

function paneOpen() {
  return sheetPane === "tour" ? tourOpen() : !hud.side.classList.contains("is-collapsed");
}

function setPaneOpen(open) {
  if (sheetPane === "tour") {
    if (tourUI) tourUI.setOpen(open);
  } else setSide(open);
}

// The three resting heights, in pixels of sheet on screen.
function sheetDetents() {
  const pane = sheetPane === "tour" ? tourSideEl : hud.side;
  const full = pane.offsetHeight || window.innerHeight * 0.8;
  const peek = parseFloat(getComputedStyle(document.body).getPropertyValue("--peek")) || 76;
  const tabs = document.querySelector("#sheet-tabs").offsetHeight || 0;
  const half = Math.max(peek + 80, Math.min(full, Math.round((window.innerHeight - tabs) * 0.5)));
  return { peek, half, full };
}

function sheetShown() {
  const d = sheetDetents();
  if (!paneOpen()) return d.peek;
  return sheetLevel === "full" ? d.full : d.half;
}

function placeSheet(shown, animate) {
  const full = sheetDetents().full;
  for (const pane of sheetPanes) {
    pane.style.transition = animate ? SHEET_SETTLE : "none";
    pane.style.transform = "translateY(" + Math.round(full - shown) + "px)";
  }
}

function settleSheet(animate = true) {
  if (!sheetMode.matches) {
    for (const pane of sheetPanes) {
      pane.style.transition = "";
      pane.style.transform = "";
    }
    document.body.classList.remove("sheet-full");
    return;
  }
  const full = paneOpen() && sheetLevel === "full";
  document.body.classList.toggle("sheet-full", full);
  if (!full) for (const pane of sheetPanes) if (!paneOpen()) pane.scrollTop = 0;
  placeSheet(sheetShown(), animate && !window.matchMedia("(prefers-reduced-motion: reduce)").matches);
}

function syncSheet() {
  const hasTour = !tourSideEl.hidden;
  const open = tourOpen();
  if (open && !tourWasOpen) sheetPane = "tour";
  tourWasOpen = open;
  if (!hasTour && sheetPane === "tour") sheetPane = "side";
  document.body.classList.toggle("sheet-tour", sheetPane === "tour");
  document.body.classList.toggle("has-tour", hasTour);
  sheetTabs.tour.hidden = !hasTour;
  const sideShown = sheetPane === "side" && !hud.side.classList.contains("is-collapsed");
  const tourShown = sheetPane === "tour" && open;
  sheetTabs.side.classList.toggle("on", sideShown);
  sheetTabs.tour.classList.toggle("on", tourShown);
  sheetTabs.side.setAttribute("aria-pressed", String(sideShown));
  sheetTabs.tour.setAttribute("aria-pressed", String(tourShown));
  if (!sheetDrag) settleSheet();
}

// Settle on a detent: the peek closes the sidebar, the others open it.
function goToDetent(level) {
  if (level === "peek") {
    if (paneOpen()) setPaneOpen(false);
    else settleSheet();
    return;
  }
  sheetLevel = level;
  if (!paneOpen()) setPaneOpen(true);
  else {
    settleSheet();
    requestAnimationFrame(resizeView);
  }
}

function showTourPane() {
  if (!tourUI) return;
  sheetPane = "tour";
  tourWasOpen = true;
  tourUI.setOpen(true);
}

sheetTabs.side.addEventListener("click", () => {
  setSide(sheetPane !== "side" || hud.side.classList.contains("is-collapsed"));
});
sheetTabs.tour.addEventListener("click", () => {
  if (sheetPane === "tour" && tourOpen()) tourUI.setOpen(false);
  else showTourPane();
});

// The grabber steps through the detents, the way a tap on it does on iOS.
for (const grip of document.querySelectorAll(".sheet-grip")) {
  grip.addEventListener("click", () => {
    if (!sheetMode.matches) return;
    if (!paneOpen()) goToDetent("half");
    else goToDetent(sheetLevel === "half" ? "full" : "half");
  });
}
// Peeking, the title row is the handle: a tap on it (not on its buttons)
// brings the sheet up.
for (const row of [document.querySelector("#title-row"), document.querySelector("#tour-title-row")]) {
  row.addEventListener("click", (event) => {
    if (!sheetMode.matches || paneOpen() || event.target.closest("button")) return;
    goToDetent("half");
  });
}
// A box that takes focus wants the keyboard and room above it: full.
for (const pane of sheetPanes) {
  pane.addEventListener("focusin", (event) => {
    if (sheetMode.matches && event.target.matches("input") && (sheetLevel !== "full" || !paneOpen())) goToDetent("full");
  });
}

// Past either end the sheet gives less and less, like a rubber band.
function rubber(over, size) {
  const k = 0.55;
  return (1 - 1 / ((over * k) / size + 1)) * size;
}

function onSheetTouchStart(event) {
  if (!sheetMode.matches || event.touches.length !== 1) {
    sheetDrag = null;
    return;
  }
  const touch = event.touches[0];
  const pane = event.currentTarget;
  sheetDrag = {
    pane,
    x: touch.clientX,
    y: touch.clientY,
    mode: "",
    // The grabber and the header always move the sheet; so does any drag
    // while the sheet is short of full.
    head: !!event.target.closest(".sheet-grip, #side-head, #tour-head"),
    from: sheetShown(),
    shown: sheetShown(),
    samples: [[touch.clientY, event.timeStamp]],
  };
}

function onSheetTouchMove(event) {
  const drag = sheetDrag;
  if (!drag || event.touches.length !== 1) return;
  const touch = event.touches[0];
  const dy = touch.clientY - drag.y;
  const dx = touch.clientX - drag.x;
  if (!drag.mode) {
    if (!dx && !dy) return;
    // The first move decides, before the browser commits to a scroll.
    const atFull = paneOpen() && sheetLevel === "full";
    if (Math.abs(dx) > Math.abs(dy) * 1.2) drag.mode = "none";
    else if (drag.head || !atFull) drag.mode = "sheet";
    else if (dy > 0 && drag.pane.scrollTop <= 0) drag.mode = "sheet";
    else drag.mode = "scroll";
    if (drag.mode === "sheet") {
      drag.y = touch.clientY;
      closeSearch();
      hud.search.blur();
    }
  }
  if (drag.mode !== "sheet") return;
  event.preventDefault();
  const { peek, full } = sheetDetents();
  let shown = drag.from - (touch.clientY - drag.y);
  if (shown > full) shown = full + rubber(shown - full, 120);
  if (shown < peek) shown = peek - rubber(peek - shown, 120);
  drag.shown = shown;
  drag.samples.push([touch.clientY, event.timeStamp]);
  if (drag.samples.length > 6) drag.samples.shift();
  placeSheet(shown, false);
}

function onSheetTouchEnd() {
  const drag = sheetDrag;
  sheetDrag = null;
  if (!drag || drag.mode !== "sheet") return;
  // Velocity over the last ~100 ms, in px/ms, positive upward.
  const last = drag.samples[drag.samples.length - 1];
  let first = drag.samples[0];
  for (const sample of drag.samples) if (last[1] - sample[1] <= 100) { first = sample; break; }
  const dt = Math.max(1, last[1] - first[1]);
  const velocity = (first[0] - last[0]) / dt;
  const d = sheetDetents();
  const order = [["peek", d.peek], ["half", d.half], ["full", d.full]];
  let level;
  if (Math.abs(velocity) > 0.4) {
    // A flick goes to the next detent in its direction from where it is now.
    const ahead = order.filter(([, at]) => (velocity > 0 ? at > drag.shown + 1 : at < drag.shown - 1));
    level = ahead.length ? (velocity > 0 ? ahead[0][0] : ahead[ahead.length - 1][0]) : (velocity > 0 ? "full" : "peek");
  } else {
    // Otherwise the nearest detent to where it would coast to.
    const projected = drag.shown + velocity * 160;
    level = order.reduce((best, item) => (Math.abs(item[1] - projected) < Math.abs(best[1] - projected) ? item : best))[0];
  }
  goToDetent(level);
}

for (const pane of sheetPanes) {
  pane.addEventListener("touchstart", onSheetTouchStart, { passive: true });
  pane.addEventListener("touchmove", onSheetTouchMove, { passive: false });
  pane.addEventListener("touchend", onSheetTouchEnd);
  pane.addEventListener("touchcancel", onSheetTouchEnd);
}

document.querySelector("#side-theme").addEventListener("click", () => openSkinPanel());
// The list under the search box is placed on the page, not in the sidebar,
// so it follows the sidebar when that scrolls.
hud.side.addEventListener("scroll", placeResults, { passive: true });
// iOS zooms the page on a pinch whatever the viewport says. The city has its
// own pinch, and the page itself never zooms.
if (touchUI.matches) document.addEventListener("gesturestart", (event) => event.preventDefault());
for (const query of [touchUI, sheetMode]) {
  query.addEventListener("change", () => {
    closeSearch();
    syncSheet();
    settleSheet(false);
    requestAnimationFrame(resizeView);
  });
}
window.addEventListener("resize", () => settleSheet(false));
syncSheet();
let arcSubject = null;
let entitySubject = null;
// Show calls draws what the node calls, particles leaving it. Show callers
// draws what calls it. The arcs are the same curve with the ends swapped, so
// the particles run back toward the node. One direction is on at a time, and
// it applies to every node kind: a function or method (its calls, its
// callers), a type (the methods it declares, the functions that call them), a
// package (what it depends on, what depends on it). The sidebar picks it, `c`
// flips it, and the choice stays until it is flipped again or the view resets.
let callInbound = false;
let lit = null;
// A tour step's highlight: what it lights, and the call arcs it draws.
let tourLit = null;
let tourLinks = null;
let tourUI = null;
let linkPoints = null;
// The other points a fit of the current links must keep in view: tower
// bases and arc crowns.
let linkFrame = [];
const held = new Set();
const flyDir = new THREE.Vector3();
let lastFrame = 0;
let lastActivity = 0;
const IDLE_SPIN_MS = 15000;
// Q and E orbit at 1.4 rad/s. The idle orbit is a fifth of that.
const IDLE_YAW = 0.28;
// The search box swallows the fly keys while it has focus. The idle orbit is
// not a fly key: it keeps turning in every focus, this box included.
const NO_KEYS = new Set();
let idleSpin = false;
// The city turns as soon as it appears. A click, a move key, or a zoom ends that turn.
// b and ? do not. After the view has been still, the slow orbit returns in every
// view: overview, a selected package, and a call focus.
let introSpin = true;
let viewDirty = true;
// The loader is up until main() paints the finished city. While it is, the
// animate loop must not draw: the loader covers the canvas, and rendering a
// half-built city every frame the loader yields is pure waste — on a large
// codebase it can cost more than the build itself.
let loading = true;
let pointerDirty = false;
let frameQueued = false;
let idleTimer = 0;
const settledPos = new THREE.Vector3();
const settledTarget = new THREE.Vector3();

function noteActivity() {
  lastActivity = performance.now();
}

function endIntro() {
  introSpin = false;
}

function requestFrame(delay = 0) {
  if (frameQueued) return;
  frameQueued = true;
  clearTimeout(idleTimer);
  if (delay > 0) idleTimer = setTimeout(() => requestAnimationFrame(animate), delay);
  else requestAnimationFrame(animate);
}

function parkLoop() {
  clearTimeout(idleTimer);
  const remain = lastActivity ? IDLE_SPIN_MS - (performance.now() - lastActivity) : IDLE_SPIN_MS;
  idleTimer = setTimeout(requestFrame, Math.max(40, remain));
}
const plinths = [];
const entitySlots = [];
const slotById = new Map();

const viewEl = document.querySelector("#view");
const renderer = new THREE.WebGLRenderer({ antialias: true });
renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.08;
renderer.outputColorSpace = THREE.SRGBColorSpace;
renderer.setClearColor(0x09090b);
viewEl.appendChild(renderer.domElement);

const scene = new THREE.Scene();
scene.background = new THREE.Color(0x09090b);
scene.fog = new THREE.FogExp2(0x09090b, 0.004);

const camera = new THREE.PerspectiveCamera(42, 1, 0.1, 2000);

// The particle material is built further down, next to the path texture. The
// resize hook is assigned there; until then resizing has nothing to rescale.
let updatePointScale = () => {};

let viewInsets = { left: 0, right: 0, bottom: 0, free: 1, width: 1, height: 1, fov: 42, minAspect: 0 };
// Upright, a fit draws the city as if the screen were this wide: larger, and
// running past the sides rather than shrunk to a phone's width.
const UPRIGHT_ASPECT = 1.25;
const UPRIGHT_SKIN_ASPECT = 0.7;
// The screen size the theme menu last framed the city for. Turning the phone
// fires several resizes while the browser settles (the toolbars move too), so
// the framing waits for the last one: each orientation then always gets the
// same framing, flown to from wherever the camera is.
let skinFitSize = "";
let skinFitTimer = 0;
// The theme menu frames the city in the middle 70% and takes the keys.
let skinPicking = false;
// Applying a preview walks the lines outward. The camera and the sidebars stay
// put until it finishes, so the still of the new theme lines up with the old one.
let skinRevealing = false;

function coverOf(selector) {
  const el = document.querySelector(selector);
  if (!el || el.hidden || el.classList.contains("is-collapsed")) return 0;
  return el.getBoundingClientRect().width;
}

// How much of the view the bottom sheet and its tabs cover, held upright,
// at the detent it is resting on. Past half the city is behind the sheet
// anyway, so it stays where half put it.
function coverBelow() {
  if (!sheetMode.matches || skinPicking) return 0;
  const tabs = document.querySelector("#sheet-tabs").offsetHeight || 0;
  return tabs + Math.min(sheetShown(), sheetDetents().half);
}

// The city's lift above the sheet eases to its new height when only the
// sheet moved, so the city glides with it instead of jumping.
let liftNow = 0;
let liftAnim = 0;
let lastViewSize = "";

function applyViewOffset(w, h, offset, lift) {
  if (offset || lift) camera.setViewOffset(w, h, offset, lift, w, h);
  else camera.clearViewOffset();
  camera.updateProjectionMatrix();
}

function easeLift(w, h, offset, lift) {
  cancelAnimationFrame(liftAnim);
  const from = liftNow;
  const t0 = performance.now();
  const step = (now) => {
    const k = Math.min(1, (now - t0) / 420);
    const eased = 1 - Math.pow(1 - k, 3);
    liftNow = from + (lift - from) * eased;
    applyViewOffset(w, h, offset, liftNow);
    viewDirty = true;
    requestFrame();
    if (k < 1) liftAnim = requestAnimationFrame(step);
  };
  liftAnim = requestAnimationFrame(step);
}

// The camera fit for a box, in the free area between the sidebars.
function fitTo(points, dir = FIT_DIR) {
  const pose = fitPose(boxOf(points), dir, {
    fov: viewInsets.fov,
    width: viewInsets.width,
    height: viewInsets.height,
    left: viewInsets.left,
    right: viewInsets.right,
    minAspect: viewInsets.minAspect,
  });
  return { pos: new THREE.Vector3(...pose.pos), target: new THREE.Vector3(...pose.target) };
}
// In front of the city (+z), a little to the right (+x), above the arcs.
const FIT_DIR = [0.32, 0.48, 0.78];

function resizeView() {
  // A resize mid-circle would move the live city off the still.
  if (skinRevealing) return;
  const w = viewEl.clientWidth;
  const h = viewEl.clientHeight;
  if (w < 2 || h < 2) return;
  camera.aspect = w / h;
  // The city is drawn between the two sidebars: the look-at point sits in
  // the middle of the free area, and fits use only its width.
  // The theme menu uses the middle 70%. Otherwise the tour takes the right edge.
  const band = skinPicking ? w * 0.15 : 0;
  // Upright, the sheet covers the bottom instead, and the city sits above it.
  const side = !skinPicking && !sheetMode.matches;
  const right = skinPicking ? band : side ? coverOf("#tour-side") : 0;
  const left = skinPicking ? band : side ? coverOf("#side") : 0;
  const bottom = Math.min(coverBelow(), Math.max(0, h - 160));
  viewInsets = insets(w, left, right);
  viewInsets.width = w;
  viewInsets.height = h - bottom;
  viewInsets.bottom = bottom;
  viewInsets.fov = freeFov(camera.fov, h, bottom);
  // The theme menu frames the city in its middle band: a smaller boost there.
  viewInsets.minAspect = !sheetMode.matches ? 0 : skinPicking ? UPRIGHT_SKIN_ASPECT : UPRIGHT_ASPECT;
  // The loading bar centres in the same free area the camera frames: the full
  // stage spans the window, and the sidebars (or the bottom sheet) cover its edges.
  loadUI.root.style.paddingLeft = viewInsets.left + "px";
  loadUI.root.style.paddingRight = viewInsets.right + "px";
  loadUI.root.style.paddingBottom = bottom + "px";
  const offset = viewOffsetX(viewInsets.left, viewInsets.right);
  const lift = viewOffsetY(bottom);
  const size = w + "x" + h;
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (size === lastViewSize && Math.abs(lift - liftNow) > 0.5 && !reduce) {
    easeLift(w, h, offset, lift);
  } else {
    cancelAnimationFrame(liftAnim);
    liftNow = lift;
    applyViewOffset(w, h, offset, lift);
  }
  lastViewSize = size;
  renderer.setSize(w, h, false);
  updatePointScale();
  placeResults();
  layoutSkinFrame();
  // Resized while the theme menu is open: frame the whole city again.
  if (skinPicking && laid && skinFitSize && skinFitSize !== size) {
    clearTimeout(skinFitTimer);
    skinFitTimer = setTimeout(() => {
      if (skinPicking && !skinRevealing) frameSkinCity();
    }, 200);
  }
  viewDirty = true;
  requestFrame();
}

resizeView();
new ResizeObserver(resizeView).observe(viewEl);
const controls = new OrbitControls(camera, renderer.domElement);
controls.enableDamping = true;
controls.dampingFactor = 0.08;
controls.maxPolarAngle = Math.PI / 2 - 0.05;
controls.minDistance = 2;
// The loop parks when nothing on screen moves. A drag, a pinch, or the glide
// after one moves the camera through the controls, so every change asks for
// the next frame; without this a touch drag on the bare city (no halo, no
// particles keeping the loop awake) drew only now and then.
controls.addEventListener("change", () => requestFrame());

const raycaster = new THREE.Raycaster();
const pointer = new THREE.Vector2(-2, -2);

const city = new THREE.Group();
scene.add(city);
const arcGroup = new THREE.Group();
scene.add(arcGroup);

// The selected node wears a halo on its sides: a bright rim where the wall
// turns away from the eye, plus a band that travels up the box. Cheap, one
// draw call, and it runs on the same clock as the particles — it advances
// while the scene is driven and freezes when the loop parks.
const haloMaterial = new THREE.ShaderMaterial({
  uniforms: {
    uTime: { value: 0 },
    uColor: { value: new THREE.Color(0x23afd0) },
    uHeight: { value: 1 },
    uStrength: { value: 1 },
  },
  vertexShader: `
    varying vec3 vNormal;
    varying vec3 vView;
    varying float vY;
    uniform float uHeight;
    void main() {
      vNormal = normalize(normalMatrix * normal);
      vec4 mv = modelViewMatrix * vec4(position, 1.0);
      vView = normalize(-mv.xyz);
      vY = clamp(position.y / max(0.001, uHeight) + 0.5, 0.0, 1.0);
      gl_Position = projectionMatrix * mv;
    }
  `,
  fragmentShader: `
    varying vec3 vNormal;
    varying vec3 vView;
    varying float vY;
    uniform float uTime;
    uniform vec3 uColor;
    uniform float uStrength;
    void main() {
      float rim = pow(1.0 - abs(dot(normalize(vNormal), normalize(vView))), 2.4);
      float wave = abs(fract(vY - uTime * 0.22) - 0.5) * 2.0;
      float band = pow(max(0.0, 1.0 - wave * 3.2), 2.0);
      float a = (rim * 0.5 + band * 0.42) * uStrength;
      if (a < 0.004) discard;
      gl_FragColor = vec4(uColor, a);
      #include <colorspace_fragment>
    }
  `,
  transparent: true,
  depthWrite: false,
  blending: THREE.AdditiveBlending,
  fog: false,
});
const halo = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), haloMaterial);
halo.visible = false;
halo.frustumCulled = false;
scene.add(halo);

// The halo travels on the node the user is on: the focused entity while the
// call diff is open, otherwise the selection. It stays on that node whichever
// way the call arcs run, so watching its callers does not move the animation
// onto a caller.
function updateHalo() {
  const subject = focus ? { kind: "entity", id: focus.entity.id } : selected;
  if (!subject) {
    halo.visible = false;
    return;
  }
  let box = null;
  if (subject.kind === "entity") {
    const slot = slotById.get(subject.id);
    if (slot) box = visualBox(slot, slotOpen(slot));
  } else if (subject.kind === "package") {
    box = laid.packages.find((item) => item.id === subject.id) || null;
  }
  if (!box) {
    halo.visible = false;
    return;
  }
  const pad = 0.35;
  halo.geometry.dispose();
  halo.geometry = new THREE.BoxGeometry(box.w + pad, box.h + pad, box.d + pad);
  halo.position.set(box.x + box.w / 2, box.y + box.h / 2, box.z + box.d / 2);
  haloMaterial.uniforms.uHeight.value = box.h;
  const change = (byPackage.get(subject.kind === "package" ? subject.id : "") || {}).change;
  haloMaterial.uniforms.uColor.value.copy(change && change !== "same" ? changeColor(change) : paint.selection);
  halo.visible = true;
}
const selectArcs = new THREE.Group();
scene.add(selectArcs);
const focusGroup = new THREE.Group();
scene.add(focusGroup);

let solidMesh = null;
let solidMat = null;
let planeMesh = null;
let groundMesh = null;
let horizonMesh = null;
let hemiLight = null;
let keyLight = null;
let rimLight = null;
let tween = null;
const ring = new THREE.LineLoop(
  circlePositions(72),
  new THREE.LineBasicMaterial({ color: 0xa1a1aa, transparent: true, opacity: 0.35 }),
);
ring.visible = false;
scene.add(ring);

function boot() {
  const light = theme.light;
  const hemi = new THREE.HemisphereLight(light.hemiSky, light.hemiGround, light.hemiIntensity);
  scene.add(hemi);
  const key = new THREE.DirectionalLight(light.key, light.keyIntensity);
  key.position.set(70, 150, 90);
  scene.add(key);
  const rim = new THREE.DirectionalLight(light.rim, light.rimIntensity);
  rim.position.set(-90, 50, -20);
  scene.add(rim);
  hemiLight = hemi;
  keyLight = key;
  rimLight = rim;
}

function tintEmissive(material) {
  material.onBeforeCompile = (shader) => {
    shader.fragmentShader = shader.fragmentShader.replace(
      "#include <color_fragment>",
      "#include <color_fragment>\n\ttotalEmissiveRadiance *= diffuseColor.rgb;",
    );
  };
}

// The loading bar owns fixed bands of the pipeline, so a stage reports its own
// fraction and the bar still adds up to one. theme 0–5%, scene 5–30%,
// layout 30–40%, index 40–48%, city 48–85%, arcs 85–100%.
const LOAD_BANDS = {
  theme: [0, 0.05],
  scene: [0.05, 0.30],
  layout: [0.30, 0.40],
  index: [0.40, 0.48],
  city: [0.48, 0.85],
  arcs: [0.85, 1],
};

function loadAt(band, fraction = 1) {
  const [from, to] = LOAD_BANDS[band];
  const f = Math.max(0, Math.min(1, Number(fraction) || 0));
  return from + (to - from) * f;
}

// showLoad paints the bar at an absolute fraction of the whole load. A null
// label keeps the stage's name, which is what the inner loops pass.
function showLoad(label, fraction) {
  loadUI.root.hidden = false;
  const pct = Math.max(0, Math.min(1, Number(fraction) || 0));
  if (label != null) loadUI.label.textContent = label;
  loadUI.bar.style.transform = "scaleX(" + pct + ")";
  loadUI.percent.textContent = Math.round(pct * 100) + "%";
  loadUI.root.setAttribute("aria-valuenow", String(Math.round(pct * 100)));
}

function hideLoad() {
  loadUI.root.hidden = true;
}

// One painted frame. requestAnimationFrame does not fire in a hidden tab, so a
// timeout is the floor: a build nobody is watching must not stall.
function nextPaint() {
  return new Promise((resolve) => {
    let done = false;
    const go = () => {
      if (done) return;
      done = true;
      resolve();
    };
    requestAnimationFrame(go);
    setTimeout(go, 50);
  });
}

// Slices a long synchronous loop across frames when someone is watching the
// progress: the work still runs on the main thread, but it hands the browser a
// frame often enough to paint the bar. The budget is one painted frame's worth;
// a shorter one buys smoother motion at the cost of more frames spent on the
// bar itself. Without a progress callback the loop runs in one go, so
// interactive paths stay synchronous. n === 0 is complete.
const SLICE_MS = 100;
async function sliced(n, progress, fn) {
  if (!progress) {
    for (let i = 0; i < n; i++) fn(i);
    return;
  }
  let start = performance.now();
  for (let i = 0; i < n; i++) {
    fn(i);
    if (performance.now() - start < SLICE_MS) continue;
    progress((i + 1) / n);
    start = performance.now();
    await nextPaint();
  }
  progress(1);
}

// Reads scene.json with the download's own progress. The server sends a
// Content-Length; without one the stage is passed at the end, because a guess
// would be worse than a bar that holds still.
async function readScene(response) {
  const total = Number(response.headers.get("Content-Length") || 0);
  if (!response.body || !total) return response.json();
  const reader = response.body.getReader();
  const chunks = [];
  let got = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    got += value.length;
    showLoad(null, loadAt("scene", got / total));
  }
  const bytes = new Uint8Array(got);
  let at = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, at);
    at += chunk.length;
  }
  return JSON.parse(new TextDecoder().decode(bytes));
}

async function main() {
  showLoad("Reading the theme…", loadAt("theme", 0));
  await nextPaint();
  const loaded = await readTheme();
  if (!loaded) {
    hideLoad();
    loading = false;
    hud.note.textContent = skinWarning;
    return;
  }
  useTheme(loaded);
  boot();
  showLoad("Reading the scene…", loadAt("theme"));
  await nextPaint();
  let response;
  try {
    response = await fetch("./scene.json");
    if (!response.ok) throw new Error(response.statusText);
    sceneDoc = await readScene(response);
  } catch (err) {
    hideLoad();
    loading = false;
    hud.note.textContent = "Could not read the scene. " + err.message;
    requestFrame();
    return;
  }
  showLoad("Laying out the city…", loadAt("scene"));
  await nextPaint();
  laid = layoutCity(sceneDoc.packages || []);
  showLoad("Indexing declarations…", loadAt("layout"));
  await nextPaint();
  indexScene();
  showLoad("Building the city…", loadAt("index"));
  await nextPaint();
  await buildCity((f) => showLoad(null, loadAt("city", f)));
  showLoad("Drawing the call arcs…", loadAt("city"));
  await nextPaint();
  await buildArcs((f) => showLoad(null, loadAt("arcs", f)));
  cityPose = frameCity();
  camera.position.copy(cityPose.pos);
  controls.target.copy(cityPose.target);
  applyFitLimits(cityPose);
  controls.update();
  applyMode();
  const named = applyQuery();
  // Fit and paint the finished city behind the bar before it goes, so the first
  // thing on screen is the city, not an empty canvas for a frame.
  resizeView();
  renderer.render(scene, camera);
  hideLoad();
  loading = false;
  requestFrame();
  tourUI = mountTour({
    apply: applyTourStep,
    clear: clearTour,
    layout: () => {
      syncSheet();
      requestAnimationFrame(resizeView);
      if (!document.querySelector("#tour-side").hidden) tourSideWanted = true;
      syncURL();
    },
    open: tourSideWanted,
    hold: named,
  });
  const wanted = new URLSearchParams(location.search).get("tour");
  tourUI.loadURL(wanted || "./tour.json");
}

function clearTourMarks() {
  tourLit = null;
  tourLinks = null;
}

function clearTour() {
  clearTourMarks();
  resetView();
}

// A step starts from a clean city: no focus, no selection, no highlight.
// Then, in order: the mode, the select or focus, the highlight and path,
// and the camera.
function applyTourStep(step) {
  const t = step.targets || {};
  clearTourMarks();
  // A tour step draws the calls it names. Callers mode would reverse them.
  callInbound = false;
  if (focus) dropFocus();
  selected = null;
  entered = null;
  entitySubject = null;
  arcSubject = null;
  ring.visible = false;
  tween = null;
  if (step.mode) mode = step.mode === "changes" ? "overlay" : "overview";

  const marks = [...(t.highlight || []), ...(t.path || [])];
  const points = [];
  if (marks.length) {
    const entities = new Set();
    const packages = new Set();
    for (const node of marks) {
      const found = byEntity.get(node.id);
      if (found) {
        entities.add(node.id);
        if (found.pkg) packages.add(found.pkg.id);
        const at = entityAnchor(found);
        if (at) points.push(at, ...entityExtent(found));
      } else if (byPackage.has(node.id)) {
        packages.add(node.id);
        points.push(...packagePoints(node.id));
      }
    }
    tourLinks = tourCallLinks(t.path || [], t.highlight || []);
    if (step.dim !== false) tourLit = { packages, entities: entities.size ? entities : null, browse: null, links: tourLinks };
  }

  const moved = !!(t.select || t.focus);
  if (t.select) {
    if (t.select.kind === "external") selectExternal(t.select.id);
    else selectPackage(t.select.id, true);
  } else if (t.focus) {
    const found = byEntity.get(t.focus.id);
    if (!found) {
      if (t.focus.kind === "external") selectExternal(t.focus.id);
      else selectPackage(t.focus.id, true);
    } else if (found.entity.kind === "function" || found.entity.kind === "method") enterFocus(found);
    else selectEntity(t.focus.id);
  } else {
    applyMode();
  }

  const preset = step.camera || (moved ? "" : (points.length ? "fit" : "overview"));
  const subject = t.select || t.focus;
  if (subject && !marks.length) {
    const found = byEntity.get(subject.id);
    if (found) {
      const at = entityAnchor(found);
      if (at) points.push(at);
    } else points.push(...packagePoints(subject.id));
  }
  if (preset === "overview") {
    cityPose = frameCity();
    flyTo(cityPose.pos, cityPose.target);
  } else if (preset === "top") {
    const b = laid.bounds;
    const pose = points.length ? tourPose(points) : null;
    const look = pose ? pose.target : new THREE.Vector3((b.minX + b.maxX) / 2, 0, (b.minZ + b.maxZ) / 2);
    // High enough to clear the arcs a selection draws above the roofs.
    const span = pose ? Math.max(pose.pos.distanceTo(look) * 1.6, 70) : citySpan() * 1.25;
    flyTo(new THREE.Vector3(look.x, look.y + span, look.z + span * 0.02), look);
  } else if (preset === "fit" || preset === "close") {
    const pose = tourPose(points.length ? points : tweenPoints());
    flyTo(pose.pos, pose.target);
  }
  const zoom = (preset === "close" ? 0.55 : 1) * (step.zoom > 0 ? step.zoom : 1);
  if (tween && zoom !== 1) {
    tween.toPos.sub(tween.toTarget).multiplyScalar(zoom).add(tween.toTarget);
  }
  noteActivity();
  endIntro();
  viewDirty = true;
  requestFrame();
}

// A package's roof corners, so framing it shows all of it.
function packagePoints(id) {
  const box = laid.packages.find((item) => item.id === id);
  if (!box) {
    const at = packageAnchor(id);
    return at ? [at] : [];
  }
  const y = box.y + box.h;
  return [[box.x, y, box.z], [box.x + box.w, y, box.z + box.d], [box.x + box.w, y, box.z], [box.x, y, box.z + box.d]];
}

// Frame a step's nodes with room for the arcs that bow above them. A
// single node gets a neighbourhood around it, not a close-up of one roof.
function tourPose(points) {
  let top = -Infinity;
  for (const p of points) top = Math.max(top, p[1]);
  const c = points[0];
  return framePose(points.concat([[c[0], top + 10, c[2]]]));
}

// The camera the select or focus just set up, as points to frame.
function tweenPoints() {
  if (tween) return [[tween.toTarget.x, tween.toTarget.y, tween.toTarget.z]];
  return [[controls.target.x, controls.target.y, controls.target.z]];
}

// The arcs a step draws: each hop of the path, and every call between two
// highlighted declarations.
function tourCallLinks(path, highlight) {
  const links = [];
  const seen = new Set();
  const push = (fromId, toId) => {
    const from = byEntity.get(fromId);
    const target = byEntity.get(toId);
    if (!from || !target || seen.has(fromId + "\0" + toId)) return;
    const link = entityLinks(from).find((item) => item.target.entity.id === toId);
    if (!link) return;
    seen.add(fromId + "\0" + toId);
    links.push({ from, target, change: link.change, step: link.step });
  };
  for (let i = 0; i + 1 < path.length; i++) push(path[i].id, path[i + 1].id);
  for (const a of highlight) {
    for (const b of highlight) if (a.id !== b.id) push(a.id, b.id);
  }
  return links.length ? links : null;
}

// The view the address bar names (see state.js), put back once the city is
// built. Until then the address is left alone, so the first applyMode does
// not write the defaults over it. enter= and fn= are the older debug forms.
// Returns whether the address named a view, so a tour does not replace it.
function applyQuery() {
  const state = readState(location.search);
  const params = new URLSearchParams(location.search);
  if (state.mode === "changes") mode = "overlay";
  callInbound = state.refs === "callers";
  if (!state.side) {
    setSide(false);
    // The fly-to below frames the free area, so the insets must be current.
    resizeView();
  }
  tourSideWanted = state.tourSide;
  urlReady = true;
  const node = readNode(state.select, nodeNames);
  if (node && node.kind === "package" && byPackage.has(node.id)) selectPackage(node.id, true);
  else if (node && node.kind === "external" && laid.externals.some((item) => item.id === node.id)) selectExternal(node.id, callInbound);
  else if (node && node.kind === "entity") selectEntity(node.id);
  const enter = params.get("enter");
  if (!selected && enter && byPackage.has(enter)) selectPackage(enter, true);
  const want = params.get("fn");
  if (!selected && want) {
    const picks = [...byEntity.values()].filter((item) => item.box && (item.entity.kind === "function" || item.entity.kind === "method"));
    const named = want === "1" ? null : picks.find((item) => item.entity.id === want);
    const changed = picks.find((item) => (item.entity.calls || []).some((step) => step.change !== "same"));
    const busy = picks.find((item) => (item.entity.calls || []).length > 3 && (item.entity.calls || []).length < 40);
    const pick = named || (want === "1" ? changed || busy || picks[0] : null);
    if (pick) enterFocus(pick);
  }
  if (!selected) applyMode();
  return !!(state.select || state.mode === "changes" || state.refs === "callers");
}

// The address bar follows the view: written in place, so a reload keeps it
// and the browser history is not one entry per click.
let urlReady = false;
let nodeNames = new Map();
// tourside= waits for the tour: until one is on screen the address keeps
// what it said, and once one is, the sidebar itself is the answer.
let tourSideWanted = true;

function syncURL() {
  if (!urlReady) return;
  const found = selected && selected.kind === "entity" ? byEntity.get(selected.id) : null;
  const node = found ? { kind: "entity", entity: found.entity, pkg: found.pkg.id } : selected && selected.kind !== "entity" ? selected : null;
  const tourSide = document.querySelector("#tour-side");
  const query = writeState(location.search, {
    select: writeNode(node, nodeNames),
    mode: mode === "overlay" ? "changes" : "overview",
    refs: callInbound ? "callers" : "calls",
    side: !hud.side.classList.contains("is-collapsed"),
    tourSide: !tourSide || tourSide.hidden ? tourSideWanted : !tourSide.classList.contains("is-collapsed"),
  });
  if (query === location.search) return;
  // Safari throws once a page rewrites its address too often. A missed write
  // must not take applyMode down with it; the next change writes it again.
  try {
    history.replaceState(history.state, "", location.pathname + query + location.hash);
  } catch { /* the address keeps the previous view */ }
}

function indexScene() {
  overlayTint = null;
  catalog.length = 0;
  for (const pkg of sceneDoc.packages || []) {
    byPackage.set(pkg.id, pkg);
    catalog.push({
      kind: pkg.external ? "external" : "package",
      id: pkg.id,
      name: pkg.name || pkg.id,
      extra: pkg.id,
      where: pkg.external ? "outside" : (pkg.parent || ""),
    });
  }
  for (const box of laid.packages) {
    const pkg = byPackage.get(box.id);
    if (!pkg) continue;
    for (const entity of pkg.entities || []) {
      byEntity.set(entity.id, { entity, pkg, box: null });
      const key = declKey(entity.file, entity.recv, entity.name);
      const list = byDecl.get(key) || [];
      list.push(entity.id);
      byDecl.set(key, list);
      if (entity.kind === "type" || entity.kind === "variable" || entity.kind === "function" || entity.kind === "method") {
        catalog.push({
          kind: entity.kind,
          id: entity.id,
          name: entity.kind === "method" && entity.recv ? entity.recv + "." + entity.name : entity.name,
          extra: pkg.id + " " + (entity.file || ""),
          where: pkg.name || pkg.id,
        });
      }
    }
    for (const block of box.entities) {
      const found = byEntity.get(block.id);
      if (found) found.box = block;
    }
  }
  // Packages go in first, so a package keeps its bare path if a declaration
  // ever spells the same one.
  nodeNames = nameIndex([
    ...(sceneDoc.packages || []).map((pkg) => ({ kind: pkg.external ? "external" : "package", id: pkg.id })),
    ...[...byEntity.values()].filter((item) => item.box).map((item) => ({ kind: "entity", entity: item.entity, pkg: item.pkg.id })),
  ]);
  indexCallers();
}

function declKey(file, recv, name) {
  return (file || "") + "\0" + (recv || "") + "\0" + (name || "");
}

function findDecl(step) {
  const list = byDecl.get(declKey(step.path, step.recv, step.name)) || [];
  for (const id of list) {
    const found = byEntity.get(id);
    if (found && found.box) return found;
  }
  return list.length ? byEntity.get(list[0]) : null;
}

function declaredTarget(step) {
  if (!step || !step.resolved) return null;
  const target = findDecl(step);
  if (!target || !target.box) return null;
  const kind = target.entity.kind;
  if (kind !== "function" && kind !== "method") return null;
  return target;
}

function citySpan() {
  const b = laid.bounds;
  return Math.max(b.maxX - b.minX, b.maxZ - b.minZ, 20);
}

function frameCity(margin = 0.92) {
  const b = laid.bounds;
  const cx = (b.minX + b.maxX) / 2;
  const cz = (b.minZ + b.maxZ) / 2;
  const span = citySpan();
  // The ground disc is built from this same radius, centered on the origin.
  const groundRadius = span * 0.95;
  const look = { x: cx, y: Math.max(b.maxY * 0.2, 0.5), z: cz };
  // In front of the city (+z), a little to the right (+x), above the disc.
  const dir = { x: span * 0.22, y: b.maxY + span * 0.2 - look.y, z: span * 0.7 };
  const points = [];
  for (let i = 0; i < 48; i++) {
    const angle = (i / 48) * Math.PI * 2;
    points.push({ x: Math.cos(angle) * groundRadius, y: 0, z: Math.sin(angle) * groundRadius });
  }
  points.push(
    { x: b.minX, y: 0, z: b.minZ },
    { x: b.maxX, y: 0, z: b.minZ },
    { x: b.minX, y: 0, z: b.maxZ },
    { x: b.maxX, y: 0, z: b.maxZ },
    { x: b.minX, y: b.maxY, z: b.minZ },
    { x: b.maxX, y: b.maxY, z: b.minZ },
    { x: b.minX, y: b.maxY, z: b.maxZ },
    { x: b.maxX, y: b.maxY, z: b.maxZ },
  );
  for (const ext of laid.externals) {
    points.push({ x: ext.x, y: ext.y + ext.h, z: ext.z });
  }
  const aspect = Math.max(viewInsets.minAspect, (viewInsets.width / Math.max(1, viewInsets.height)) * (viewInsets.free / Math.max(1, viewInsets.width)));
  const dist = fitDistance(points, look, dir, viewInsets.fov, aspect, margin);
  const len = Math.hypot(dir.x, dir.y, dir.z) || 1;
  return {
    pos: new THREE.Vector3(look.x + (dir.x / len) * dist, look.y + (dir.y / len) * dist, look.z + (dir.z / len) * dist),
    target: new THREE.Vector3(look.x, look.y, look.z),
    dist,
  };
}

function applyFitLimits(pose) {
  const dist = pose.dist || camera.position.distanceTo(pose.target);
  controls.maxDistance = Math.max(citySpan() * 4, dist * 1.35);
  camera.far = Math.max(2000, dist * 4);
  camera.updateProjectionMatrix();
}

async function buildCity(progress = () => {}) {
  const span = citySpan();
  scene.fog.color.set(theme.fog.color);
  scene.fog.density = theme.fog.falloff / span;
  const groundRadius = span * 0.95;
  const planeSize = Math.max(groundRadius * 8, 80);
  const planeSeg = theme.plane.vertexSource ? (theme.plane.segments || 64) : 1;
  // The field and the disc carry an opacity, so a skin can leave the city
  // floating on its disc: both are transparent materials, because a boolean
  // like `transparent` cannot be faded and the fade lerps opacity.
  planeMesh = new THREE.Mesh(
    new THREE.PlaneGeometry(planeSize, planeSize, planeSeg, planeSeg),
    rememberShade(dress(new THREE.MeshLambertMaterial({
      color: theme.plane.color,
      transparent: true,
      depthWrite: (theme.plane.opacity ?? 1) >= 1,
      opacity: theme.plane.opacity ?? 1,
    }), theme.plane, theme), theme.plane),
  );
  planeMesh.rotation.x = -Math.PI / 2;
  planeMesh.position.y = -0.12;
  planeMesh.userData = { kind: "plane" };
  city.add(planeMesh);
  groundMesh = new THREE.Mesh(
    new THREE.CircleGeometry(groundRadius, 72),
    rememberShade(dress(new THREE.MeshLambertMaterial({
      color: theme.ground.color,
      transparent: true,
      depthWrite: (theme.ground.opacity ?? 1) >= 1,
      opacity: theme.ground.opacity ?? 1,
    }), theme.ground, theme), theme.ground),
  );
  groundMesh.rotation.x = -Math.PI / 2;
  groundMesh.position.y = -0.04;
  groundMesh.userData = { kind: "ground" };
  city.add(groundMesh);
  horizonMesh = new THREE.Mesh(
    new THREE.RingGeometry(groundRadius * 0.92, groundRadius * 0.935, 80),
    rememberShade(dress(new THREE.MeshBasicMaterial({
      color: theme.horizon.color,
      side: THREE.DoubleSide,
      transparent: true,
      opacity: theme.horizon.opacity,
    }), theme.horizon, theme), theme.horizon),
  );
  horizonMesh.rotation.x = -Math.PI / 2;
  horizonMesh.position.y = 0.01;
  city.add(horizonMesh);

  // A sign is a canvas texture, one per package, so this loop is the slow half
  // of the stage. It is sliced so the bar keeps moving while it runs.
  await sliced(laid.packages.length, (f) => progress(f * 0.5), (i) => {
    const box = laid.packages[i];
    const pkg = byPackage.get(box.id);
    const geom = new THREE.BoxGeometry(box.w, box.h, box.d);
    const material = new THREE.MeshLambertMaterial({
      color: plinthColor(box.depth, box.synthetic),
      emissive: new THREE.Color(theme.package.emissive),
      emissiveIntensity: theme.package.emissiveIntensity,
    });
    tintEmissive(material);
    rememberShade(dress(material, theme.package, theme), theme.package);
    const mesh = new THREE.Mesh(geom, material);
    mesh.position.set(box.x + box.w / 2, box.y + box.h / 2, box.z + box.d / 2);
    mesh.userData = { kind: "package", id: box.id };
    city.add(mesh);
    const plate = namePlate(box);
    if (plate) mesh.add(plate);
    plinths.push({ id: box.id, box, mesh, material, plate, pkg });
  });

  for (const box of laid.externals) {
    const mesh = new THREE.Mesh(
      new THREE.OctahedronGeometry(0.9, 0),
      new THREE.MeshStandardMaterial({
        color: theme.external.color,
        metalness: theme.external.metalness,
        roughness: theme.external.roughness,
        emissive: theme.external.emissive,
        emissiveIntensity: theme.external.emissiveIntensity,
      }),
    );
    rememberShade(dress(mesh.material, theme.external, theme), theme.external);
    mesh.position.set(box.x, box.y + 1.2, box.z);
    mesh.userData = { kind: "external", id: box.id };
    city.add(mesh);
    plinths.push({ id: box.id, box, mesh, material: mesh.material, plate: null, external: true });
  }

  const solids = [];
  await sliced(laid.packages.length, (f) => progress(0.5 + f * 0.15), (i) => {
    const box = laid.packages[i];
    for (const block of box.entities) {
      const found = byEntity.get(block.id);
      if (!found) continue;
      const slot = {
        id: block.id,
        pkgId: box.id,
        kind: block.kind,
        x: block.x, y: block.y, z: block.z, w: block.w, h: block.h, d: block.d,
        entity: found.entity,
      };
      solids.push(slot);
      entitySlots.push(slot);
      slotById.set(slot.id, slot);
    }
  });

  const geo = coloredBox();
  if (theme.entity.vertexSource && theme.entity.vertexSource.includes("aHouse")) {
    geo.setAttribute("aHouse", houseAttribute(solids));
  }
  solidMat = new THREE.MeshLambertMaterial({
    color: theme.entity.color,
    emissive: theme.entity.emissive,
    emissiveIntensity: theme.entity.emissiveIntensity,
    vertexColors: true,
  });
  tintEmissive(solidMat);
  rememberShade(dress(solidMat, theme.entity, theme), theme.entity);
  solidMesh = await makeInstances(geo, solidMat, solids, (f) => progress(0.65 + f * 0.35));
  if (solidMesh) city.add(solidMesh);
}

function coloredBox() {
  // Instanced colors multiply the geometry color. An empty color attribute is
  // black, which zeros the instance color.
  const geo = new THREE.BoxGeometry(1, 1, 1);
  const colors = new Float32Array(geo.attributes.position.count * 3);
  colors.fill(1);
  geo.setAttribute("color", new THREE.BufferAttribute(colors, 3));
  return geo;
}

function houseAttribute(slots) {
  const data = new Float32Array(slots.length * 2);
  for (let i = 0; i < slots.length; i++) {
    const kind = slots[i].kind;
    // 0 type, 1 function, 2 method, 3 variable. A skin that only knows the
    // first three reads a variable as "not a type and not a method".
    data[i * 2] = kind === "type" ? 0 : kind === "method" ? 2 : kind === "variable" ? 3 : 1;
    data[i * 2 + 1] = varyUnit(slots[i].id);
  }
  return new THREE.InstancedBufferAttribute(data, 2);
}

function varyUnit(id) {
  let hash = 0;
  for (let i = 0; i < id.length; i++) hash = (hash * 33 + id.charCodeAt(i)) >>> 0;
  return (hash % 1000) / 999;
}

async function makeInstances(geo, material, slots, progress = null) {
  if (!slots.length) {
    if (progress) progress(1);
    return null;
  }
  const mesh = new THREE.InstancedMesh(geo, material, slots.length);
  mesh.instanceColor = new THREE.InstancedBufferAttribute(new Float32Array(slots.length * 3), 3);
  const color = new THREE.Color();
  // One matrix and one colour per declaration: slicing it keeps the bar alive
  // on a codebase with tens of thousands of them.
  await sliced(slots.length, progress, (index) => {
    const slot = slots[index];
    color.copy(entityColor(slot.entity));
    mesh.setColorAt(index, color);
    writeSlot(mesh, index, slot, 1);
  });
  mesh.instanceColor.needsUpdate = true;
  mesh.instanceMatrix.needsUpdate = true;
  mesh.frustumCulled = false;
  mesh.userData.slots = slots;
  return mesh;
}

function drawState(slot, open) {
  return { overlay: mode === "overlay", lit: !!lit, open };
}

function slotSize(slot, open) {
  return drawnSize(slot, drawState(slot, open));
}

// The drawn box: a method stands on the roof its type is drawn at.
function visualBox(slot, open) {
  const parentId = slot.entity && slot.entity.kind === "method" ? slot.entity.parent : "";
  const parent = parentId ? slotById.get(parentId) : null;
  return drawnBox(slot, drawState(slot, open), parent, parent ? drawState(parent, slotOpen(parent)) : null);
}

function writeSlot(mesh, index, slot, open) {
  const box = visualBox(slot, open);
  const drawn = slot.drawn;
  if (drawn && drawn[0] === box.x && drawn[1] === box.y && drawn[2] === box.z && drawn[3] === box.w && drawn[4] === box.h && drawn[5] === box.d) {
    return false;
  }
  slot.drawn = [box.x, box.y, box.z, box.w, box.h, box.d];
  scratch.pos.set(box.x + box.w / 2, box.y + box.h / 2, box.z + box.d / 2);
  scratch.scale.set(box.w, box.h, box.d);
  scratch.mat.compose(scratch.pos, scratch.quat, scratch.scale);
  mesh.setMatrixAt(index, scratch.mat);
  return true;
}

// A sign is painted at this size and then stretched over its plate: a bigger
// canvas is what keeps the text sharp now that a plate grows with its building.
const SIGN_FONT = 96;
const SIGN_PAD = 28;

function signFont() {
  return "600 " + SIGN_FONT + "px ui-monospace, monospace";
}

function drawSign(canvas, text, textColor, background) {
  const ctx = canvas.getContext("2d");
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  if (background && background !== "transparent") {
    ctx.fillStyle = background;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  }
  ctx.font = signFont();
  ctx.fillStyle = textColor;
  ctx.textBaseline = "middle";
  ctx.fillText(text, SIGN_PAD / 2, canvas.height / 2);
}

function redrawPlates(textColor, background) {
  for (const plinth of plinths) {
    const plate = plinth.plate;
    const tex = plate && plate.userData.sign;
    if (!tex) continue;
    drawSign(tex.image, plate.userData.signText, textColor, background);
    tex.needsUpdate = true;
  }
}

function signTexture(label) {
  const probe = document.createElement("canvas").getContext("2d");
  probe.font = signFont();
  const text = label.length > 22 ? label.slice(0, 21) + "…" : label;
  const textW = Math.ceil(probe.measureText(text).width);
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(SIGN_FONT * 1.34, textW + SIGN_PAD);
  canvas.height = Math.round(SIGN_FONT * 1.5);
  const background = theme.label.background;
  drawSign(canvas, text, theme.label.text, background);
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
  return { tex, aspect: canvas.width / canvas.height, text };
}

function setPlateOpacity(plate, opacity) {
  if (!plate) return;
  const mats = new Set();
  plate.traverse((obj) => {
    if (obj.material) mats.add(obj.material);
  });
  for (const mat of mats) {
    mat.opacity = opacity;
    mat.transparent = true;
  }
}

// The name sits on all four faces, just under the roof, facing outward.
// PlaneGeometry faces local +z. rotation.y turns that normal to each side:
// 0 → +z, π → −z, +π/2 → +x, −π/2 → −x.
// A sign is as big as its wall allows: as wide as the face it sits on, and no
// taller than the building can carry. A tall package gets a big sign, a flat
// one a small one, and the two pairs of faces get a geometry each because the
// front of a building is usually wider than its side.
function plateGeometry(span, boxHeight, aspect) {
  const width = Math.max(0.6, span * 0.94);
  const maxHeight = Math.max(0.5, Math.min(boxHeight * 0.62, 4));
  const height = Math.min(width / aspect, maxHeight);
  return new THREE.PlaneGeometry(height * aspect, height);
}

function namePlate(box) {
  if (Math.max(box.w, box.d) < 5 || Math.min(box.w, box.d) < 2.2) return null;
  const { tex, aspect, text } = signTexture(box.name || box.id);
  const material = new THREE.MeshBasicMaterial({ map: tex, transparent: true, depthWrite: false, fog: false });
  const group = new THREE.Group();
  group.userData.sign = tex;
  group.userData.signText = text;
  const gap = 0.045;
  const faces = [
    [0, box.d / 2 + gap, 0, box.w],
    [0, -box.d / 2 - gap, Math.PI, box.w],
    [box.w / 2 + gap, 0, Math.PI / 2, box.d],
    [-box.w / 2 - gap, 0, -Math.PI / 2, box.d],
  ];
  for (const [x, z, rot, span] of faces) {
    const geo = plateGeometry(span, box.h, aspect);
    const mesh = new THREE.Mesh(geo, material);
    mesh.position.set(x, box.h / 2 - geo.parameters.height / 2 - 0.06, z);
    mesh.rotation.y = rot;
    group.add(mesh);
  }
  return group;
}

function plinthColor(depth, synthetic) {
  if (synthetic) return new THREE.Color(theme.package.synthetic);
  const steps = theme.package.steps;
  return new THREE.Color(steps[Math.min(Math.max(depth, 0), steps.length - 1)]);
}

function entityColor(entity) {
  if (mode === "overlay" && entity.change && entity.change !== "same") {
    return changeColor(entity.change, entity.part);
  }
  // A variable reuses the method grey: it is a declaration without a body,
  // and a new skin key would have to be added to every skin to give it its
  // own colour. The grey already reads as the quieter kind of block.
  const base = entity.kind === "type" ? paint.type
    : entity.kind === "method" || entity.kind === "variable" ? paint.method
    : paint.function;
  return vary(entity.id, base);
}

function vary(id, color) {
  let hash = 0;
  for (let i = 0; i < id.length; i++) hash = (hash * 33 + id.charCodeAt(i)) >>> 0;
  const shift = ((hash % 17) - 8) / 220;
  return color.clone().offsetHSL(0, 0, shift);
}

// The overview draws an arc for every import that was added or deleted, and
// for every pair of modules whose calls changed while the import stayed.
async function buildArcs(progress = () => {}) {
  // The calls the diff changed are drawn between the towers that make them:
  // the same arcs a selection draws, for the whole range at once. The frame
  // the selection would use is left alone — the overview is not a selection.
  // This comes first because it starts from an empty group.
  await drawCallLinks(arcGroup, mergePairLinks(changedCallLinks()), false, (f) => progress(f * 0.85));
  // Added and removed package dependencies stay module-level: they are edges
  // between modules, not calls, and there is no tower to hang them on.
  const drawn = new Set();
  const draw = (from, to, change) => {
    const start = packageAnchor(from);
    const end = packageAnchor(to);
    if (!start || !end) return;
    drawn.add(from + "\0" + to);
    addArc(arcGroup, start, end, changeColor(change), clearLift(start, end, [from, to]), {
      kind: "dep", id: to, label: to, change, from,
    }, from, to);
  };
  const deps = [];
  for (const pkg of sceneDoc.packages || []) {
    if (pkg.external) continue;
    for (const dep of pkg.deps || []) {
      if (dep.change !== "added" && dep.change !== "removed") continue;
      deps.push([pkg.id, dep.to, dep.change]);
    }
  }
  await sliced(deps.length, (f) => progress(0.85 + f * 0.15), (i) => {
    draw(deps[i][0], deps[i][1], deps[i][2]);
  });
}

// Every call the diff changed, as a link between the two towers involved.
function changedCallLinks() {
  const links = [];
  for (const found of byEntity.values()) {
    if (!found.box) continue;
    for (const link of entityLinks(found)) {
      if (link.attached) continue;
      const change = link.step && link.step.change;
      if (!change || change === "same") continue;
      links.push({ from: found, target: link.target, far: link.target, change, step: link.step });
    }
  }
  return links;
}

// The arc starts a little above the roof, so it is plainly leaving the
// building rather than skimming it.
const LAND = 0.25;

let ownTopCache = null;
function ownTop(id) {
  if (!ownTopCache) {
    ownTopCache = new Map();
    for (const box of laid.packages) ownTopCache.set(box.id, box.y + box.h);
    for (const slot of entitySlots) {
      ownTopCache.set(slot.pkgId, Math.max(ownTopCache.get(slot.pkgId) || 0, slot.y + slot.h));
    }
  }
  return ownTopCache.get(id) || 0;
}

// While a module is selected, other towers are shut. Land on the roof that is
// actually drawn. With nothing selected, use the laid tower tops.
function shownOwnTop(id) {
  const box = laid.packages.find((item) => item.id === id);
  const plinth = box ? box.y + box.h : 0;
  if (!lit) return Math.max(plinth, ownTop(id));
  let top = plinth;
  for (const slot of entitySlots) {
    if (slot.pkgId !== id) continue;
    const drawn = visualBox(slot, slotOpen(slot));
    top = Math.max(top, drawn.y + drawn.h);
  }
  return top;
}

function packageRoof(id) {
  const box = laid.packages.find((item) => item.id === id);
  if (box) return [box.x + box.w / 2, box.y + box.h, box.z + box.d / 2];
  const ext = laid.externals.find((item) => item.id === id);
  if (ext) return [ext.x, ext.y + ext.h, ext.z];
  return null;
}

function packageAnchor(id) {
  const box = laid.packages.find((item) => item.id === id);
  if (!box) {
    const ext = laid.externals.find((item) => item.id === id);
    if (ext) return [ext.x, ext.y + ext.h + 0.7, ext.z];
    return null;
  }
  // Right on the roof: the arc starts and ends where the eye expects, and the
  // curve's steep egress keeps it off the neighbours. Raising the anchor to the
  // neighbourhood's skyline made the ends float above the buildings, which read
  // worse than the collision it avoided.
  return [box.x + box.w / 2, box.y + box.h + LAND, box.z + box.d / 2];
}
function rectContainsXZ(box, x, z) {
  return x >= box.x && x <= box.x + box.w && z >= box.z && z <= box.z + box.d;
}

// t along the ground segment where the line is over the rectangle.
function localTop(x, z) {
  let top = 0;
  for (const box of laid.packages) {
    if (!rectContainsXZ(box, x, z)) continue;
    top = Math.max(top, shownOwnTop(box.id));
  }
  return top;
}

// The two control points of the arc. They sit directly above the ends, so the
// curve leaves its roof steeply instead of crawling out flat — a quadratic
// pinned at the midpoint could not clear a tower standing right beside an
// endpoint, however high its apex was raised, and the clearance loop used to
// run away to a lift of 350 without gaining a metre.
// This is the curve: makeLink draws it, the particles ride it, and the
// clearance loop measures it, so there is no second interpretation to disagree
// with.
function arcControls(from, to, lift) {
  return [
    [from[0], from[1] + lift, from[2]],
    [to[0], to[1] + lift, to[2]],
  ];
}

function arcSamples(from, to, lift, n) {
  const steps = n || 48;
  const [c1, c2] = arcControls(from, to, lift);
  const out = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const u = 1 - t;
    const a = u * u * u;
    const b = 3 * u * u * t;
    const c = 3 * u * t * t;
    const d = t * t * t;
    out.push([
      a * from[0] + b * c1[0] + c * c2[0] + d * to[0],
      a * from[1] + b * c1[1] + c * c2[1] + d * to[1],
      a * from[2] + b * c1[2] + c * c2[2] + d * to[2],
    ]);
  }
  return out;
}

// How deep the path sinks into a building, in world units, and where. One
// definition of a collision for the clearance loop and for ?check=1.
// The package and everything under it: the arc's own two buildings, which it is
// allowed to cross — starting on a roof means starting in that roof's own tower
// field. Everything else is an obstacle.
function inSubtree(boxId, pkgId) {
  if (!pkgId) return false;
  // Same building: the package itself, everything stacked inside it, and the
  // plates it stands on. In a treemap, reaching a nested package means crossing
  // its ancestors' volumes, so they cannot count as obstacles either.
  return boxId === pkgId || boxId.startsWith(pkgId + "/") || pkgId.startsWith(boxId + "/");
}

function worstPenetration(points, margin = 0, skip) {
  let worst = 0;
  let at = null;
  let atT = 0;
  const steps = Math.max(1, points.length - 1);
  for (let i = 0; i < points.length; i++) {
    const p = points[i];
    for (const box of laid.packages) {
      if (!rectContainsXZ(box, p[0], p[2])) continue;
      if (skip && skip.some((id) => inSubtree(box.id, id))) continue;
      const top = shownOwnTop(box.id) + margin;
      if (p[1] >= top) continue;
      const depth = top - p[1];
      if (depth > worst) {
        worst = depth;
        at = box.id;
        atT = i / steps;
      }
    }
  }
  return { depth: worst, box: at, t: atT };
}

// Raise the apex until the curve is measured clear. There used to be a closed
// form here: it reasoned about a quadratic while the tube was drawn along a
// Catmull-Rom through its samples, it skipped every box whose footprint held an
// endpoint (which, in a treemap, is every child tower at the ends), and it
// capped the result, turning every large requirement into a silent collision.
function clearLift(from, to, ends) {
  return Math.min(measuredLift(from, to, ends), liftCap(from, to));
}

// The lift that puts the middle of the arc a little above the tallest roof in
// the city, or a fair bow for the distance, whichever is more. A loop that
// chases an obstacle beside an endpoint can ask for several times that and
// send the arc far out of frame.
function liftCap(from, to) {
  const dist = Math.hypot(to[0] - from[0], to[2] - from[2]);
  const base = Math.min(from[1], to[1]);
  const over = (laid.bounds.maxY + 2 - base) / 0.75;
  return Math.max(3, dist * 0.25, over);
}

// A call arc bows like main's call arcs did: a fifth of the distance, between
// 3 and 26.
function callLift(from, to) {
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  return Math.max(3, Math.min(dist * 0.22, 26)) / 0.75;
}

function measuredLift(from, to, ends) {
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  let lift = Math.max(3, Math.min(dist * 0.2, 20));
  // Sampled finer than the tube is drawn, with a margin, so a thin tower cannot
  // slip between two samples and the tube radius stays clear as well. The step
  // is the exact lift the worst point still needs: at parameter t the lift
  // contributes 3(u^2 t + u t^2), so dividing by it converges in a few passes
  // instead of creeping up by one unit and running out of iterations.
  for (let i = 0; i < 24; i++) {
    const worst = worstPenetration(arcSamples(from, to, lift, 128), 1, ends);
    if (worst.depth <= 0) return lift;
    // Right at an end nothing can be done by raising the apex: the endpoint is
    // fixed, and packageAnchor has already put it above that column.
    if (worst.t <= 0.004 || worst.t >= 0.996) return lift;
    const u = 1 - worst.t;
    const coef = 3 * (u * u * worst.t + u * worst.t * worst.t);
    lift += worst.depth / Math.max(coef, 0.05) + 0.1;
  }
  return lift;
}
function towerTop(found) {
  if (!found || !found.entity) return null;
  const slot = slotById.get(found.entity.id);
  if (!slot) {
    if (!found.box) return null;
    const box = found.box;
    return [box.x + box.w / 2, box.y + box.h + 0.55, box.z + box.d / 2];
  }
  const box = visualBox(slot, slotOpen(slot));
  return [box.x + box.w / 2, box.y + box.h + 0.55, box.z + box.d / 2];
}

// entityExtent is the top and the base of an entity's drawn block.
function entityExtent(found) {
  if (!found || !found.entity) return [];
  const slot = slotById.get(found.entity.id);
  const box = slot ? visualBox(slot, slotOpen(slot)) : found.box;
  if (!box) return [];
  return [[box.x, box.y, box.z], [box.x + box.w, box.y + box.h + 0.55, box.z + box.d]];
}

function entityAnchor(found) {
  const top = towerTop(found);
  if (top) return top;
  if (found && found.pkg) return packageAnchor(found.pkg.id);
  return null;
}

function makeLink(from, to, color, lift, roofFrom, roofTo) {
  if (!from || !to) return null;
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  if (dist < 0.35) return null;
  // A riser at each end: the anchor sits as high as its neighbourhood demands,
  // and without this the line began in mid-air beside the building. The riser
  // runs up the building's own column from the roof to the anchor, so the arc
  // reads as leaving a roof and landing on one.
  const road = arcSamples(from, to, lift, 24).slice(1, -1);
  const path = [];
  for (const p of [roofFrom, from]) if (p) path.push(new THREE.Vector3(p[0], p[1], p[2]));
  for (const p of road) path.push(new THREE.Vector3(p[0], p[1], p[2]));
  for (const p of [to, roofTo]) if (p) path.push(new THREE.Vector3(p[0], p[1], p[2]));
  const curve = new THREE.CatmullRomCurve3(path);
  const radius = Math.max(0.07, Math.min(dist * 0.0028, 0.22));
  // As many segments as main's bows had. Twice that doubled the triangles the
  // overview draws and bought nothing visible at this radius.
  const segs = (dist > 48 ? 28 : 16) + (roofFrom ? 2 : 0) + (roofTo ? 2 : 0);
  const geo = new THREE.TubeGeometry(curve, segs, radius, 5, false);
  const mat = new THREE.MeshBasicMaterial({ color, fog: false });
  // The tube has real vertices, so its bounds are right and it can be culled
  // when the camera is inside the city.
  return new THREE.Mesh(geo, mat);
}
function isStdPackage(id) {
  const pkg = byPackage.get(id);
  if (!pkg || !pkg.external) return false;
  const head = String(pkg.id || "").split("/")[0];
  return head.length > 0 && !head.includes(".");
}

const flows = [];
const flowScratch = [0, 0, 0];

// Particles run on the GPU. Each flow is one row of a path texture; the vertex
// shader walks the row and the fragment shader cuts a disc out of the sprite.
// The CPU writes a path once and never touches a position again, and the point
// is round instead of the square a bare PointsMaterial draws.
const FLOW_SAMPLES = 64;
let flowTexture = null;
let flowData = null;
let flowRows = 0;

const flowMaterial = new THREE.ShaderMaterial({
  uniforms: {
    uPath: { value: null },
    uRows: { value: 1 },
    uTime: { value: 0 },
    uScale: { value: 800 },
  },
  vertexShader: `
    #define SAMPLES ${FLOW_SAMPLES}.0
    attribute float aRow;
    attribute float aU;
    attribute float aSpeed;
    attribute float aSize;
    attribute vec3 aColor;
    uniform sampler2D uPath;
    uniform float uRows;
    uniform float uTime;
    uniform float uScale;
    varying vec3 vColor;
    void main() {
      float u = fract(aU + uTime * aSpeed);
      float x = u * (SAMPLES - 1.0);
      float i0 = floor(x);
      float f = x - i0;
      float row = (aRow + 0.5) / uRows;
      vec3 p0 = texture2D(uPath, vec2((i0 + 0.5) / SAMPLES, row)).xyz;
      vec3 p1 = texture2D(uPath, vec2((i0 + 1.5) / SAMPLES, row)).xyz;
      vec4 mv = modelViewMatrix * vec4(mix(p0, p1, f), 1.0);
      // The dot rides the centre of the arc's tube. Bring it out in front of
      // the tube so the tube does not hide it.
      mv.xyz += normalize(-mv.xyz) * aSize * 0.6;
      gl_Position = projectionMatrix * mv;
      gl_PointSize = clamp(aSize * uScale / max(0.001, -mv.z), 4.0, 12.0);
      vColor = aColor;
    }
  `,
  fragmentShader: `
    uniform float uTime;
    varying vec3 vColor;
    void main() {
      vec2 d = gl_PointCoord - vec2(0.5);
      float r = dot(d, d) * 4.0;
      if (r > 1.0) discard;
      float a = (1.0 - r * r) * 0.95;
      gl_FragColor = vec4(vColor, a);
      #include <colorspace_fragment>
    }
  `,
  transparent: true,
  depthWrite: false,
  fog: false,
});

// Particle sizes are in world units: the scale is the drawing buffer height
// over the height of the view frustum at distance 1.
updatePointScale = () => {
  const h = renderer.domElement.height || 1;
  flowMaterial.uniforms.uScale.value = h / (2 * Math.tan((camera.fov * Math.PI) / 360));
};
updatePointScale();

function ensureFlowTexture(height) {
  if (flowTexture && flowTexture.image.height === height) return flowTexture;
  const data = new Float32Array(FLOW_SAMPLES * 4 * height);
  if (flowData) data.set(flowData.subarray(0, Math.min(flowData.length, data.length)));
  flowData = data;
  flowTexture = new THREE.DataTexture(data, FLOW_SAMPLES, height, THREE.RGBAFormat, THREE.FloatType);
  flowTexture.minFilter = THREE.NearestFilter;
  flowTexture.magFilter = THREE.NearestFilter;
  flowTexture.needsUpdate = true;
  flowMaterial.uniforms.uPath.value = flowTexture;
  flowMaterial.uniforms.uRows.value = height;
  return flowTexture;
}

function writeFlowRow(flow, row) {
  ensureFlowTexture(Math.max(1, flowRows));
  flow.row = row;
  const base = row * FLOW_SAMPLES * 4;
  for (let i = 0; i < FLOW_SAMPLES; i++) {
    const s = i * 3;
    const o = base + i * 4;
    flowData[o] = flow.samples[s] || 0;
    flowData[o + 1] = flow.samples[s + 1] || 0;
    flowData[o + 2] = flow.samples[s + 2] || 0;
    flowData[o + 3] = 1;
  }
  flowTexture.needsUpdate = true;
}
function syncFlowTexture() {
  flowRows = Math.max(1, flows.length);
  ensureFlowTexture(flowRows);
  for (let i = 0; i < flows.length; i++) {
    writeFlowRow(flows[i].userData.flow, i);
    const rows = flows[i].geometry.getAttribute("aRow");
    rows.array.fill(i);
    rows.needsUpdate = true;
  }
}

function wrapUnit(value) {
  if (!Number.isFinite(value)) return 0;
  const wrapped = value % 1;
  return wrapped < 0 ? wrapped + 1 : wrapped;
}

// Particles run from the caller (t = 0) to the callee (t = 1).
// The bow is stored as numbers. CatmullRomCurve3 keeps one shared scratch
// vector and throws once many arcs are sampled in the same frame.
function makeFlow(from, to, color, lift) {
  if (!from || !to) return null;
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  if (dist < 0.35) return null;
  // The particle path is the same curve the line is drawn along, sampled evenly.
  // writeFlowRow reads x, y, z flat, three numbers per sample.
  const samples = arcSamples(from, to, lift, FLOW_SAMPLES - 1).flat();
  const count = Math.max(3, Math.min(8, Math.round(dist / 9)));
  const size = Math.max(1.2, Math.min(dist * 0.016, 2.4));
  const speed = Math.min(0.45, Math.max(0.12, 14 / Math.max(dist, 1)));
  const geo = new THREE.BufferGeometry();
  geo.setAttribute("position", new THREE.BufferAttribute(new Float32Array(count * 3), 3));
  const rows = new Float32Array(count);
  const params = new Float32Array(count);
  const speeds = new Float32Array(count);
  const sizes = new Float32Array(count);
  const colors = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    rows[i] = flows.length;
    params[i] = i / count;
    speeds[i] = speed;
    sizes[i] = size;
    colors[i * 3] = color.r;
    colors[i * 3 + 1] = color.g;
    colors[i * 3 + 2] = color.b;
  }
  geo.setAttribute("aRow", new THREE.BufferAttribute(rows, 1));
  geo.setAttribute("aU", new THREE.BufferAttribute(params, 1));
  geo.setAttribute("aSpeed", new THREE.BufferAttribute(speeds, 1));
  geo.setAttribute("aSize", new THREE.BufferAttribute(sizes, 1));
  geo.setAttribute("aColor", new THREE.BufferAttribute(colors, 3));
  const mesh = new THREE.Points(geo, flowMaterial);
  mesh.frustumCulled = false;
  mesh.userData.flow = { samples, count, speed, row: flows.length };
  flows.push(mesh);
  flowRows = Math.max(1, flows.length);
  ensureFlowTexture(flowRows);
  writeFlowRow(mesh.userData.flow, flowRows - 1);
  return mesh;
}
function flowShown(mesh) {
  let node = mesh;
  while (node) {
    if (!node.visible) return false;
    node = node.parent;
  }
  return true;
}

// Particles march while they are on screen; the shader does the walking, so
// this only reports whether anything needs a frame.
function tickFlows(dt) {
  let shown = false;
  for (const mesh of flows) {
    if (!flowShown(mesh)) continue;
    shown = true;
    break;
  }
  if (shown) flowMaterial.uniforms.uTime.value += dt > 0 ? dt : 0;
  return shown;
}

// Collision check for the arcs. The clearance maths is a closed-form guess and
// nothing ever verified the geometry that gets drawn, so every collision so far
// was found by eye. This walks the real mesh vertices against the real boxes:
// open the viewer with ?check=1 and it prints one COLLIDE line per offending
// arc to the console. No cost when it is not asked for.
function arcCollisions() {
  const hits = [];
  const v = new THREE.Vector3();
  const tops = new Map();
  for (const box of laid.packages) tops.set(box.id, shownOwnTop(box.id));
  for (const child of arcGroup.children) {
    // Particles compute their positions in the shader, so their buffer is all
    // zeros: only the tube meshes are real geometry to test.
    if (!child.isMesh) continue;
    const attr = child.geometry && child.geometry.attributes && child.geometry.attributes.position;
    if (!attr) continue;
    let worst = null;
    let count = 0;
    const ends = child.userData.ends || [];
    for (let i = 0; i < attr.count; i++) {
      v.fromBufferAttribute(attr, i);
      child.localToWorld(v);
      // The riser is meant to run up its own building: skip the part of the
      // path that is below the anchor and inside reach of either end.
      let riser = false;
      for (const e of ends) {
        if (v.y > e[1]) continue;
        if (Math.hypot(v.x - e[0], v.z - e[2]) <= ANCHOR_REACH + 0.6) riser = true;
      }
      if (riser) continue;
      const skipIds = [child.userData.from, child.userData.to || child.userData.id];
      for (const box of laid.packages) {
        if (skipIds.some((id) => inSubtree(box.id, id))) continue;
        if (v.x < box.x || v.x > box.x + box.w || v.z < box.z || v.z > box.z + box.d) continue;
        const top = tops.get(box.id) || 0;
        if (v.y >= top) continue;
        count++;
        const depth = top - v.y;
        if (!worst || depth > worst.depth) {
          worst = { depth: Math.round(depth * 100) / 100, box: box.id, y: Math.round(v.y * 100) / 100, top: Math.round(top * 100) / 100 };
        }
      }
    }
    if (count) {
      const d = child.userData || {};
      hits.push({ arc: (d.from || "?") + " -> " + (d.label || d.id || "?"), verts: attr.count, inside: count, worst });
    }
  }
  hits.sort((a, b) => b.worst.depth - a.worst.depth);
  return hits;
}

window.citydiffCheck = () => {
  const hits = arcCollisions();
  console.log("COLLIDE total=" + hits.length + " of " + arcGroup.children.length + " arcs");
  for (const hit of hits.slice(0, 25)) {
    console.log("COLLIDE " + JSON.stringify(hit));
  }
  return hits.length;
};

// fromId and toId name the packages whose roofs the arc rises from and lands
// on. A call arc between towers has neither.
function addArc(group, from, to, color, lift, data, fromId, toId) {
  const mesh = makeLink(from, to, color, lift, fromId ? packageRoof(fromId) : null, toId ? packageRoof(toId) : null);
  if (!mesh) return;
  const tint = { kind: data.kind, change: data.change, std: !!data.std };
  mesh.userData = data;
  mesh.userData.tint = tint;
  group.add(mesh);
  const flow = makeFlow(from, to, color, lift);
  if (flow) {
    flow.userData.tint = tint;
    group.add(flow);
  }
}

function paintLinks(group) {
  if (!group) return;
  for (const child of group.children) {
    const tint = child.userData && child.userData.tint;
    if (!tint) continue;
    const color = tint.kind === "call" ? linkColor(tint.change, tint.std) : changeColor(tint.change);
    if (child.material && child.material.color) child.material.color.copy(color);
    const attr = child.geometry && child.geometry.getAttribute && child.geometry.getAttribute("aColor");
    if (!attr) continue;
    for (let i = 0; i < attr.count; i++) attr.setXYZ(i, color.r, color.g, color.b);
    attr.needsUpdate = true;
  }
}

function paintPlinths() {
  const overlay = mode === "overlay";
  for (const plinth of plinths) {
    const pkgObj = byPackage.get(plinth.id);
    const change = (pkgObj || {}).change || "same";
    const role = packageRole(plinth.id);
    const marked = overlay && change !== "same";
    if (!plinth.material) continue;
    if (plinth.external) {
      if (marked) plinth.material.color.copy(changeColor(change));
      else if (role === "dep") plinth.material.color.copy(paint.call);
      else plinth.material.color.set(theme.external.color);
      if (role === "dep" || marked) plinth.material.emissive.copy(plinth.material.color);
      else plinth.material.emissive.set(theme.external.emissive);
      plinth.material.emissiveIntensity = role === "dep" ? theme.external.depEmissive : theme.external.emissiveIntensity;
      plinth.material.metalness = theme.external.metalness;
      plinth.material.roughness = theme.external.roughness;
      if (role === "dim") {
        plinth.material.color.multiplyScalar(theme.dim.external);
        plinth.material.emissive.multiplyScalar(theme.dim.external);
      }
      continue;
    }
    // A package whose file changed but whose declarations did not is a quiet
    // change: it keeps the modified hue, muted, instead of the bright one.
    const low = isLowLevel(pkgObj);
    if (role === "dep" && !marked) plinth.material.color.copy(paint.call);
    else if (marked && low) plinth.material.color.copy(lowLevelColor());
    else plinth.material.color.copy(marked ? changeColor(change) : plinthColor(plinth.box.depth, plinth.box.synthetic));
    plinth.material.emissive.set(theme.package.emissive);
    plinth.material.emissiveIntensity = role === "dep" ? theme.package.depEmissive : (marked && !low ? theme.package.markedEmissive : theme.package.emissiveIntensity);
    if (plinth.plate) plinth.plate.visible = change !== "removed" || overlay;
    if (role === "dim") {
      plinth.material.color.multiplyScalar(theme.dim.package);
      plinth.material.emissiveIntensity = theme.dim.emissive;
      setPlateOpacity(plinth.plate, theme.dim.plate);
    } else {
      setPlateOpacity(plinth.plate, 1);
    }
  }
  recolor(solidMesh);
  if (solidMat) {
    solidMat.color.set(theme.entity.color);
    solidMat.emissive.set(theme.entity.emissive);
    solidMat.emissiveIntensity = theme.entity.emissiveIntensity;
  }
  paintLinks(arcGroup);
  paintLinks(selectArcs);
  paintLinks(focusGroup);
}

function applyMode() {
  linkFrame = [];
  syncLit();
  updateHalo();
  const overlay = mode === "overlay";
  for (const plinth of plinths) {
    const change = (byPackage.get(plinth.id) || {}).change || "same";
    const removed = change === "removed";
    const show = plinth.external || !removed || overlay;
    plinth.mesh.visible = show;
    if (plinth.plate) plinth.plate.visible = !removed || overlay;
    if (!plinth.external && plinth.material) {
      plinth.material.transparent = false;
      plinth.material.opacity = 1;
      plinth.material.depthWrite = true;
      plinth.material.wireframe = false;
    }
  }
  if (solidMat) {
    solidMat.transparent = false;
    solidMat.opacity = 1;
    solidMat.depthWrite = true;
    solidMat.needsUpdate = true;
  }
  arcGroup.visible = overlay && !lit;
  if (focus) {
    ring.visible = false;
    selectArcs.visible = false;
    buildFocus();
  } else if (entitySubject) {
    ring.visible = false;
    selectArcs.visible = true;
    const found = byEntity.get(entitySubject);
    if (found) drawEntityLinks(selectArcs, found);
    else clearGroup(selectArcs);
  } else {
    selectArcs.visible = true;
    if (entered && laid && !(lit && lit.entities)) placeRing(entered);
    else ring.visible = false;
    if (arcSubject && lit && lit.links && lit.links.length) drawCallLinks(selectArcs, lit.links);
    else if (arcSubject) drawSelectionArcs(arcSubject.id, callInbound);
    else if (tourLinks) drawCallLinks(selectArcs, tourLinks);
    else {
      clearGroup(selectArcs);
      linkFrame = [];
    }
  }
  paintPlinths();
  updateHUD();
  syncURL();
  if (bird.on) {
    birdKey = "";
    buildBird();
    birdHide();
  }
  viewDirty = true;
  requestFrame();
}

// The Changes view fades what took no part in the diff. Overview draws one
// snapshot, so nothing fades there, and a range without a diff has no change
// to keep and fades nothing either.
function overlayFade() {
  return mode === "overlay" && !!(sceneDoc && sceneDoc.diff);
}

// What took part in the diff. A package or a declaration is involved when it
// changed, when a changed call touches it, or when a changed import ties it to
// another package. A package also carries its descendants: the path down to a
// change stays on screen. Worked out once per scene, because the diff does not
// move.
function overlayInvolvement() {
  if (overlayTint) return overlayTint;
  const packages = new Set();
  const entities = new Set();
  for (const pkg of sceneDoc.packages || []) {
    if (pkg.change && pkg.change !== "same") packages.add(pkg.id);
  }
  for (const found of byEntity.values()) {
    if (found.entity.change && found.entity.change !== "same") {
      entities.add(found.entity.id);
      if (found.pkg) packages.add(found.pkg.id);
    }
  }
  // Both ends of a changed call are part of the change, even the end that did
  // not change itself: the call is what moved.
  for (const link of changedCallLinks()) {
    entities.add(link.from.entity.id);
    entities.add(link.target.entity.id);
    if (link.from.pkg) packages.add(link.from.pkg.id);
    if (link.target.pkg) packages.add(link.target.pkg.id);
  }
  // An added or removed import is a change of its own, with no declaration
  // behind it.
  for (const pkg of sceneDoc.packages || []) {
    if (pkg.external) continue;
    for (const dep of pkg.deps || []) {
      if (dep.change !== "added" && dep.change !== "removed") continue;
      packages.add(pkg.id);
      if (byPackage.has(dep.to)) packages.add(dep.to);
    }
  }
  for (const id of [...packages]) {
    let parent = (byPackage.get(id) || {}).parent;
    while (parent && !packages.has(parent)) {
      packages.add(parent);
      parent = (byPackage.get(parent) || {}).parent;
    }
  }
  overlayTint = { packages, entities };
  return overlayTint;
}

// The file changed under the package but no declaration in it did: imports,
// re-exports, comments, formatting. A real change, and a quiet one.
function isLowLevel(pkg) {
  if (!pkg || !overlayFade() || pkg.change !== "modified") return false;
  return !declaredEntities(pkg).some((entity) => entity.change && entity.change !== "same");
}

// The muted yellow a low level change is drawn in: the modified hue,
// desaturated and darker, so it is plainly the same family and plainly quieter.
function lowLevelColor() {
  const color = changeColor("modified").clone();
  const hsl = { h: 0, s: 0, l: 0 };
  color.getHSL(hsl);
  color.setHSL(hsl.h, hsl.s * 0.5, hsl.l * 0.55);
  return color;
}

// "dep" is a module on the other end of a module arc. It stays bright and
// takes the arc color. "dim" is everyone else while a selection is up.
function packageRole(id) {
  if (!lit) {
    if (!overlayFade()) return "idle";
    return overlayInvolvement().packages.has(id) ? "idle" : "dim";
  }
  if (lit.packages && lit.packages.has(id)) {
    if (arcSubject && arcSubject.id === id) return "subject";
    return "dep";
  }
  if (lit.entities) {
    for (const entityId of lit.entities) {
      const found = byEntity.get(entityId);
      if (found && found.pkg && found.pkg.id === id) return "subject";
    }
  }
  return "dim";
}

function entityIsLit(id, pkgId) {
  if (!lit) {
    if (!overlayFade()) return true;
    return overlayInvolvement().entities.has(id);
  }
  if (lit.entities) return lit.entities.has(id);
  if (lit.packages && lit.packages.has(pkgId)) return true;
  if (lit.browse && pkgId === lit.browse) return true;
  return false;
}

function packageEdges(id, inbound) {
  const edges = [];
  if (inbound) {
    for (const pkg of sceneDoc.packages || []) {
      if (pkg.external) continue;
      for (const dep of pkg.deps || []) {
        if (dep.to !== id) continue;
        if (mode !== "overlay" && dep.change === "removed") continue;
        edges.push({ from: pkg.id, to: id, change: dep.change });
      }
    }
  } else {
    const pkg = byPackage.get(id);
    for (const dep of (pkg && pkg.deps) || []) {
      if (mode !== "overlay" && dep.change === "removed") continue;
      edges.push({ from: id, to: dep.to, change: dep.change });
    }
  }
  return edges;
}

function entityLinks(found) {
  if (!found) return [];
  if (found.entity.kind === "type") {
    // A type does not call its methods: they are drawn on top of it, as towers
    // on its own base. The links are kept for the list — a method is one click
    // away there — and marked so the city does not also draw a curve to a
    // tower the eye can already see standing on the type.
    const links = [];
    for (const other of byEntity.values()) {
      if (other.entity.kind !== "method" || other.entity.parent !== found.entity.id || !other.box) continue;
      links.push({ target: other, change: other.entity.change || "same", step: null, attached: true });
    }
    return links;
  }
  if (found.entity.kind !== "function" && found.entity.kind !== "method") return [];
  const byTarget = new Map();
  for (const step of found.entity.calls || []) {
    const target = declaredTarget(step);
    if (!target || target.entity.id === found.entity.id) continue;
    const prev = byTarget.get(target.entity.id);
    if (!prev) byTarget.set(target.entity.id, { target, change: step.change, step });
    else prev.change = strongerChange(prev.change, step.change);
  }
  return [...byTarget.values()];
}

function isCallable(found) {
  if (!found) return false;
  const kind = found.entity.kind;
  return kind === "function" || kind === "method";
}

// One arc per caller. Several calls from the same function share it,
// and the stronger change is the one the arc keeps.
function indexCallers() {
  callersOf.clear();
  for (const found of byEntity.values()) {
    if (!isCallable(found) || !found.box) continue;
    for (const step of found.entity.calls || []) {
      const target = declaredTarget(step);
      if (!target || target.entity.id === found.entity.id) continue;
      let byCaller = callersOf.get(target.entity.id);
      if (!byCaller) {
        byCaller = new Map();
        callersOf.set(target.entity.id, byCaller);
      }
      const prev = byCaller.get(found.entity.id);
      if (!prev) byCaller.set(found.entity.id, { target: found, change: step.change, step });
      else prev.change = strongerChange(prev.change, step.change);
    }
  }
}

function callerLinks(found) {
  if (!found) return [];
  const byCaller = callersOf.get(found.entity.id);
  return byCaller ? [...byCaller.values()] : [];
}

function isType(found) {
  return !!found && found.entity.kind === "type";
}

// A type has no call list of its own: it is called through its methods. Its
// callers are the functions that call any method it declares.
function typeCallerLinks(found) {
  const byCaller = new Map();
  for (const other of byEntity.values()) {
    if (other.entity.kind !== "method" || other.entity.parent !== found.entity.id || !other.box) continue;
    for (const link of callerLinks(other)) {
      const prev = byCaller.get(link.target.entity.id);
      if (!prev) byCaller.set(link.target.entity.id, { target: link.target, change: link.change, step: link.step });
      else prev.change = strongerChange(prev.change, link.change);
    }
  }
  return [...byCaller.values()];
}

// The directions a node has: a function or method reverses its calls, a type
// the methods it declares, a package its dependency edges (see packageEdges).
function hasDirection(found) {
  return isCallable(found) || isType(found);
}

// What the subject draws. A type keeps its methods either way: only the
// functions calling those methods are added when the direction is reversed.
function subjectLinks(found) {
  if (!found) return [];
  if (isType(found)) return callInbound ? typeCallersOutside(found) : [];
  if (callInbound && isCallable(found)) return callerLinks(found);
  return entityLinks(found);
}

// A type does not call anything: its methods are towers on its own base, not
// calls it makes, so there is nothing to list in that direction. What reaches
// into a type is worth listing, and that is the callers of its methods — the
// ones from outside it, because a method calling a sibling is the same
// building talking to itself.
function typeCallersOutside(found) {
  return typeCallerLinks(found).filter((link) => link.target.entity.parent !== found.entity.id);
}

function litForEntity(found) {
  if (!found) return null;
  const entities = new Set([found.entity.id]);
  for (const link of subjectLinks(found)) entities.add(link.target.entity.id);
  return { entities, packages: null };
}

// The calls that leave a package are the calls its towers make. A package is
// not one caller: the arc starts at the function or method that writes the
// call, so the city shows which declaration reaches where.
function packageCallLinks(id) {
  const links = [];
  for (const found of byEntity.values()) {
    if (!found.box || !found.pkg || found.pkg.id !== id) continue;
    for (const link of entityLinks(found)) {
      // A method attached to a type is already a tower on that type: no curve.
      if (link.attached) continue;
      links.push({ from: found, target: link.target, far: link.target, change: link.change, step: link.step });
    }
  }
  return links;
}

// The calls that enter a package land on the towers that are called, and they
// start at the tower that makes the call.
function packageCallerLinks(id) {
  const links = [];
  for (const found of byEntity.values()) {
    if (!found.box || !found.pkg || found.pkg.id !== id) continue;
    for (const link of callerLinks(found)) {
      links.push({ from: link.target, target: found, far: link.target, change: link.change, step: link.step });
    }
  }
  return links;
}

// One arc per pair of towers. Several calls from one function to another share
// it, and the stronger change is the one the arc keeps. Added and deleted
// together read as changed.
function mergePairLinks(links) {
  const byPair = new Map();
  const rank = { same: 0, modified: 1, removed: 2, added: 3 };
  for (const link of links) {
    const key = link.from.entity.id + "\u0000" + link.target.entity.id;
    const prev = byPair.get(key);
    if (!prev) {
      byPair.set(key, { from: link.from, target: link.target, far: link.far || link.target, change: link.change, step: link.step, changes: new Set([link.change]) });
      continue;
    }
    prev.changes.add(link.change);
    if ((rank[link.change] || 0) >= (rank[prev.change] || 0)) {
      prev.step = link.step;
      prev.change = link.change;
    }
  }
  for (const link of byPair.values()) {
    if (link.changes.has("added") && link.changes.has("removed")) link.change = "modified";
  }
  return [...byPair.values()];
}

function litForPackage(id, inbound) {
  const packages = new Set([id]);
  const links = inbound ? packageCallerLinks(id) : packageCallLinks(id);
  if (links.length) {
    const entities = new Set();
    for (const link of links) {
      entities.add(link.from.entity.id);
      entities.add(link.target.entity.id);
      if (link.target.pkg) packages.add(link.target.pkg.id);
      if (link.from.pkg) packages.add(link.from.pkg.id);
    }
    return { packages, entities, browse: null, links: mergePairLinks(links) };
  }
  // No tower here makes a call, or is called: a package outside the tree has
  // no declarations at all, so its dependency fan is all there is to draw.
  if (inbound) {
    for (const edge of packageEdges(id, true)) packages.add(edge.from);
    return { packages, entities: null, browse: null, links: null };
  }
  for (const edge of packageEdges(id, false)) packages.add(edge.to);
  return { packages, entities: null, browse: id, links: null };
}

function syncLit() {
  if (tourLit) lit = tourLit;
  else if (focus) lit = litForEntity(focus);
  else if (entitySubject) lit = litForEntity(byEntity.get(entitySubject));
  else if (arcSubject) lit = litForPackage(arcSubject.id, callInbound);
  else lit = null;
}

function drawEntityLinks(group, found) {
  // Callers are stored as the link target, the same shape as a callee. The
  // arc is drawn from the caller to this tower, so the particle walk, which
  // always runs from the first end to the second, comes back in.
  const inbound = callInbound && hasDirection(found);
  const links = subjectLinks(found).filter((link) => !link.attached).map((link) => (inbound
    ? { from: link.target, target: found, far: link.target, change: link.change, step: link.step }
    : { from: found, target: link.target, far: link.target, change: link.change, step: link.step }));
  drawCallLinks(group, links);
  if (!linkPoints || !linkPoints.length) {
    const top = towerTop(found);
    linkPoints = top ? [top] : [];
  }
}

// The arcs for a list of links. The initial build passes a progress callback and
// is sliced across frames; every interactive caller passes none, so the loop
// runs in one go and linkPoints / linkFrame are ready before the call returns.
async function drawCallLinks(group, links, publish = true, progress = null) {
  clearGroup(group);
  const points = [];
  const frame = [];
  const drawOne = (i) => {
    const link = links[i];
    const from = towerTop(link.from);
    const to = towerTop(link.target);
    if (!from || !to) return;
    // Every caller is its own end. Pushing `from` once would keep only the first.
    points.push(from, to);
    const far = link.far || link.target;
    const std = !!(far.pkg && isStdPackage(far.pkg.id));
    const color = linkColor(link.change, std);
    const data = { kind: "call", entityId: far.entity.id, step: link.step, label: entityLabel(far.entity), change: link.change, std };
    const lift = callLift(from, to);
    addArc(group, from, to, color, lift, data);
    frame.push(...entityExtent(link.from), ...entityExtent(link.target));
    frame.push([(from[0] + to[0]) / 2, (from[1] + to[1]) / 2 + lift * 0.75, (from[2] + to[2]) / 2]);
  };
  if (progress) await sliced(links.length, progress, drawOne);
  else for (let i = 0; i < links.length; i++) drawOne(i);
  if (publish) {
    linkPoints = points;
    linkFrame = frame;
  }
}

function changeColor(change, part) {
  if (change === "added") return paint.added;
  if (change === "removed") return paint.removed;
  if (change === "modified") {
    if (part === "body") return paint.body;
    if (part === "both") return paint.both;
    return paint.modified;
  }
  if (change === "moved") return paint.moved;
  return paint.same;
}

function recolor(mesh) {
  if (!mesh) return;
  const color = new THREE.Color();
  mesh.userData.slots.forEach((slot, index) => {
    color.copy(entityColor(slot.entity));
    if (!entityIsLit(slot.id, slot.pkgId)) color.multiplyScalar(theme.dim.entity);
    mesh.setColorAt(index, color);
  });
  mesh.instanceColor.needsUpdate = true;
}

// One called function does not open the rest of its package.
// Only the functions on the call stay up. The others shut.
function slotOpen(slot) {
  if (!lit) return 1;
  if (lit.entities) return lit.entities.has(slot.id) ? 1 : 0;
  if (lit.browse && slot.pkgId === lit.browse) return 1;
  return 0;
}

function refreshInstances() {
  if (!solidMesh) return;
  let changed = false;
  solidMesh.userData.slots.forEach((slot, index) => {
    if (writeSlot(solidMesh, index, slot, slotOpen(slot))) changed = true;
  });
  if (changed) solidMesh.instanceMatrix.needsUpdate = true;
}

function selectPackage(id, fly) {
  selected = { kind: "package", id };
  entered = fly ? id : entered;
  entitySubject = null;
  arcSubject = { id };
  applyMode();
  if (fly) flyToPackage(id);
  noteJump();
}

// A package outside the tree has no dependencies of its own to show, so it
// opens on the packages that depend on it. `c` flips it afterwards.
function selectExternal(id, inbound = true) {
  callInbound = inbound;
  selected = { kind: "external", id };
  entered = null;
  entitySubject = null;
  arcSubject = { id };
  applyMode();
  const ext = laid.externals.find((item) => item.id === id);
  if (ext) flyTo(new THREE.Vector3(ext.x + 8, ext.y + 7, ext.z + 10), new THREE.Vector3(ext.x, ext.y, ext.z));
  noteJump();
}

function selectEntity(id) {
  const found = byEntity.get(id);
  if (!found || !found.box) return;
  // A type's useful direction is who reaches into it: it has no calls of its
  // own, so opening on that side would show an empty list.
  if (isType(found)) callInbound = true;
  // A function is here for its calls: selecting one opens the call diff
  // straight away, on the side calls or callers is set to, instead of asking
  // for a second click.
  // Escape leaves the diff and the node with it.
  if (found.entity.kind === "function" || found.entity.kind === "method") {
    enterFocus(found);
    return;
  }
  selected = { kind: "entity", id };
  entered = found.pkg.id;
  entitySubject = id;
  arcSubject = null;
  applyMode();
  const points = (linkPoints && linkPoints.length ? linkPoints : [entityAnchor(found)]).concat(entityExtent(found));
  const pose = framePose(points);
  flyTo(pose.pos, pose.target);
  noteJump();
}

function enterFocus(found) {
  focus = found;
  selected = { kind: "entity", id: found.entity.id };
  entered = found.pkg.id;
  entitySubject = found.entity.id;
  arcSubject = null;
  applyMode();
  const cam = focusCamera();
  flyTo(cam.pos, cam.target);
  noteJump();
}

function dropFocus() {
  focus = null;
  clearGroup(focusGroup);
  selectArcs.visible = true;
  arcGroup.visible = mode === "overlay";
}

function circlePositions(segments) {
  const geom = new THREE.BufferGeometry();
  const positions = [];
  for (let i = 0; i < segments; i++) {
    const angle = (i / segments) * Math.PI * 2;
    positions.push(Math.cos(angle), 0, Math.sin(angle));
  }
  geom.setAttribute("position", new THREE.Float32BufferAttribute(positions, 3));
  return geom;
}

function placeRing(id) {
  const box = laid.packages.find((item) => item.id === id);
  if (!box) {
    ring.visible = false;
    return;
  }
  const radius = Math.max(box.w, box.d) * 0.48;
  ring.position.set(box.x + box.w / 2, box.y + box.h + 0.18, box.z + box.d / 2);
  ring.scale.set(radius, 1, radius);
  ring.visible = true;
}

function linkColor(change, std) {
  if (mode === "overlay" && change && change !== "same") return changeColor(change);
  if (std) return paint.std;
  return paint.call;
}

function drawSelectionArcs(id, inbound) {
  clearGroup(selectArcs);
  // The whole fan meets at the centre of the top of the selected package.
  const hub = packageAnchor(id);
  if (!hub) return;
  const edges = packageEdges(id, inbound);
  for (const edge of edges) {
    const farId = inbound ? edge.from : edge.to;
    const far = packageAnchor(farId);
    if (!far) continue;
    const from = inbound ? far : hub;
    const to = inbound ? hub : far;
    const color = linkColor(edge.change, isStdPackage(edge.to));
    const target = byPackage.get(farId);
    const fromId = inbound ? farId : id;
    const toId = inbound ? id : farId;
    addArc(selectArcs, from, to, color, clearLift(from, to, [fromId, toId]), {
      kind: "dep",
      id: farId,
      label: (target && (target.name || target.id)) || farId,
      external: !!(target && target.external),
      from: fromId,
      to: toId,
    }, fromId, toId);
  }
}

function flyToPackage(id) {
  const origin = packageAnchor(id);
  if (!origin) return;
  const points = [origin];
  if (lit && lit.links && lit.links.length) {
    for (const link of lit.links) {
      const from = towerTop(link.from);
      const to = towerTop(link.target);
      if (from) points.push(from);
      if (to) points.push(to);
    }
  } else {
    const pkg = byPackage.get(id);
    for (const dep of (pkg && pkg.deps) || []) {
      if (dep.external) continue;
      if (mode !== "overlay" && dep.change === "removed") continue;
      const at = packageAnchor(dep.to);
      if (at) points.push(at);
    }
  }
  const box = laid.packages.find((item) => item.id === id);
  if (box) {
    points.push([box.x, box.y + box.h, box.z]);
    points.push([box.x + box.w, box.y + box.h, box.z + box.d]);
    points.push([box.x, box.y, box.z + box.d], [box.x + box.w, box.y, box.z]);
  }
  // The bow crowns above the higher end. Keep it inside the frame with the landings.
  let top = origin[1];
  for (const p of points) top = Math.max(top, p[1]);
  points.push([origin[0], top + 14, origin[2]]);
  const pose = frameFan(points);
  flyTo(pose.pos, pose.target);
}

function frameFan(points) {
  return fitTo(points);
}

// framePose fits the camera to every point, plus the bases of the towers
// and the crowns of the arcs the current links draw, so the whole subject
// is in view from roof to ground.
function framePose(points) {
  return fitTo(points.concat(linkFrame));
}

function flyTo(pos, target) {
  tween = {
    fromPos: camera.position.clone(),
    fromTarget: controls.target.clone(),
    toPos: pos.clone(),
    toTarget: target.clone(),
    t0: performance.now(),
    ms: 880,
  };
}

function tickTween(now) {
  if (!tween) return false;
  const k = Math.min(1, (now - tween.t0) / tween.ms);
  const s = k * k * (3 - 2 * k);
  camera.position.lerpVectors(tween.fromPos, tween.toPos, s);
  controls.target.lerpVectors(tween.fromTarget, tween.toTarget, s);
  if (k === 1) {
    const done = tween.done;
    tween = null;
    if (done) done();
  }
  return true;
}

function strongerChange(a, b) {
  const rank = { same: 0, modified: 1, removed: 2, added: 3 };
  return (rank[a] || 0) >= (rank[b] || 0) ? a : b;
}

function buildFocus() {
  const found = focus;
  if (!found) {
    clearGroup(focusGroup);
    focusGroup.userData.points = null;
    return;
  }
  drawEntityLinks(focusGroup, found);
  // The ghost of the old body sits on the tower itself (#17). The call arcs
  // are drawn from whichever end the direction picks.
  addBodyBars(found.entity, mode === "overlay");
  focus.points = linkPoints ? linkPoints.slice() : [];
}

// The focused tower already is the function. The old body of a changed or
// deleted function is a ghost around it, on the same base: never an extra
// building, and never off its roof.
function addBodyBars(entity, overlay) {
  const slot = slotById.get(entity.id);
  const ghost = oldBodyBox(slot ? visualBox(slot, slotOpen(slot)) : null, entity, overlay);
  if (!ghost) return;
  const mesh = new THREE.Mesh(
    new THREE.BoxGeometry(ghost.w, ghost.h, ghost.d),
    new THREE.MeshBasicMaterial({ color: paint.removed, transparent: true, opacity: 0.28, depthWrite: false }),
  );
  mesh.position.set(ghost.x + ghost.w / 2, ghost.y + ghost.h / 2, ghost.z + ghost.d / 2);
  mesh.userData.oldBody = true;
  // A skin change repaints the ghost from the same tint the arcs use.
  mesh.userData.tint = { kind: "bar", change: "removed" };
  focusGroup.add(mesh);
}

function focusCamera() {
  const points = ((focus && focus.points) || []).concat(entityExtent(focus));
  return framePose(points.length ? points : [[0, 0, 0]]);
}

function clearGroup(group) {
  let dropped = false;
  for (const child of [...group.children]) {
    child.traverse((obj) => {
      if (obj.userData && obj.userData.flow) {
        const index = flows.indexOf(obj);
        if (index >= 0) flows.splice(index, 1);
        dropped = true;
      }
      if (obj.geometry) obj.geometry.dispose();
      if (obj.material) {
        const list = Array.isArray(obj.material) ? obj.material : [obj.material];
        for (const mat of list) {
          if (mat === flowMaterial || mat === haloMaterial) continue;
          if (mat.map) mat.map.dispose();
          // The material itself is left to the garbage collector, not
          // disposed: disposing the last user of a shader program deletes
          // the program, and the next selection compiles it again. On a
          // phone that compile is a visible stall on every select.
        }
      }
    });
    group.remove(child);
  }
  if (dropped) syncFlowTexture();
}

function hitTest() {
  raycaster.setFromCamera(pointer, camera);
  const objects = [];
  const pushLinks = (root) => {
    if (!root.visible) return;
    root.traverse((obj) => {
      if (obj.isMesh && !obj.isInstancedMesh) objects.push(obj);
    });
  };
  if (focus) pushLinks(focusGroup);
  else {
    pushLinks(selectArcs);
    pushLinks(arcGroup);
  }
  if (solidMesh && solidMesh.visible) objects.push(solidMesh);
  for (const plinth of plinths) {
    if (plinth.mesh.visible) objects.push(plinth.mesh);
  }
  const hits = raycaster.intersectObjects(objects, false);
  return hits[0] || null;
}

function linkPayload(obj) {
  let node = obj;
  while (node) {
    const data = node.userData || {};
    if (data.kind === "call" || data.kind === "dep" || data.kind === "package" || data.kind === "external") return data;
    node = node.parent;
  }
  return {};
}

function describeHit(hit) {
  if (!hit) return null;
  const data = linkPayload(hit.object);
  if (data.kind === "package" || data.kind === "external") return { kind: data.kind, id: data.id, point: hit.point };
  if (data.kind === "call") {
    return { kind: "call", entityId: data.entityId, step: data.step, label: data.label, point: hit.point };
  }
  if (data.kind === "dep") {
    return { kind: "dep", id: data.id, label: data.label, external: !!data.external, point: hit.point };
  }
  if (hit.instanceId != null && hit.object.userData.slots) {
    const slot = hit.object.userData.slots[hit.instanceId];
    return slot ? { kind: "entity", id: slot.id, point: hit.point } : null;
  }
  return null;
}

function onHover(hit) {
  if (skinPicking || idleSpin) {
    hud.tag.style.display = "none";
    renderer.domElement.style.cursor = "";
    return;
  }
  const found = describeHit(hit);
  if (!found) {
    hud.tag.style.display = "none";
    renderer.domElement.style.cursor = "";
    return;
  }
  if (found.kind === "call") {
    showTag(found.point, entityInfo(found.entityId) || found.label || "call");
    return;
  }
  if (found.kind === "dep") {
    showTag(found.point, packageInfo(found.id));
    return;
  }
  const pkg = byPackage.get(found.id);
  const entity = byEntity.get(found.id);
  const label = entity ? entityLabel(entity.entity) : (pkg ? pkg.name || pkg.id : found.id);
  showTag(found.point, label);
}

function showTag(point, text) {
  scratch.proj.copy(point).project(camera);
  if (scratch.proj.z > 1) {
    hud.tag.style.display = "none";
    return;
  }
  hud.tag.style.display = "block";
  const rect = renderer.domElement.getBoundingClientRect();
  hud.tag.style.left = (rect.left + (scratch.proj.x * 0.5 + 0.5) * rect.width) + "px";
  hud.tag.style.top = (rect.top + (-scratch.proj.y * 0.5 + 0.5) * rect.height) + "px";
  hud.tag.textContent = text;
  renderer.domElement.style.cursor = "pointer";
}

function activate(found) {
  if (!found) return;
  clearTourMarks();
  if (found.kind === "call") {
    const target = found.entityId ? byEntity.get(found.entityId) : null;
    if (!target) return;
    focus = null;
    clearGroup(focusGroup);
    if (target.entity.kind === "function" || target.entity.kind === "method") enterFocus(target);
    else {
      selectEntity(target.entity.id);
      applyMode();
    }
    return;
  }
  if (found.kind === "dep") {
    dropFocus();
    const pkg = byPackage.get(found.id);
    if (!pkg) return;
    if (pkg.external) selectExternal(found.id);
    else selectPackage(found.id, true);
    applyMode();
    return;
  }
  // A click on another node leaves the call diff for it, the same as picking
  // it in the sidebar: no Escape first.
  if (focus && !(found.kind === "entity" && found.id === focus.entity.id)) dropFocus();
  else if (focus) return;
  if (found.kind === "external") {
    selectExternal(found.id);
    return;
  }
  if (found.kind === "package") {
    selectPackage(found.id, true);
    return;
  }
  if (found.kind === "entity") selectEntity(found.id);
}

function entityLabel(entity) {
  if (entity.kind === "method" && entity.recv) return entity.recv + "." + entity.name;
  return entity.name || entity.kind;
}

function packageInfo(id) {
  const pkg = byPackage.get(id);
  if (!pkg) return id || "";
  const name = pkg.name || pkg.id;
  if (isStdPackage(id)) return name + "  ·  std";
  if (pkg.id && pkg.id !== name) return name + "  ·  " + pkg.id;
  return name;
}

function entityInfo(id) {
  const found = byEntity.get(id);
  if (!found) return "";
  const label = entityLabel(found.entity);
  const kind = found.entity.kind || "";
  const pkg = found.pkg && (found.pkg.id || found.pkg.name);
  const head = [kind, label].filter(Boolean).join(" ");
  return pkg ? head + "  ·  " + pkg : head;
}

function updateHUD() {
  const root = sceneDoc && (sceneDoc.module || sceneDoc.root || "project");
  hud.title.textContent = root;
  hud.overview.classList.toggle("on", mode === "overview");
  hud.changes.classList.toggle("on", mode === "overlay");
  let note = "";
  if (sceneDoc && !sceneDoc.diff) {
    if (mode === "overlay") note = "This view is one snapshot. Pass -range to lay a diff on the city.";
  }
  if (jumps.length > 1 && !jumpDetached) {
    const at = "jump " + (jumpIndex + 1) + "/" + jumps.length;
    note = note ? note + "  ·  " + at : at;
  }
  if (skinWarning) note = note ? skinWarning + "  ·  " + note : skinWarning;
  hud.note.textContent = note;
  renderDetail();
}

function renderDetail() {
  stopMarches();
  const subject = subjectKey();
  if (subject !== detailSubject) {
    detailSubject = subject;
    sectionsTouched = false;
    openSections.clear();
  }
  hud.detailHead.replaceChildren();
  hud.detailBody.replaceChildren();
  if (focus) {
    hud.detailHead.append(focusDetail(focus.entity));
    hud.detailBody.append(roster(focus.pkg));
    return;
  }
  if (selected && selected.kind === "entity") {
    const found = byEntity.get(selected.id);
    if (found) {
      hud.detailHead.append(entityDetail(found.entity));
      hud.detailBody.append(roster(found.pkg));
    }
    return;
  }
  const id = (selected && selected.id) || (sceneDoc && sceneDoc.root);
  const pkg = id ? byPackage.get(id) : null;
  if (pkg) {
    hud.detailHead.append(packageDetail(pkg));
    hud.detailBody.append(roster(pkg));
    return;
  }
  hud.detailHead.append(noRootDetail());
  hud.detailBody.append(noRootRoster());
}

// A range whose two sides name different modules — a rename, or several
// modules — has no single root package. The diff is still there to read, so
// the sidebar opens on the whole of it: the range's modules, what changed
// under each, and the same drill-down the root package would have given.
function noRootDetail() {
  const wrap = document.createElement("div");
  const before = (sceneDoc && sceneDoc.moduleBefore) || "";
  const after = (sceneDoc && sceneDoc.moduleAfter) || "";
  const modules = [...new Set([...splitModules(before), ...splitModules(after)])];
  const label = before && after && before !== after ? before + " \u2192 " + after : (after || before || "this range");
  const title = document.createElement("h2");
  const titleClip = clipText(label);
  title.append(titleClip);
  attachMarquee(title, titleClip);
  wrap.append(title);
  const meta = document.createElement("p");
  meta.textContent = modules.length + (modules.length === 1 ? " module" : " modules");
  wrap.append(meta);
  wrap.append(changeTally(allDeclaredEntities()));
  return wrap;
}

function splitModules(names) {
  return String(names || "").split(",").map((name) => name.trim()).filter(Boolean);
}

function allDeclaredEntities() {
  const out = [];
  for (const pkg of sceneDoc.packages || []) {
    if (pkg.external) continue;
    out.push(...declaredEntities(pkg));
  }
  return out;
}

// Every internal package at the top of the city: a module root, or a package
// whose parent is not in the snapshot.
function topPackages() {
  const internals = (sceneDoc.packages || []).filter((pkg) => !pkg.external);
  const ids = new Set(internals.map((pkg) => pkg.id));
  return internals.filter((pkg) => !pkg.parent || !ids.has(pkg.parent));
}

// The tally the whole range carries, counted like a package's own: every
// declaration and every call to a declaration in the snapshot.
function cityTally() {
  const tally = { added: 0, modified: 0, removed: 0 };
  for (const pkg of topPackages()) {
    const below = subtreeStats(pkg);
    tally.added += below.added;
    tally.modified += below.modified;
    tally.removed += below.removed;
  }
  return tally;
}

// The body of the no-root view: one Packages expander over the whole city. The
// changed packages come first at any depth, the unchanged modules fold under
// them, so the range can be read from the top and walked into.
function noRootRoster() {
  const wrap = document.createElement("div");
  wrap.className = "sections";
  const roots = topPackages();
  if (!roots.length) return wrap;
  const byName = (a, b) => packageLabel(a).localeCompare(packageLabel(b));
  const direct = splitChanges(roots, (pkg) => pkg.change);
  const nested = changedInSubtree({ id: undefined }).filter((pkg) => pkg.parent && !roots.some((root) => root.id === pkg.id));
  const changed = [...direct.hot, ...nested].sort(byName);
  const section = {
    key: "packages",
    label: "Packages",
    hot: mode === "overlay" ? deletedFirst(changed) : roots.slice().sort(byName),
    same: mode === "overlay" ? direct.same.slice().sort(byName) : [],
    row: (pkg) => packageRow(pkg, ""),
    totals: () => cityTally(),
  };
  wrap.append(sectionBlock(section, !sectionsTouched));
  return wrap;
}

function packageDetail(pkg) {
  const wrap = document.createElement("div");
  const title = document.createElement("h2");
  const titleClip = clipText(pkg.name || pkg.id);
  title.append(titleClip);
  attachMarquee(title, titleClip);
  wrap.append(title);
  const meta = document.createElement("p");
  meta.append(pkg.id);
  const mark = changeMark(pkg.change);
  if (mark) {
    meta.append(" · ");
    meta.append(mark);
  }
  wrap.append(meta);
  wrap.append(changeTally(declaredEntities(pkg)));
  // The change is real but sits below the declarations: importing, re-exporting,
  // a comment, a reformat. The city draws it muted; the panel says why.
  if (isLowLevel(pkg)) {
    const line = document.createElement("p");
    line.className = "low-level";
    line.textContent = "low level code change";
    line.title = "The file changed but no declaration did: imports, re-exports, comments, formatting.";
    wrap.append(line);
  }
  const deps = pkg.deps || [];
  if (deps.length && mode === "overlay") {
    const added = deps.filter((dep) => dep.change === "added").length;
    const removed = deps.filter((dep) => dep.change === "removed").length;
    const line = document.createElement("p");
    line.append("dependencies ");
    const plus = document.createElement("span");
    plus.className = "added";
    plus.textContent = "+" + added;
    const minus = document.createElement("span");
    minus.className = "removed";
    minus.textContent = " −" + removed;
    line.append(plus, minus);
    wrap.append(line);
  } else if (deps.length) {
    const line = document.createElement("p");
    line.textContent = deps.length + " dependencies";
    wrap.append(line);
  }
  appendRefs(wrap);
  return wrap;
}

function declaredEntities(pkg) {
  return (pkg.entities || []).filter((entity) => entity.kind === "type" || entity.kind === "variable" || entity.kind === "function" || entity.kind === "method");
}

function changeTally(entities) {
  const line = document.createElement("p");
  line.className = "tally";
  line.append(entities.length + " declarations");
  if (mode !== "overlay") return line;
  const added = entities.filter((entity) => entity.change === "added").length;
  const removed = entities.filter((entity) => entity.change === "removed").length;
  const modified = entities.filter((entity) => entity.change === "modified").length;
  if (added) line.append(tallySpan("added", "+" + added, added + " added"));
  if (removed) line.append(tallySpan("removed", "\u2212" + removed, removed + " deleted"));
  if (modified) line.append(tallySpan("modified", "~" + modified, modified + " changed"));
  return line;
}

function tallySpan(kind, text, title) {
  const span = document.createElement("span");
  span.className = kind;
  span.textContent = text;
  if (title) span.title = title;
  return span;
}

function splitChanges(list, changeOf) {
  const hot = [];
  const same = [];
  for (const item of list) {
    const change = changeOf(item);
    if (mode === "overlay" && (!change || change === "same")) same.push(item);
    else hot.push(item);
  }
  return { hot, same };
}

function childPackages(id) {
  return (sceneDoc.packages || []).filter((item) => !item.external && item.parent === id);
}

function packageTouched(pkg) {
  if (!pkg) return false;
  if (pkg.change && pkg.change !== "same") return true;
  return declaredEntities(pkg).some((entity) => entity.change && entity.change !== "same");
}

function changedInSubtree(pkg) {
  const out = [];
  for (const child of childPackages(pkg.id)) {
    if (packageTouched(child)) out.push(child);
    out.push(...changedInSubtree(child));
  }
  return out;
}

// The children of a node, grouped by kind. Each group is one collapsible
// section with git's four numbers on its header: what the range added,
// changed, deleted and left alone.
// One entry per node at the other end of the arcs: the calls the selected node
// makes, or the calls it takes, depending on the direction. Clicking an entry
// is the same as clicking the arc — it selects that node.
function refRow(node, change) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row";
  if (selected && selected.id === node.entity.id) button.classList.add("on");
  const name = clipText(entityLabel(node.entity));
  button.append(name);
  attachMarquee(button, name);
  const where = document.createElement("span");
  where.className = "where";
  where.textContent = node.pkg ? (node.pkg.name || node.pkg.id) : "";
  button.append(where);
  const mark = changeMark(change);
  if (mark) {
    mark.classList.add("mark");
    button.append(mark);
  }
  button.addEventListener("click", () => openEntity(node.entity.id));
  return button;
}

// The same node can be reached by several calls; the list shows it once, with
// the strongest change among them.
function mergeByNode(entries) {
  const byNode = new Map();
  for (const entry of entries) {
    const id = entry.node.entity.id;
    const prev = byNode.get(id);
    if (!prev) byNode.set(id, { node: entry.node, change: entry.change });
    else prev.change = strongerChange(prev.change, entry.change);
  }
  return [...byNode.values()];
}

function refsSection() {
  const subject = subjectNode();
  if (!subject) return null;
  let entries = [];
  if (subject.kind === "entity") {
    const found = byEntity.get(subject.id);
    if (!found || !found.box || !hasDirection(found)) return null;
    entries = subjectLinks(found).map((link) => ({ node: link.target, change: link.change }));
  } else {
    const links = callInbound ? packageCallerLinks(subject.id) : packageCallLinks(subject.id);
    entries = links.map((link) => ({ node: callInbound ? link.from : link.target, change: link.change }));
  }
  const merged = mergeByNode(entries);
  if (!merged.length) return null;
  return {
    key: "refs",
    label: callInbound ? "Callers" : "Calls",
    hot: deletedFirst(merged.filter((entry) => entry.change !== "same")),
    same: merged.filter((entry) => entry.change === "same"),
    row: (entry) => refRow(entry.node, entry.change),
  };
}

function roster(pkg) {
  const wrap = document.createElement("div");
  wrap.className = "sections";
  const refs = refsSection();
  if (!pkg) {
    if (refs) wrap.append(sectionBlock(refs, !sectionsTouched));
    return wrap;
  }
  const sections = rosterSections(pkg).filter((section) => section.hot.length || section.same.length);
  // The references come first, so they sit right under the Calls / Callers
  // buttons and are the section that is open to start from.
  if (refs) sections.unshift(refs);
  // One section open to start from, the rest folded: the counts on the folded
  // headers say what they hold. Once the user has folded or unfolded something
  // for this node, their choice is what stands.
  const first = sections.length ? sections[0].key : "";
  for (const section of sections) {
    const open = sectionsTouched ? openSections.has(section.key) : section.key === first;
    wrap.append(sectionBlock(section, open));
  }
  return wrap;
}

function rosterSections(pkg) {
  const children = childPackages(pkg.id);
  children.sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id));
  const direct = splitChanges(children, (item) => item.change);
  const nested = changedInSubtree(pkg).filter((item) => item.parent !== pkg.id);
  const packageList = [...direct.hot, ...nested];
  packageList.sort((a, b) => rowLabel(a, pkg.id).localeCompare(rowLabel(b, pkg.id)));
  const sections = [{
    key: "packages",
    label: "Packages",
    hot: mode === "overlay" ? deletedFirst(packageList) : children,
    same: mode === "overlay" ? direct.same : [],
    row: (item) => packageRow(item, pkg.id),
    totals: () => statsBelow(pkg),
  }];
  const groups = [
    ["types", "Types", "type"],
    ["variables", "Variables", "variable"],
    ["functions", "Functions", "function"],
    ["methods", "Methods", "method"],
  ];
  for (const [key, label, kind] of groups) {
    const list = declaredEntities(pkg).filter((entity) => entity.kind === kind);
    list.sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)));
    const split = splitChanges(list, (item) => item.change);
    sections.push({ key, label, hot: deletedFirst(split.hot), same: split.same, row: entityRow });
  }
  return sections;
}

// What the user has folded for the node on screen. Selecting another node
// starts from the default again: the first section open, the rest folded.
const openSections = new Set();
let sectionsTouched = false;
let detailSubject = "";

// The node the panel is showing, so a change of node resets the folds.
function subjectKey() {
  if (focus) return "focus:" + focus.entity.id;
  if (selected) return selected.kind + ":" + selected.id;
  return "root";
}

function sectionBlock(section, open) {
  const wrap = document.createElement("section");
  wrap.className = open ? "section open" : "section";
  // The key names the section for the jump list, which restores its scroll.
  wrap.dataset.section = section.key;
  const head = document.createElement("button");
  head.type = "button";
  head.className = "section-head";
  head.setAttribute("aria-expanded", wrap.classList.contains("open") ? "true" : "false");
  const caret = document.createElement("span");
  caret.className = "section-caret";
  caret.innerHTML = '<svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true"><path d="M6 3 L11 8 L6 13" fill="none" stroke="currentColor" stroke-width="1.6"/></svg>';
  const label = document.createElement("span");
  label.className = "section-label";
  label.textContent = section.label;
  const counts = document.createElement("span");
  counts.className = "section-counts";
  for (const [kind, text] of sectionCounts(section)) {
    const span = document.createElement("span");
    span.className = kind;
    span.textContent = text;
    counts.append(span);
  }
  head.append(caret, label, counts);
  head.addEventListener("click", () => {
    const next = !wrap.classList.contains("open");
    setSectionOpen(wrap, head, section.key, next);
  });
  const body = document.createElement("div");
  body.className = "section-body";
  body.inert = !open;
  for (const item of [...section.hot, ...section.same]) body.append(section.row(item));
  wrap.append(head, body);
  return wrap;
}

// A folded section is out of the tab order: its rows are clipped, and Tab
// should reach the next section's header, not walk through rows nobody can see.
function setSectionOpen(wrap, head, key, open) {
  wrap.classList.toggle("open", open);
  head.setAttribute("aria-expanded", open ? "true" : "false");
  const body = wrap.querySelector(".section-body");
  if (body) body.inert = !open;
  sectionsTouched = true;
  if (open) openSections.add(key);
  else openSections.delete(key);
  syncJumpSidebar();
}

// A jump restores the sidebar, not only the node: which sections were open and
// how far their lists were scrolled are part of where the user was. The folds
// come from the state above, the offsets from the DOM, so what is stored is
// what is on screen.
function sidebarState() {
  const state = { open: [...openSections], touched: sectionsTouched, scroll: hud.detailBody.scrollTop, bodies: {} };
  for (const wrap of hud.detailBody.querySelectorAll(".section.open")) {
    const body = wrap.querySelector(".section-body");
    if (body && wrap.dataset.section) state.bodies[wrap.dataset.section] = body.scrollTop;
  }
  return state;
}

// The folds and scrolls of the node on screen belong to its jump entry, so a
// toggle or a scroll writes them back right away: the entry stays what the
// user last left there, ready for the next `o`.
function syncJumpSidebar() {
  if (jumping || jumpIndex < 0 || jumpIndex >= jumps.length) return;
  jumps[jumpIndex].sidebar = sidebarState();
}

// added, changed, deleted, untouched — the untouched count is the quiet one.
//
// Outside Changes mode there is no diff on screen to count, and the rows carry
// no marks either, so the header says how many children there are and nothing
// more: the same thing the node's own tally line above it does.
function sectionCounts(section) {
  const items = [...section.hot, ...section.same];
  if (mode !== "overlay") return [["total", String(items.length)]];
  const tally = { added: 0, modified: 0, removed: 0, same: 0 };
  if (section.totals) {
    // The Packages expander holds packages, and a package holds more than its
    // own mark: its header counts every declaration and call inside it, so the
    // number on the fold is the size of what unfolds.
    const totals = section.totals();
    tally.added = totals.added;
    tally.modified = totals.modified;
    tally.removed = totals.removed;
  } else {
    for (const item of items) {
      const change = item.change;
      tally[change in tally ? change : "same"] += 1;
    }
  }
  const out = [];
  if (tally.added) out.push(["added", "+" + tally.added]);
  if (tally.modified) out.push(["modified", "~" + tally.modified]);
  if (tally.removed) out.push(["removed", "\u2212" + tally.removed]);
  if (tally.same) out.push(["same", String(tally.same)]);
  return out;
}

function packageLabel(pkg) {
  const name = pkg.name || pkg.id;
  const same = (sceneDoc.packages || []).filter((item) => (item.name || item.id) === name && !item.external);
  if (same.length < 2) return name;
  const parts = pkg.id.split("/");
  return parts.slice(-2).join("/");
}

function rowLabel(pkg, ownerId) {
  if (ownerId && pkg.parent !== ownerId && pkg.id.startsWith(ownerId + "/")) return pkg.id.slice(ownerId.length + 1);
  return packageLabel(pkg);
}

// What changed inside a package: its own declarations, and the calls they make
// to declarations this codebase has — the ones the viewer can draw and name.
// Counted per call step, not per target: a function that lost a call to a
// target and gained another has both, and folding them by target (which is what
// the arcs do) hid the loss behind the gain.
// A package row is the way into that package, so it counts what you find when
// you get there: its own declarations and calls, plus everything in the
// packages below it. Counting leaves rather than packages is what makes an
// expander's totals add up to what is inside it, however deep the tree goes.
const subtreeCache = new Map();

function subtreeStats(pkg) {
  const hit = subtreeCache.get(pkg.id);
  if (hit) return hit;
  const tally = packageStats(pkg);
  for (const child of childPackages(pkg.id)) {
    const below = subtreeStats(child);
    tally.added += below.added;
    tally.modified += below.modified;
    tally.removed += below.removed;
  }
  subtreeCache.set(pkg.id, tally);
  return tally;
}

// What is below this package and not inside it: the Types, Functions and
// Methods sections count the package's own declarations already, and the
// Packages expander would count them a second time.
function statsBelow(pkg) {
  const tally = { added: 0, modified: 0, removed: 0 };
  for (const child of childPackages(pkg.id)) {
    const below = subtreeStats(child);
    tally.added += below.added;
    tally.modified += below.modified;
    tally.removed += below.removed;
  }
  return tally;
}

function packageStats(pkg) {
  const tally = { added: 0, modified: 0, removed: 0 };
  const bump = (change) => {
    if (tally[change] != null) tally[change] += 1;
  };
  for (const entity of declaredEntities(pkg)) {
    bump(entity.change);
    for (const step of entity.calls || []) {
      if (!declaredTarget(step)) continue;
      bump(step.change);
    }
  }
  return tally;
}

function packageRow(pkg, ownerId) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row";
  if (selected && selected.id === pkg.id) button.classList.add("on");
  const name = clipText(rowLabel(pkg, ownerId));
  button.append(name);
  attachMarquee(button, name);
  const tally = subtreeStats(pkg);
  const parts = [
    ["added", "+", tally.added, "added"],
    ["modified", "~", tally.modified, "changed"],
    ["removed", "\u2212", tally.removed, "deleted"],
  ].filter(([, , count]) => count > 0);
  // Overview shows the city, not the diff: no counts, no marks, nothing that
  // only means something next to a change.
  const low = isLowLevel(pkg);
  if ((parts.length || low) && mode === "overlay") {
    const mark = document.createElement("span");
    mark.className = "mark stats";
    const summary = parts.map(([, , count, word]) => count + " " + word).join(", ");
    if (low) mark.title = [summary, "low level code change"].filter(Boolean).join("; ");
    else mark.title = summary;
    for (const [kind, glyph, count] of parts) {
      const span = document.createElement("span");
      span.className = kind;
      span.textContent = glyph + count;
      mark.append(span);
    }
    if (low) {
      const hint = document.createElement("span");
      hint.className = "low";
      hint.textContent = "low level";
      mark.append(hint);
    }
    button.append(mark);
  }
  button.addEventListener("click", () => openPackage(pkg.id));
  return button;
}

function entityRow(entity) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row";
  if ((focus && focus.entity.id === entity.id) || (selected && selected.id === entity.id)) button.classList.add("on");
  const name = clipText(entityLabel(entity));
  button.append(name);
  attachMarquee(button, name);
  const named = entity.kind === "function" || entity.kind === "method";
  const mark = changeMark(entity.change, entity.part, named);
  if (mark) {
    mark.classList.add("mark");
    button.append(mark);
  }
  button.addEventListener("click", () => openEntity(entity.id));
  return button;
}

function openPackage(id) {
  if (focus) dropFocus();
  selectPackage(id, true);
}

function openEntity(id) {
  const found = byEntity.get(id);
  if (!found || !found.box) return;
  if (focus && focus.entity.id !== id) dropFocus();
  selectEntity(id);
}

// The buttons are pointless when there is nothing behind them: a package with
// no declarations has nothing to call, and a type with no methods is never
// called through one. The panel says "0 declarations" in the package case.
function hasRefs(subject) {
  if (!subject) return false;
  if (subject.kind === "entity") {
    const found = byEntity.get(subject.id);
    if (!found) return false;
    if (isType(found)) return typeCallersOutside(found).length > 0;
    return true;
  }
  const pkg = byPackage.get(subject.id);
  return !!pkg && declaredEntities(pkg).length > 0;
}

// The direction control. It sits with the subject's own details and acts on
// whatever the panel is showing, so there is one control and one state.
function refsControl() {
  if (!hasRefs(subjectNode())) return null;
  const wrap = document.createElement("div");
  wrap.className = "modes refs";
  wrap.setAttribute("role", "radiogroup");
  wrap.setAttribute("aria-label", "Show calls or callers");
  // A type has no calls of its own to show, so it gets the one direction.
  const subject = subjectNode();
  const found = subject && subject.kind === "entity" ? byEntity.get(subject.id) : null;
  const directions = found && isType(found) ? [[true, "Callers"]] : [[false, "Calls"], [true, "Callers"]];
  for (const [inbound, label] of directions) {
    const button = document.createElement("button");
    button.type = "button";
    button.setAttribute("role", "radio");
    const on = callInbound === inbound;
    if (on) button.className = "on";
    button.setAttribute("aria-checked", on ? "true" : "false");
    button.textContent = label;
    button.addEventListener("click", () => setCallDirection(inbound, null));
    wrap.append(button);
  }
  return wrap;
}

function appendRefs(wrap) {
  const control = refsControl();
  if (control) wrap.append(control);
}

function entityDetail(entity) {
  const wrap = document.createElement("div");
  const title = document.createElement("h2");
  const titleClip = clipText(entityLabel(entity));
  title.append(titleClip);
  attachMarquee(title, titleClip);
  const named = entity.kind === "function" || entity.kind === "method";
  const mark = changeMark(entity.change, entity.part, named);
  if (mark) title.append(mark);
  wrap.append(title);
  const meta = document.createElement("p");
  meta.textContent = [entity.kind, entity.file].filter(Boolean).join(" · ");
  wrap.append(meta);
  if (entity.kind === "function" || entity.kind === "method") {
    const size = document.createElement("p");
    size.textContent = sizeText(entity);
    wrap.append(size);
    appendRefs(wrap);
  } else if (entity.kind === "type") {
    appendRefs(wrap);
  }
  if (entity.fields && entity.fields.length) {
    const line = document.createElement("p");
    line.textContent = "fields  " + entity.fields.join(", ");
    wrap.append(line);
  }
  return wrap;
}

function focusDetail(entity) {
  const wrap = document.createElement("div");
  const title = document.createElement("h2");
  const titleClip = clipText(entityLabel(entity));
  title.append(titleClip);
  attachMarquee(title, titleClip);
  const mark = changeMark(entity.change, entity.part, true);
  if (mark) title.append(mark);
  wrap.append(title);
  appendRefs(wrap);
  // The calls or the callers are the References section in the body, behind
  // its own header: the head keeps what the node is and how big it is, and
  // nothing is listed or counted twice.
  const meta = document.createElement("p");
  meta.append(sizeText(entity));
  wrap.append(meta);
  return wrap;
}

// Only a function or method has a body: a type or a variable would read
// "body 0 bytes" and the number would mean nothing.
function sizeText(entity) {
  if (entity.kind !== "function" && entity.kind !== "method") return "";
  const after = entity.change === "removed" ? 0 : (entity.bodyBytes || 0);
  if (mode === "overlay" && entity.bodyBytesBefore != null && entity.bodyBytesBefore !== after) {
    return "body  " + entity.bodyBytesBefore + " → " + after + " bytes";
  }
  return "body  " + (entity.bodyBytes || 0) + " bytes";
}

// changePart is the word for what changed in a function or method: the mark
// says "~ body" rather than leaving a colour to be decoded, and the tooltip
// says the whole sentence. A declaration that is not callable has no parts.
function changePart(change, part, named) {
  if (!named || change !== "modified") return "";
  if (part === "body") return "body";
  if (part === "signature") return "signature";
  if (part === "both") return "both";
  return "";
}

function modifiedClass(part) {
  if (part === "body" || part === "both") return "modified " + part;
  return "modified";
}

function modifiedWord(part, named) {
  if (!named) return "changed";
  if (part === "body") return "body changed";
  if (part === "signature") return "signature changed";
  if (part === "both") return "signature and body changed";
  return "changed";
}

// The word a mark stands for, for the hover text.
function changeWord(change, part, named) {
  if (change === "added") return "added";
  if (change === "removed") return "deleted";
  if (change === "moved") return "moved";
  return modifiedWord(part, named);
}

// A mark is one glyph: + added, ~ changed, − deleted, → moved. The colour
// carries the rest of it — three yellows for a changed declaration — and the
// words are on hover, where they do not have to fit beside a name.
const CHANGE_GLYPH = { added: "+", modified: "~", removed: "\u2212", moved: "\u2192" };

function changeMark(change, part, named) {
  if (!change || change === "same" || mode !== "overlay") return null;
  const span = document.createElement("span");
  if (change === "added") {
    span.className = "added";
  } else if (change === "removed") {
    span.className = "removed";
  } else if (change === "modified") {
    span.className = modifiedClass(part);
  } else if (change === "moved") {
    span.className = "moved";
  } else {
    return null;
  }
  const word = changePart(change, part, named);
  span.textContent = (CHANGE_GLYPH[change] || "~") + (word ? " " + word : "");
  span.title = changeWord(change, part, named);
  return span;
}

function resetView() {
  focus = null;
  clearGroup(focusGroup);
  entered = null;
  entitySubject = null;
  arcSubject = null;
  callInbound = false;
  selected = null;
  jumps = [];
  jumpIndex = -1;
  jumpDetached = false;
  ring.visible = false;
  applyMode();
  cityPose = frameCity();
  applyFitLimits(cityPose);
  flyTo(cityPose.pos, cityPose.target);
}

function goBack() {
  const leftNode = !!(selected || focus);
  goBackInner();
  if (leftNode && !selected && !focus && jumpIndex >= 0) {
    jumpDetached = true;
    updateHUD();
    return;
  }
  noteJump();
}

// Escape always lets go of the node: the call diff, the selection and the
// package it sits in, and flies back to the whole city.
function goBackInner() {
  const hadSelection = focus || selected || entered || entitySubject || arcSubject;
  if (focus) dropFocus();
  entered = null;
  entitySubject = null;
  arcSubject = null;
  selected = null;
  ring.visible = false;
  applyMode();
  if (!hadSelection) return;
  cityPose = frameCity();
  applyFitLimits(cityPose);
  flyTo(cityPose.pos, cityPose.target);
}

// The subject key an entry will have once it is restored, so the panel's own
// subject key can be put in step before the render that follows.
function entrySubjectKey(entry) {
  if (entry.focus) return "focus:" + entry.focus;
  if (entry.selected) return entry.selected.kind + ":" + entry.selected.id;
  return "root";
}

// Put a saved sidebar setup in place before the render that follows, and keep
// detailSubject in step so renderDetail does not clear the restored folds. The
// scroll offsets wait for the DOM that render builds.
function stageJumpSidebar(entry) {
  const state = entry.sidebar || null;
  sectionsTouched = !!(state && state.touched);
  openSections.clear();
  if (state) for (const key of state.open || []) openSections.add(key);
  detailSubject = entrySubjectKey(entry);
  pendingSidebarScroll = state;
}

// The sidebar body and each open section body scroll on their own, so both
// offsets go back once the render has rebuilt the panel.
function applySidebarScroll(state) {
  if (!state) return;
  hud.detailBody.scrollTop = state.scroll || 0;
  for (const wrap of hud.detailBody.querySelectorAll(".section.open")) {
    const top = wrap.dataset.section ? state.bodies[wrap.dataset.section] : null;
    const body = wrap.querySelector(".section-body");
    if (body && top != null) body.scrollTop = top;
  }
}

// Apply once now and once on the next frame: the flex sizes that decide how far
// a body can scroll settle a frame after the render, so the first pass can clamp
// to a smaller range than the second. A later jump cancels the pending frame.
function restoreSidebarScroll() {
  const state = pendingSidebarScroll;
  if (!state) return;
  applySidebarScroll(state);
  cancelAnimationFrame(sidebarScrollFrame);
  sidebarScrollFrame = requestAnimationFrame(() => applySidebarScroll(state));
}

// What a jump restores: the whole navigation state, not just the camera.
function subjectNow() {
  return {
    selected: selected ? { kind: selected.kind, id: selected.id } : null,
    entered: entered || null,
    entity: entitySubject || null,
    arc: arcSubject ? { id: arcSubject.id } : null,
    inbound: callInbound,
    focus: focus ? focus.entity.id : null,
  };
}

function sameSubject(a, b) {
  if (!!a.selected !== !!b.selected) return false;
  if (a.selected && (a.selected.kind !== b.selected.kind || a.selected.id !== b.selected.id)) return false;
  if (a.entered !== b.entered || a.entity !== b.entity || a.focus !== b.focus) return false;
  if (!!a.arc !== !!b.arc) return false;
  if (a.arc && a.arc.id !== b.arc.id) return false;
  if (!!a.inbound !== !!b.inbound) return false;
  return true;
}

// Record the node the user just landed on. Consecutive identical states
// collapse, and selecting after going back truncates the forward tail, the
// same way vim's jump list behaves. The bare city is not a node.
function noteJump() {
  if (jumping) return;
  const now = subjectNow();
  if (!now.selected && !now.focus) return;
  const current = jumps[jumpIndex];
  const wasDetached = jumpDetached;
  jumpDetached = false;
  if (current && sameSubject(current, now)) {
    if (wasDetached) {
      updateHUD();
      // The bare city reset the folds, so the entry follows what is on screen.
      current.sidebar = sidebarState();
    }
    return;
  }
  jumps = jumps.slice(0, jumpIndex + 1);
  jumps.push(now);
  jumpIndex = jumps.length - 1;
  updateHUD();
  // The node is on screen now, so its folds and scrolls are the entry's own.
  jumps[jumpIndex].sidebar = sidebarState();
}

// Put the state back and fly the camera to it, the same way the original
// selection did.
function restoreSubject(entry) {
  if (!entry) return;
  stageJumpSidebar(entry);
  if (focus) dropFocus();
  selected = entry.selected ? { kind: entry.selected.kind, id: entry.selected.id } : null;
  entered = entry.entered;
  entitySubject = entry.entity;
  arcSubject = entry.arc ? { id: entry.arc.id } : null;
  callInbound = !!entry.inbound;
  ring.visible = false;
  applyMode();
  if (entry.focus) {
    const found = byEntity.get(entry.focus);
    if (found && found.box) {
      enterFocus(found);
      return;
    }
  }
  if (!entry.selected) {
    cityPose = frameCity();
    applyFitLimits(cityPose);
    flyTo(cityPose.pos, cityPose.target);
    return;
  }
  if (entry.selected.kind === "package") {
    flyToPackage(entry.selected.id);
    return;
  }
  if (entry.selected.kind === "external") {
    const ext = laid.externals.find((item) => item.id === entry.selected.id);
    if (ext) flyTo(new THREE.Vector3(ext.x + 8, ext.y + 7, ext.z + 10), new THREE.Vector3(ext.x, ext.y, ext.z));
    return;
  }
  const found = byEntity.get(entry.selected.id);
  if (!found || !found.box) return;
  const points = (linkPoints && linkPoints.length ? linkPoints : [entityAnchor(found)]).concat(entityExtent(found));
  const pose = framePose(points);
  flyTo(pose.pos, pose.target);
}

// step is -1 for the older entry, +1 for the newer one.
function jumpStep(step) {
  if (!jumps.length) return;
  const next = jumpDetached ? jumpIndex + (step < 0 ? 0 : step) : jumpIndex + step;
  if (next < 0 || next >= jumps.length) return;
  jumpIndex = next;
  jumpDetached = false;
  jumping = true;
  try {
    restoreSubject(jumps[jumpIndex]);
  } finally {
    jumping = false;
  }
  updateHUD();
  restoreSidebarScroll();
}

function typingSearch() {
  return document.activeElement === hud.search;
}

function flyToken(key) {
  if (key === "+" || key === "=") return "+";
  if (key === "-" || key === "_") return "-";
  if (key === "ArrowUp") return "arrowup";
  if (key === "ArrowDown") return "arrowdown";
  if (key === "ArrowLeft") return "arrowleft";
  if (key === "ArrowRight") return "arrowright";
  if (key.length !== 1) return "";
  const lower = key.toLowerCase();
  return "wasdqe".includes(lower) ? lower : "";
}

function axis(positive, negative, keys = held) {
  const pos = positive.some((key) => keys.has(key));
  const neg = negative.some((key) => keys.has(key));
  return (pos ? 1 : 0) - (neg ? 1 : 0);
}

function flyCamera(dt, now) {
  if (skinPicking) {
    idleSpin = false;
    return false;
  }
  // A focused search box must not steer the camera, but it must not stop the
  // city either: the idle orbit is global, so only the keys are dropped.
  const keys = typingSearch() ? NO_KEYS : held;
  const forward = axis(["w", "arrowup"], ["s", "arrowdown"], keys);
  const strafe = axis(["d", "arrowright"], ["a", "arrowleft"], keys);
  const yaw = (keys.has("q") ? 1 : 0) - (keys.has("e") ? 1 : 0);
  const zoom = (keys.has("+") ? 1 : 0) - (keys.has("-") ? 1 : 0);
  const userMove = forward || strafe || yaw || zoom;
  if (userMove) endIntro();
  // Any focus counts: overview, a selected package, a call focus, or the search
  // box. Fifteen seconds after the last real input, the city turns again.
  const idleReady = !introSpin && lastActivity && now - lastActivity >= IDLE_SPIN_MS;
  idleSpin = !bird.on && !userMove && !tween && ((introSpin && !!cityPose) || idleReady);
  const yawRate = idleSpin ? IDLE_YAW : yaw * 1.4;
  if (!forward && !strafe && !yawRate && !zoom) return false;
  if (userMove) tween = null;
  camera.getWorldDirection(flyDir);
  const next = flyStep(camera.position, controls.target, flyDir, { forward, strafe, yaw: yawRate, zoom }, dt, {
    minDistance: controls.minDistance,
    maxDistance: controls.maxDistance,
  });
  camera.position.set(next.position.x, next.position.y, next.position.z);
  controls.target.set(next.target.x, next.target.y, next.target.z);
  return true;
}

function animate(now) {
  frameQueued = false;
  if (loading) return;
  // The editor covers the city: no frames, no halo, no idle orbit until it
  // closes, and the close starts the loop again.
  if (editorActive()) {
    clearTimeout(idleTimer);
    return;
  }
  const t = now || performance.now();
  const dt = lastFrame ? Math.min(0.05, Math.max(0, (t - lastFrame) / 1000)) : 0.016;
  lastFrame = t;
  if (!lastActivity) lastActivity = t;
  if (held.size && !typingSearch()) tween = null;
  const tweening = tickTween(t);
  const flying = flyCamera(dt, t);
  controls.update();
  const moved = tweening || flying ||
    camera.position.distanceToSquared(settledPos) > 1e-6 ||
    controls.target.distanceToSquared(settledTarget) > 1e-6;
  if (viewDirty && laid) refreshInstances();
  if (moved) {
    settledPos.copy(camera.position);
    settledTarget.copy(controls.target);
  }
  // Particles are decoration. They advance while the scene is being driven —
  // Decoration never stops: particles march and the halo travels whenever they
  // are on screen. It is cheap — one uniform write each, all of it on the GPU —
  // so the only cost is the frame itself, and a frame that carries nothing but
  // decoration is paced lower than one the user is driving.
  const motion = tweening || viewDirty || (flying && !idleSpin);
  const flowing = tickFlows(dt);
  const fading = tickThemeFade(t);
  if (halo.visible) haloMaterial.uniforms.uTime.value += dt > 0 ? dt : 0;
  const live = theme && theme.live;
  if (live) theme.uTime.value += dt > 0 ? dt : 0;
  let hovered = false;
  if (idleSpin) {
    hovered = pointerDirty;
    if (pointerDirty) onHover(null);
    pointerDirty = false;
  } else if (pointerDirty && !pointerDown && !bird.on) {
    onHover(hitTest());
    hovered = pointerDirty;
    pointerDirty = false;
  }
  if (themeReady && (moved || flowing || viewDirty || hovered || halo.visible || live || fading)) {
    renderer.render(scene, camera);
    viewDirty = false;
  }
  if (motion || idleSpin || flowing || halo.visible || introSpin || live || fading) {
    requestFrame();
  } else {
    parkLoop();
  }
}

hud.overview.addEventListener("click", () => { mode = "overview"; applyMode(); });
hud.changes.addEventListener("click", () => { mode = "overlay"; applyMode(); });

// The sidebar's calls / callers control lives in the detail panel, next to the
// subject it acts on (see refsControl). The direction is a mode, like
// Overview / Changes: it stays while the user moves between nodes.

// The node the toggle acts on: the focused entity, or whatever is selected.
function subjectNode() {
  if (focus) return { kind: "entity", id: focus.entity.id };
  return selected ? { kind: selected.kind, id: selected.id } : null;
}

// Flip the direction and re-apply it to the node on screen. The buttons and
// `c` pass no node, which means the current subject; a click on a node passes
// the node it landed on.
function setCallDirection(inbound, node) {
  callInbound = inbound;
  const subject = node || subjectNode();
  if (!subject) {
    applyMode();
    return;
  }
  if (subject.kind === "entity") {
    const found = byEntity.get(subject.id);
    if (!found || !found.box) {
      applyMode();
      return;
    }
    if (focus && focus.entity.id === subject.id) {
      applyMode();
      const cam = focusCamera();
      flyTo(cam.pos, cam.target);
      return;
    }
    if (selected && selected.kind === "entity" && selected.id === subject.id) {
      if (focus) dropFocus();
      applyMode();
      const points = (linkPoints && linkPoints.length ? linkPoints : [entityAnchor(found)]).concat(entityExtent(found));
      const pose = framePose(points);
      flyTo(pose.pos, pose.target);
      return;
    }
    if (focus) dropFocus();
    selectEntity(subject.id);
    return;
  }
  // A package: the same fan, drawn from packageEdges the other way round.
  if (selected && selected.kind === subject.kind && selected.id === subject.id) {
    if (focus) dropFocus();
    applyMode();
    flyToPackage(subject.id);
    return;
  }
  if (focus) dropFocus();
  if (subject.kind === "external") selectExternal(subject.id, inbound);
  else selectPackage(subject.id, true);
}

// Right-click has no menu of its own: the Calls / Callers buttons pick the
// direction. The canvas still swallows the browser's context menu.
renderer.domElement.addEventListener("contextmenu", (event) => event.preventDefault());
// A tap is one finger that went down and came up in place. A second finger
// makes it a pinch or a pan, and nothing it lifts off is a tap.
const touching = new Set();
renderer.domElement.addEventListener("pointerdown", (event) => {
  touching.add(event.pointerId);
  if (touching.size > 1) cancelPendingTap();
  if (skinPicking || quickZoom) return;
  pointerDown = touching.size > 1 ? null : { x: event.clientX, y: event.clientY, button: event.button, touch: event.pointerType !== "mouse" };
  if (tourUI) tourUI.userTookOver();
  tween = null;
});
function aimPointer(x, y) {
  const rect = renderer.domElement.getBoundingClientRect();
  pointer.x = ((x - rect.left) / rect.width) * 2 - 1;
  pointer.y = -((y - rect.top) / rect.height) * 2 + 1;
}
// Hover names follow a mouse. A finger has no hover: it only aims taps.
renderer.domElement.addEventListener("pointermove", (event) => {
  aimPointer(event.clientX, event.clientY);
  if (event.pointerType === "mouse") pointerDirty = true;
  requestFrame();
});
renderer.domElement.addEventListener("pointercancel", (event) => {
  touching.delete(event.pointerId);
  pointerDown = null;
});
renderer.domElement.addEventListener("pointerup", (event) => {
  touching.delete(event.pointerId);
  if (!pointerDown) return;
  const start = pointerDown;
  const moved = Math.hypot(event.clientX - start.x, event.clientY - start.y);
  pointerDown = null;
  if (skinPicking) return;
  // A fingertip wobbles more than a mouse does.
  if (moved > (start.touch ? 10 : 5) || start.button !== 0) return;
  if (bird.on) return;
  if (start.touch) {
    holdTap(event.clientX, event.clientY);
    return;
  }
  activate(describeHit(hitTest()));
});

// Touch taps work the way they do on a map. A tap waits a moment before it
// acts, because it may be the first half of a double tap: on its own it does
// what a click does (and on the ground or the sky it is Escape). A second
// finger-down in that moment, near the first, is a double tap and never
// selects. Lifted at once it zooms in toward the spot. Held and dragged it is
// the one-finger zoom: down zooms in, up zooms out, for as long as the finger
// moves, and the orbit stays out of it.
const DOUBLE_TAP_MS = 250;
const TAP_SLOP_PX = 40;
const QUICK_ZOOM_PX = 160;
let pendingTap = null;
let quickZoom = null;

function holdTap(x, y) {
  cancelPendingTap();
  pendingTap = { x, y, at: performance.now(), timer: 0 };
  const tap = pendingTap;
  tap.timer = setTimeout(() => {
    if (pendingTap !== tap) return;
    pendingTap = null;
    runTap(x, y);
  }, DOUBLE_TAP_MS);
}

function cancelPendingTap() {
  if (pendingTap) clearTimeout(pendingTap.timer);
  pendingTap = null;
}

function runTap(x, y) {
  if (skinPicking || !laid || bird.on) return;
  aimPointer(x, y);
  noteActivity();
  endIntro();
  const found = describeHit(hitTest());
  if (found) activate(found);
  else goBack();
  requestFrame();
}

// Capture on the canvas's parent runs before the orbit control's own
// pointerdown, so a double tap can switch the orbit off before it starts.
viewEl.addEventListener("pointerdown", (event) => {
  if (event.target !== renderer.domElement || event.pointerType === "mouse" || skinPicking) return;
  const first = pendingTap;
  if (!first || touching.size > 0) return;
  if (performance.now() - first.at > DOUBLE_TAP_MS || Math.hypot(event.clientX - first.x, event.clientY - first.y) > TAP_SLOP_PX) return;
  cancelPendingTap();
  controls.enabled = false;
  tween = null;
  // The finger may leave the canvas mid-zoom; its moves still come here.
  try { renderer.domElement.setPointerCapture(event.pointerId); } catch { /* no capture: moves off the canvas are lost */ }
  const target = controls.target.clone();
  quickZoom = {
    id: event.pointerId,
    x: event.clientX,
    y: event.clientY,
    moved: false,
    target,
    away: camera.position.clone().sub(target),
  };
  noteActivity();
  endIntro();
}, true);

viewEl.addEventListener("pointermove", (event) => {
  const zoom = quickZoom;
  if (!zoom || zoom.id !== event.pointerId) return;
  const dy = event.clientY - zoom.y;
  if (!zoom.moved && Math.abs(dy) < 8) return;
  zoom.moved = true;
  const dist = Math.min(controls.maxDistance, Math.max(controls.minDistance, zoom.away.length() * Math.exp(-dy / QUICK_ZOOM_PX)));
  camera.position.copy(zoom.target).add(zoom.away.clone().setLength(dist));
  controls.target.copy(zoom.target);
  viewDirty = true;
  requestFrame();
}, true);

function endQuickZoom(event) {
  const zoom = quickZoom;
  if (!zoom || zoom.id !== event.pointerId) return;
  quickZoom = null;
  touching.delete(event.pointerId);
  controls.enabled = !skinPicking;
  if (!zoom.moved && event.type === "pointerup") {
    aimPointer(event.clientX, event.clientY);
    zoomToward(0.5, true);
  }
  requestFrame();
}
viewEl.addEventListener("pointerup", endQuickZoom, true);
viewEl.addEventListener("pointercancel", endQuickZoom, true);

const tapGround = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0);
const tapSpot = new THREE.Vector3();

// Move the camera k times its distance from the look-at point. Zooming in
// also slides the look-at point halfway to where the finger landed.
function zoomToward(k, toSpot) {
  const target = (tween ? tween.toTarget : controls.target).clone();
  const pos = (tween ? tween.toPos : camera.position).clone();
  if (toSpot) {
    raycaster.setFromCamera(pointer, camera);
    tapGround.constant = -target.y;
    if (raycaster.ray.intersectPlane(tapGround, tapSpot)) {
      const shift = tapSpot.sub(target).multiplyScalar(0.5);
      target.add(shift);
      pos.add(shift);
    }
  }
  const away = pos.sub(target);
  const dist = Math.min(controls.maxDistance, Math.max(controls.minDistance, away.length() * k));
  away.setLength(dist);
  flyTo(target.clone().add(away), target);
}

function closeSearch() {
  searchHits = [];
  commandHits = [];
  searchCursor = 0;
  hud.results.hidden = true;
  hud.results.replaceChildren();
}

function renderSearch() {
  stopMarches();
  hud.results.replaceChildren();
  if (!searchHits.length) {
    hud.results.hidden = true;
    return;
  }
  hud.results.hidden = false;
  placeResults();
  searchHits.forEach((item, index) => {
    const button = document.createElement("button");
    button.type = "button";
    if (index === searchCursor) button.className = "on";
    const kind = document.createElement("span");
    kind.className = "kind";
    kind.textContent = item.kind;
    const name = clipText(item.name);
    attachMarquee(button, name);
    const where = document.createElement("span");
    where.className = "where";
    where.textContent = item.where || "";
    button.append(kind, name, where);
    button.addEventListener("mousedown", (event) => event.preventDefault());
    button.addEventListener("click", () => goToResult(item));
    hud.results.append(button);
  });
  revealSearchCursor();
}

function revealSearchCursor() {
  const list = hud.results;
  const current = list.querySelector("button.on");
  if (!current) return;
  const top = current.offsetTop;
  const bottom = top + current.offsetHeight;
  const viewTop = list.scrollTop;
  const viewBottom = viewTop + list.clientHeight;
  if (top < viewTop) list.scrollTop = top;
  else if (bottom > viewBottom) list.scrollTop = bottom - list.clientHeight;
}

// The menu is outside the sidebar. A backdrop-filter inside that panel
// cannot frost the list drawn behind it.
function placeResults() {
  const list = hud.results;
  if (list.hidden) return;
  const box = hud.search.getBoundingClientRect();
  if (box.width < 2 || box.height < 2) {
    closeSearch();
    return;
  }
  list.style.left = box.left + "px";
  list.style.top = (box.bottom + 2) + "px";
  list.style.width = box.width + "px";
  // On a phone the box sits low in the sheet: the list stops at the screen's edge.
  list.style.maxHeight = Math.max(120, Math.min(280, window.innerHeight - box.bottom - 10)) + "px";
}

// The box finds nodes. A value that starts with a slash is a command: the
// list shows what the viewer can do, and Enter runs the highlighted one.
function onSearch() {
  searchCursor = 0;
  if (hud.search.value.startsWith("/")) {
    searchHits = [];
    commandHits = matchCommands(hud.search.value);
    renderCommands();
    return;
  }
  commandHits = [];
  searchHits = rankMatches(catalog, hud.search.value, 12);
  renderSearch();
}

const COMMANDS = [
  { name: "/skin", does: "choose a theme", run: () => openSkinPanel() },
];

function matchCommands(value) {
  const q = value.trim().toLowerCase();
  if (q === "/") return COMMANDS;
  return COMMANDS.filter((cmd) => cmd.name.startsWith(q));
}

function renderCommands() {
  stopMarches();
  hud.results.replaceChildren();
  if (!commandHits.length) {
    hud.results.hidden = true;
    return;
  }
  hud.results.hidden = false;
  placeResults();
  commandHits.forEach((cmd, index) => {
    const button = document.createElement("button");
    button.type = "button";
    if (index === searchCursor) button.className = "on";
    const kind = document.createElement("span");
    kind.className = "kind";
    kind.textContent = "cmd";
    const name = document.createElement("span");
    name.className = "name";
    name.textContent = cmd.name;
    const where = document.createElement("span");
    where.className = "where";
    where.textContent = cmd.does;
    button.append(kind, name, where);
    button.addEventListener("mousedown", (event) => event.preventDefault());
    button.addEventListener("click", () => runCommand(cmd));
    hud.results.append(button);
  });
  revealSearchCursor();
}

function runCommand(cmd) {
  if (!cmd) return;
  hud.search.value = "";
  closeSearch();
  hud.search.blur();
  cmd.run();
}

function goToResult(item) {
  closeSearch();
  hud.search.blur();
  if (!item) return;
  if (item.kind === "package") {
    dropFocus();
    selectPackage(item.id, true);
    applyMode();
    return;
  }
  if (item.kind === "external") {
    dropFocus();
    selectExternal(item.id);
    applyMode();
    return;
  }
  const found = byEntity.get(item.id);
  if (!found) return;
  // Search lands on the tower, the same as one click. A second click or
  // Enter opens the call diff. Opening it here plants the body bars in the air.
  dropFocus();
  selected = null;
  selectEntity(item.id);
}

hud.search.addEventListener("input", onSearch);

window.addEventListener("pointerdown", (event) => {
  noteActivity();
  endIntro();
  requestFrame();
});
// A moving cursor is not activity: it redraws the hover it is over, but it must
// not reset the idle clock, or the orbit and the animations would never settle.
window.addEventListener("wheel", () => { noteActivity(); endIntro(); requestFrame(); }, { passive: true });

// Registered first, and on the capture path, so it runs before the viewer's
// own keys and before the tour. While the menu is open those keys do nothing.
window.addEventListener("keydown", (event) => {
  if (event.target === hud.search) return;
  // The theme menu's own find box types freely; Enter and Escape are its.
  if (event.target === skinSearch) {
    event.stopImmediatePropagation();
    if (event.key === "Enter") {
      event.preventDefault();
      skinSearch.blur();
      void acceptSkin();
    } else if (event.key === "Escape") {
      event.preventDefault();
      closeSkinSearch();
    }
    return;
  }
  if (!skinPicking && !skinRevealing) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  requestFrame();
  if (skinRevealing) return;
  noteActivity();
  endIntro();
  if (!confirmEl.hidden) {
    if (event.key === "Escape") confirmIgnoreSkin();
    return;
  }
  const plain = !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey;
  if (event.key === "Escape") {
    clearSkinFind();
    requestCloseSkinPanel();
    return;
  }
  if (plain && (event.key === "ArrowLeft" || event.key === "ArrowRight")) {
    moveSkinCursor(event.key === "ArrowRight" ? 1 : -1);
    return;
  }
  if (event.key === "Enter" && !event.repeat) {
    void acceptSkin();
    return;
  }
  if (plain && event.key === "Backspace") {
    if (!skinFind) return;
    const rest = skinFind.slice(0, -1);
    if (!rest) clearSkinFind();
    else queueSkinFind(rest);
    return;
  }
  if (plain && event.key.length === 1 && /[a-z0-9- ]/i.test(event.key)) {
    queueSkinFind(skinFind + event.key.toLowerCase());
  }
}, true);

window.addEventListener("keydown", (event) => {
  // The terminal owns the keyboard while it is up; esc closes an error.
  if (editorActive()) {
    if (event.key === "Escape" && document.querySelector("#editor .editor-msg:not([hidden])")) {
      event.preventDefault();
      closeEditor();
    }
    return;
  }
  // The diff overlay is a reading surface: it scrolls, it takes Escape, and
  // nothing else the viewer listens for should fire while it is up.
  if (diffActive()) return;
  requestFrame();
  if (editAction(event) && event.target !== hud.search) {
    const target = editTarget(selected || (focus && { kind: "entity", id: focus.entity.id }), byEntity, byPackage);
    if (target) {
      event.preventDefault();
      held.clear();
      openEditor(target, () => {
        renderer.domElement.focus?.();
        // Time in the editor is not idle time: the orbit waits its full delay.
        noteActivity();
        lastFrame = 0;
        requestFrame();
      });
    }
    return;
  }
  if (diffAction(event) && event.target !== hud.search) {
    const target = diffTarget(selected || (focus && { kind: "entity", id: focus.entity.id }), byEntity, byPackage);
    if (target) {
      event.preventDefault();
      held.clear();
      openDiff(target, () => { renderer.domElement.focus?.(); requestFrame(); }).catch(() => {});
    }
    return;
  }
  const typing = event.target === hud.search;
  const sidebarKey = !typing && event.key === "b" && !event.metaKey && !event.ctrlKey && !event.altKey;
  const helpKey = !typing && event.key === "?";
  if (!sidebarKey && !helpKey) {
    noteActivity();
    endIntro();
  }
  if (event.key === "/" && !typing) {
    event.preventDefault();
    setSide(true);
    hud.search.focus();
    hud.search.select();
    return;
  }
  if (!typing && event.key === "b" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    setSide(hud.side.classList.contains("is-collapsed"));
    return;
  }
  if (!typing && event.key === "?" && !event.repeat) {
    event.preventDefault();
    setLegend(hud.legend.hidden);
    return;
  }
  if (!typing && event.key === "Escape" && !hud.legend.hidden) {
    event.preventDefault();
    setLegend(false);
    return;
  }
  if (!typing && event.key === "Escape" && !confirmEl.hidden) {
    event.preventDefault();
    confirmIgnoreSkin();
    return;
  }
  const flyKey = flyToken(event.key);
  if (!typing && hud.legend.hidden && flyKey && !event.metaKey && !event.ctrlKey && !event.altKey) {
    held.add(flyKey);
    event.preventDefault();
  }
  if (typing) {
    // Arrows, Tab and Shift-Tab walk the completions. Tab only takes the key
    // when there is something to complete; with an empty list it moves focus
    // the way it always does.
    if (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Tab") {
      const list = commandHits.length ? commandHits : searchHits;
      if (!list.length) {
        if (event.key === "Tab") return;
        event.preventDefault();
        return;
      }
      event.preventDefault();
      const back = event.key === "ArrowUp" || event.shiftKey;
      searchCursor = (searchCursor + (back ? -1 : 1) + list.length) % list.length;
      if (commandHits.length) renderCommands();
      else renderSearch();
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      if (commandHits.length) runCommand(commandHits[searchCursor]);
      else if (searchHits[searchCursor]) goToResult(searchHits[searchCursor]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      if (hud.search.value) {
        hud.search.value = "";
        closeSearch();
      } else {
        closeSearch();
        hud.search.blur();
      }
      return;
    }
    return;
  }
  if (birdAction(event)) {
    event.preventDefault();
    if (bird.on) leaveBird();
    else enterBird();
    return;
  }
  // In the bird view the city underneath is hidden: only the keys that do
  // not select or move through it stay live.
  if (bird.on && !["m", "1", "2"].includes(event.key)) return;
  if (event.key === "0" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    resetView();
    return;
  }
  if (event.key === "m" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    mode = mode === "overlay" ? "overview" : "overlay";
    applyMode();
    return;
  }
  if (event.key === "c" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    setCallDirection(!callInbound, null);
    return;
  }
  if (event.key === "o" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    jumpStep(-1);
    return;
  }
  if (event.key === "i" && !event.repeat && !event.metaKey && !event.ctrlKey && !event.altKey) {
    event.preventDefault();
    jumpStep(1);
    return;
  }
  if (event.key === "1") { mode = "overview"; applyMode(); }
  if (event.key === "2") { mode = "overlay"; applyMode(); }
  if (event.key === "Escape") goBack();
  // A focused button owns Enter. The browser is about to activate it, and
  // re-rendering the panel from here would detach it first, so the click would
  // never land. The list rows are buttons, and so are the sidebar's own.
  if (event.key === "Enter" && event.target?.closest?.("button")) return;
  if (event.key === "Enter" && selected && selected.kind === "entity") {
    const found = byEntity.get(selected.id);
    if (found && (found.entity.kind === "function" || found.entity.kind === "method")) enterFocus(found);
  }
});
window.addEventListener("keyup", (event) => {
  const flyKey = flyToken(event.key);
  if (flyKey) held.delete(flyKey);
  requestFrame();
});
window.addEventListener("blur", () => { held.clear(); requestFrame(); });
// The legend's key list is written from KEYBINDS, the same list the tour
// keys and the README are checked against.
document.querySelector("#keys").innerHTML = KEYBINDS.map(
  (b) => "<li>" + b.keys.map((k) => "<kbd>" + k.replace(/&/g, "&amp;").replace(/</g, "&lt;") + "</kbd>").join(" ") + " " + b.does + "</li>",
).join("");

function setLegend(open) {
  hud.legend.hidden = !open;
}

// The arrow hides the panel on click, not on the way past it: pointing at it
// used to collapse the sidebar out from under the pointer.
hud.collapse.addEventListener("click", () => setSide(sheetMode.matches && hud.side.classList.contains("is-collapsed")));
hud.help.addEventListener("click", () => setLegend(hud.legend.hidden));
hud.logo.addEventListener("click", () => setSide(true));
document.querySelector("#legend-close").addEventListener("click", () => setLegend(false));
hud.legend.addEventListener("click", (event) => {
  if (event.target === hud.legend) setLegend(false);
});

for (const row of document.querySelectorAll("#legend .ex")) {
  row.addEventListener("click", () => {
    const open = row.classList.contains("open");
    for (const other of document.querySelectorAll("#legend .ex")) other.classList.remove("open");
    if (!open) row.classList.add("open");
  });
}

window.addEventListener("resize", resizeView);

// Scroll events do not bubble, so this listens in the capture phase to catch
// the sidebar body and every section body: a scroll is part of the entry.
hud.detailBody.addEventListener("scroll", () => syncJumpSidebar(), true);

// Themes: what the /skin command opens. Arrows preview in place. Letters find
// a theme by the start of its name. Enter, and Change on the question, apply
// the preview: the two diagonal lines move out to the edges, the new theme
// between them and the old one outside. Closing any other way drops the preview.

const SKIN_KEY = "citydiff.skin";

function storedSkin() {
  try {
    return localStorage.getItem(SKIN_KEY) || "dark";
  } catch {
    return "dark"; // storage can be off; the viewer still works
  }
}

function storeSkin(id) {
  try {
    if (!id || id === "dark") localStorage.removeItem(SKIN_KEY);
    else localStorage.setItem(SKIN_KEY, id);
  } catch { /* the preview still holds for this session */ }
}

const skinMenu = document.querySelector("#skin-menu");
const skinPrev = document.querySelector("#skin-prev");
const skinNext = document.querySelector("#skin-next");
const skinName = document.querySelector("#skin-name");
const skinCount = document.querySelector("#skin-count");
const skinLinePrev = document.querySelector("#skin-line-prev");
const skinLineNext = document.querySelector("#skin-line-next");
const skinSearch = document.querySelector("#skin-search");
let skinChoices = null;
let skinPreview = null;
let skinPose = null;
let skinGen = 0;
let skinError = "";
let skinFind = "";
let skinFindTimer = 0;
let skinFindGen = 0;
let skinFindJob = null;
let skinAccepting = false;
const skinById = new Map();
const SKIN_INSET = 0.15;
const SKIN_FIND_MS = 400;

async function loadSkinChoices() {
  if (skinChoices) return skinChoices;
  skinChoices = [];
  try {
    const res = await fetch("/skins.json");
    if (res.ok) {
      const catalog = await res.json();
      skinChoices = (catalog.skins || []).map((skin) => skin.id).filter(Boolean);
    }
  } catch { /* the panel shows nothing to choose */ }
  return skinChoices;
}

function skinLabel(id) {
  return String(id || "").split("-").filter(Boolean).map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(" ");
}

async function cachedSkin(id) {
  if (skinById.has(id)) return skinById.get(id);
  const skin = await loadSkin(id);
  skinById.set(id, skin);
  return skin;
}

// The two dividers lean the same way, 12 degrees. At rest they cross
// mid-height at 15% and 85%, so the gap between them is 70% of the width.
// inset is that margin: 0 puts a line on each edge, and past 0 the line
// has left the screen.
function skinFrame(inset) {
  const w = skinMenu.clientWidth;
  const h = skinMenu.clientHeight;
  const slide = Math.tan(12 * Math.PI / 180) * h * 0.5;
  const left = w * inset;
  const right = w * (1 - inset);
  return {
    w, h, slide,
    leftTop: left + slide,
    leftBot: left - slide,
    rightTop: right + slide,
    rightBot: right - slide,
  };
}

function paintSkinFrame(frame) {
  const { w, h, leftTop, leftBot, rightTop, rightBot } = frame;
  skinPrev.style.clipPath = "polygon(0px 0px, " + leftTop + "px 0px, " + leftBot + "px " + h + "px, 0px " + h + "px)";
  skinNext.style.clipPath = "polygon(" + rightTop + "px 0px, " + w + "px 0px, " + w + "px " + h + "px, " + rightBot + "px " + h + "px)";
  placeSkinLine(skinLinePrev, leftTop, leftBot, h);
  placeSkinLine(skinLineNext, rightTop, rightBot, h);
  return "polygon(" + leftTop + "px 0px, " + rightTop + "px 0px, " + rightBot + "px " + h + "px, " + leftBot + "px " + h + "px)";
}

function layoutSkinFrame() {
  if (!skinPicking || skinRevealing) return;
  const frame = skinFrame(SKIN_INSET);
  if (frame.w < 2 || frame.h < 2) return;
  paintSkinFrame(frame);
}

function placeSkinLine(line, x1, x2, h) {
  line.setAttribute("x1", x1);
  line.setAttribute("y1", 0);
  line.setAttribute("x2", x2);
  line.setAttribute("y2", h);
}

function paintSkinMenu() {
  const id = skinPreview || storedSkin();
  const at = skinChoices ? skinChoices.indexOf(id) : -1;
  skinName.textContent = skinError || skinLabel(id);
  skinCount.textContent = at >= 0 && skinChoices.length ? (at + 1) + "/" + skinChoices.length : "";
  const prev = at > 0 ? skinChoices[at - 1] : "";
  const next = at >= 0 && skinChoices && at < skinChoices.length - 1 ? skinChoices[at + 1] : "";
  paintWing(skinPrev, prev);
  paintWing(skinNext, next);
}

async function paintWing(el, id) {
  el.dataset.skin = id || "";
  el.style.transition = "";
  if (!id) {
    el.style.opacity = "0";
    el.style.pointerEvents = "none";
    el.tabIndex = -1;
    el.setAttribute("aria-hidden", "true");
    return;
  }
  try {
    const skin = await cachedSkin(id);
    if (el.dataset.skin !== id) return;
    el.style.backgroundColor = (skin.background && skin.background.color) || (skin.hud && skin.hud.bg) || "";
    el.style.opacity = "1";
    el.style.pointerEvents = "auto";
    el.tabIndex = 0;
    el.setAttribute("aria-hidden", "false");
    el.setAttribute("aria-label", skinLabel(id));
  } catch {
    if (el.dataset.skin !== id) return;
    el.style.opacity = "0";
    el.style.pointerEvents = "none";
  }
}

// A theme change never reloads: the city keeps its camera, selection, focus,
// mode and tour, and the colours cross over in the fade.
async function showTheme(id) {
  const skin = await loadSkin(id);
  await loadShade(skin);
  fadeToTheme(skin);
}

async function previewSkin(id) {
  if (skinRevealing) return;
  const gen = ++skinGen;
  skinError = "";
  try {
    const skin = await cachedSkin(id);
    await loadShade(skin);
    if (gen !== skinGen) return;
    skinPreview = id;
    fadeToTheme(skin);
  } catch (err) {
    if (gen !== skinGen) return;
    skinError = "Could not read theme " + id + ". " + err.message;
  }
  paintSkinMenu();
}

// Drop the 450 ms page fade so the lines are the only transition. The class
// comes back after the new values are committed, or the background would ease
// under the still.
function snapPage(next) {
  const root = document.documentElement;
  const ready = root.classList.contains("theme-ready");
  if (ready) root.classList.remove("theme-ready");
  applyPage(next, true);
  if (ready) {
    void root.offsetWidth;
    root.classList.add("theme-ready");
  }
}

// Paint a theme in one frame. The fade leaves colours mid-lerp, and the lines
// need a finished picture of each side.
function showThemeNow(next) {
  if (!next) return;
  themeFade = null;
  if (theme && theme.uTime) next.uTime = theme.uTime;
  adoptShaders(next);
  theme = next;
  syncPaint(next);
  repaintScene();
  redrawPlates(next.label.text, next.label.background);
  applySky(scene, next);
  snapPage(next);
  skinWarning = next.shadeWarning || "";
  updateHUD();
  renderer.render(scene, camera);
  viewDirty = false;
}

// The WebGL canvas is one theme at a time. A still of the new theme, clipped
// to the band between the two lines, sits over the live city painted with the
// old one. Walking the lines outward uncovers the new theme.
function snapshotView() {
  const src = renderer.domElement;
  if (!src.width || !src.height) return null;
  try {
    const snap = document.createElement("canvas");
    snap.id = "skin-reveal";
    snap.width = src.width;
    snap.height = src.height;
    snap.setAttribute("aria-hidden", "true");
    const ctx = snap.getContext("2d", { alpha: false });
    if (!ctx) return null;
    ctx.drawImage(src, 0, 0);
    const rect = src.getBoundingClientRect();
    snap.style.left = rect.left + "px";
    snap.style.top = rect.top + "px";
    snap.style.width = rect.width + "px";
    snap.style.height = rect.height + "px";
    snap.style.clipPath = paintSkinFrame(skinFrame(SKIN_INSET));
    return snap;
  } catch {
    return null;
  }
}

function hideSkinWings() {
  // The stylesheet fades a wing's opacity. Applying needs the old city
  // outside the lines on the first frame, not a neighbour colour easing away.
  for (const wing of [skinPrev, skinNext]) {
    wing.style.transition = "none";
    wing.style.opacity = "0";
    wing.style.pointerEvents = "none";
  }
}

// inset runs from the resting 15% out past the edges, so the last frame has
// both lines off the screen and the still covers the view.
function spreadLines(snap) {
  const start = SKIN_INSET;
  const wide = skinFrame(0);
  const end = wide.w > 0 ? -(wide.slide + 4) / wide.w : -0.2;
  const t0 = performance.now();
  return new Promise((resolve) => {
    const step = (now) => {
      if (!snap.isConnected) {
        resolve();
        return;
      }
      const k = Math.min(1, (now - t0) / THEME_REVEAL_MS);
      const eased = 1 - (1 - k) * (1 - k) * (1 - k);
      snap.style.clipPath = paintSkinFrame(skinFrame(start + (end - start) * eased));
      if (k < 1) requestAnimationFrame(step);
      else resolve();
    };
    requestAnimationFrame(step);
  });
}

// Enter and Change both come through here. Arrows and letters never do: a
// preview is the fade, and the lines leaving the screen is the apply.
async function saveSkin() {
  if (skinRevealing) return;
  if (!skinPreview || skinPreview === storedSkin()) {
    closeSkinPanel();
    return;
  }
  const id = skinPreview;
  const previousId = storedSkin();
  const next = theme;
  skinRevealing = true;
  const gen = ++skinGen;
  let snap = null;
  let applied = false;
  try {
    const prev = await cachedSkin(previousId);
    await loadShade(prev);
    if (gen !== skinGen || !skinPicking) return;
    storeSkin(id);
    skinPreview = null;
    applied = true;
    tween = null;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!reduce) {
      showThemeNow(next);
      snap = snapshotView();
      if (snap) showThemeNow(prev);
    }
    hideSkinWings();
    if (snap) {
      document.body.appendChild(snap);
      await spreadLines(snap);
      showThemeNow(next);
      snap.remove();
      snap = null;
    } else {
      showThemeNow(next);
    }
    skinWarning = "Theme " + id + " saved.";
    updateHUD();
  } catch {
    storeSkin(id);
    skinPreview = null;
    applied = true;
    try { showThemeNow(next); } catch { /* the city stays as it is */ }
    skinWarning = "Theme " + id + " saved.";
    updateHUD();
  } finally {
    if (snap && snap.parentNode) snap.remove();
    skinRevealing = false;
    if (applied) closeSkinPanel();
  }
}

// Left and right walk the themes from the preview, which starts at the
// saved one. The ends stop, so an empty side band means there is no theme
// that way. Each step previews what it lands on.
function moveSkinCursor(step) {
  if (skinRevealing || !skinChoices.length) return;
  clearSkinFind();
  const at = skinChoices.indexOf(skinPreview || storedSkin());
  const next = (at < 0 ? 0 : at) + step;
  if (next < 0 || next >= skinChoices.length) return;
  previewSkin(skinChoices[next]);
}

// Letters find a theme by the start of its id or its displayed name.
// "to" matches tokyo-night. The pause lets a second letter arrive before
// the city recolours, so a word does not step through every prefix.
function clearSkinFind() {
  clearTimeout(skinFindTimer);
  skinFindTimer = 0;
  skinFind = "";
  skinFindGen++;
  skinGen++;
}

function skinFindMatch(query) {
  const q = String(query || "").toLowerCase();
  if (!q.trim() || !skinChoices) return "";
  const fold = (value) => value.toLowerCase().replace(/[\s_-]+/g, "");
  const qFold = fold(q);
  return skinChoices.find((id) => {
    if (id.toLowerCase().startsWith(q)) return true;
    const label = skinLabel(id).toLowerCase();
    return label.startsWith(q) || fold(id).startsWith(qFold) || fold(label).startsWith(qFold);
  }) || "";
}

function queueSkinFind(text) {
  skinFind = text;
  clearTimeout(skinFindTimer);
  // A letter replaces a jump that has not landed yet. The pause starts over.
  skinFindGen++;
  skinGen++;
  skinFindTimer = setTimeout(() => {
    skinFindJob = applySkinFind();
  }, SKIN_FIND_MS);
}

async function applySkinFind() {
  const gen = skinFindGen;
  skinFindTimer = 0;
  const query = skinFind;
  skinFind = "";
  if (!query || gen !== skinFindGen || skinRevealing || !skinPicking) return;
  const match = skinFindMatch(query);
  if (!match || match === (skinPreview || storedSkin())) return;
  await previewSkin(match);
}

async function commitSkinFind() {
  clearTimeout(skinFindTimer);
  skinFindTimer = 0;
  if (skinFindJob) {
    const job = skinFindJob;
    skinFindJob = null;
    await job;
  }
  if (!skinFind) return;
  skinFindJob = applySkinFind();
  const job = skinFindJob;
  skinFindJob = null;
  await job;
}

async function acceptSkin() {
  if (skinAccepting || skinRevealing) return;
  skinAccepting = true;
  try {
    await commitSkinFind();
    if (unsavedPreview()) await saveSkin();
    else closeSkinPanel();
  } finally {
    skinAccepting = false;
  }
}

async function openSkinPanel() {
  if (skinRevealing) return;
  clearSkinFind();
  closeSearch();
  hud.search.blur();
  setLegend(false);
  skinError = "";
  await loadSkinChoices();
  if (!skinChoices.length) return;
  if (!skinPicking) {
    skinPose = { pos: camera.position.clone(), target: controls.target.clone() };
    skinPicking = true;
    document.body.classList.add("skin-open");
    skinMenu.hidden = false;
    controls.enabled = false;
    held.clear();
    tween = null;
    introSpin = false;
    onHover(null);
    resizeView();
    if (laid) frameSkinCity();
  }
  paintSkinMenu();
  skinMenu.focus({ preventScroll: true });
}

function frameSkinCity() {
  skinFitSize = viewEl.clientWidth + "x" + viewEl.clientHeight;
  const pose = frameCity(0.84);
  applyFitLimits(pose);
  flyTo(pose.pos, pose.target);
  // Called from a timer too, when nothing else has asked for a frame.
  requestFrame();
}

async function closeSkinPanel() {
  if (!skinPicking || skinRevealing) return;
  clearSkinFind();
  const preview = skinPreview;
  skinPreview = null;
  skinPicking = false;
  skinFitSize = "";
  clearTimeout(skinFitTimer);
  closeSkinSearch();
  skinMenu.hidden = true;
  document.body.classList.remove("skin-open");
  controls.enabled = true;
  const back = skinPose;
  skinPose = null;
  resizeView();
  if (back) flyTo(back.pos, back.target);
  if (!preview || preview === storedSkin()) return;
  // Closing without saving drops the preview and puts the saved theme back.
  const gen = ++skinGen;
  try {
    const skin = await cachedSkin(storedSkin());
    await loadShade(skin);
    if (gen === skinGen) fadeToTheme(skin);
  } catch { /* the saved theme is gone; keep what is on screen */ }
}

// A preview that was never saved is the one case where leaving the menu loses
// something the user asked for. Escape asks, rather than quietly putting the
// old theme back.
const confirmEl = document.querySelector("#confirm");
const confirmText = document.querySelector("#confirm-text");
const confirmChange = document.querySelector("#confirm-change");
const confirmIgnore = document.querySelector("#confirm-ignore");

function unsavedPreview() {
  return !!skinPreview && skinPreview !== storedSkin();
}

function askAboutSkin() {
  if (!skinPicking) return false;
  if (!unsavedPreview()) return false;
  confirmText.textContent =
    "\u201c" + skinPreview + "\u201d is previewed and not saved. Change to it, or ignore the preview and stay on \u201c" + storedSkin() + "\u201d.";
  confirmEl.hidden = false;
  confirmChange.focus();
  return true;
}

function closeConfirm() {
  confirmEl.hidden = true;
  if (skinPicking) skinMenu.focus({ preventScroll: true });
}

// Change: what was previewed becomes the saved theme, and the panel closes.
function confirmChangeSkin() {
  closeConfirm();
  saveSkin();
}

// Ignore: the preview is dropped and the saved theme comes back.
function confirmIgnoreSkin() {
  closeConfirm();
  closeSkinPanel();
}

// The way out of the panel: ask when there is something to lose.
function requestCloseSkinPanel() {
  if (askAboutSkin()) return;
  closeSkinPanel();
}

// On a touch screen the themes are a strip: a swipe to the left brings the
// next one in from the right, and a swipe to the right the previous one. The
// window hears it on the capture path, so the wings and the city behind the
// menu do not have to. A swipe that ends on a wing is not also a tap on it.
let skinSwipe = null;
let skinSwiped = 0;
const SKIN_SWIPE_PX = 40;

window.addEventListener("pointerdown", (event) => {
  if (!skinPicking || skinRevealing || event.pointerType === "mouse" || !confirmEl.hidden) return;
  if (event.target.closest && event.target.closest("#skin-actions, #skin-find")) return;
  skinSwipe = { id: event.pointerId, x: event.clientX, y: event.clientY };
}, true);
window.addEventListener("pointercancel", () => { skinSwipe = null; }, true);
window.addEventListener("pointerup", (event) => {
  const start = skinSwipe;
  if (!start || start.id !== event.pointerId) return;
  skinSwipe = null;
  if (!skinPicking) return;
  const dx = event.clientX - start.x;
  const dy = event.clientY - start.y;
  if (Math.abs(dx) < SKIN_SWIPE_PX || Math.abs(dx) < Math.abs(dy) * 1.5) return;
  skinSwiped = performance.now();
  noteActivity();
  moveSkinCursor(dx < 0 ? 1 : -1);
  requestFrame();
}, true);

function swipedJustNow() {
  return performance.now() - skinSwiped < 400;
}

skinPrev.addEventListener("click", () => { if (!swipedJustNow()) moveSkinCursor(-1); });
skinNext.addEventListener("click", () => { if (!swipedJustNow()) moveSkinCursor(1); });
// Without a keyboard there are no letters to find a theme by: the magnifier
// opens a box (and the phone's keyboard) that finds the same way they do.
function openSkinSearch() {
  skinSearch.hidden = false;
  skinSearch.value = "";
  skinSearch.focus({ preventScroll: true });
}

function closeSkinSearch() {
  skinSearch.value = "";
  skinSearch.hidden = true;
  if (document.activeElement === skinSearch) skinSearch.blur();
  if (skinPicking) skinMenu.focus({ preventScroll: true });
}

document.querySelector("#skin-find-open").addEventListener("click", () => {
  if (skinSearch.hidden) openSkinSearch();
  else closeSkinSearch();
});
skinSearch.addEventListener("input", () => {
  const query = skinSearch.value.trim().toLowerCase();
  if (query) queueSkinFind(query);
  else clearSkinFind();
});
skinSearch.addEventListener("blur", () => {
  if (!skinSearch.value) skinSearch.hidden = true;
});

// Without a keyboard there is no Enter or Escape: these two stand in for them.
document.querySelector("#skin-apply").addEventListener("click", () => { void acceptSkin(); });
document.querySelector("#skin-cancel").addEventListener("click", () => closeSkinPanel());
confirmChange.addEventListener("click", confirmChangeSkin);
confirmIgnore.addEventListener("click", confirmIgnoreSkin);
confirmEl.addEventListener("click", (event) => { if (event.target === confirmEl) confirmIgnoreSkin(); });

main();

// ?perf shows how the page keeps up on this device: frames drawn in the last
// second, the slowest of them, draw calls and the pixel ratio. A pause in
// drawing (nothing moving) is not counted as a slow frame.
if (new URLSearchParams(location.search).has("perf")) {
  const box = document.createElement("div");
  box.id = "perf";
  document.body.append(box);
  let drawn = 0;
  let worst = 0;
  let last = 0;
  const render = renderer.render.bind(renderer);
  renderer.render = (target, eye) => {
    const now = performance.now();
    const gap = now - last;
    if (last && gap < 500) worst = Math.max(worst, gap);
    last = now;
    drawn++;
    render(target, eye);
  };
  setInterval(() => {
    box.textContent = drawn + " fps · worst " + Math.round(worst) + " ms · " + renderer.info.render.calls + " calls · " + renderer.getPixelRatio() + "x";
    drawn = 0;
    worst = 0;
  }, 1000);
}

// ?check=1 reports arcs that pass through buildings.
if (new URLSearchParams(location.search).has("check")) {
  setTimeout(() => window.citydiffCheck(), 3000);
}

// Bird view (y): the city from straight above, merged into districts, one
// flat-topped slab each, tinted by change in the changes mode, with one
// curve per pair of districts that depend on each other. Built once per
// scene and mode; the normal city is hidden, not touched, and comes back
// as it was.
const bird = new BirdToggle();
const birdGroup = new THREE.Group();
birdGroup.visible = false;
scene.add(birdGroup);
let birdKey = "";
let birdHidden = null;
let birdStats = null;
const BIRD_KEEP = () => new Set([planeMesh, groundMesh, horizonMesh]);

function birdLabelTexture(text, color) {
  const px = 64;
  const canvas = document.createElement("canvas");
  const ctx = canvas.getContext("2d");
  ctx.font = `600 ${px}px ui-monospace, SFMono-Regular, Menlo, monospace`;
  const w = Math.ceil(ctx.measureText(text).width) + 16;
  canvas.width = w;
  canvas.height = px + 16;
  ctx.font = `600 ${px}px ui-monospace, SFMono-Regular, Menlo, monospace`;
  ctx.textBaseline = "middle";
  ctx.lineWidth = 10;
  ctx.strokeStyle = "rgba(0,0,0,0.85)";
  ctx.strokeText(text, 8, canvas.height / 2);
  ctx.fillStyle = color;
  ctx.fillText(text, 8, canvas.height / 2);
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.anisotropy = 4;
  return { tex, aspect: canvas.width / canvas.height };
}

function birdEdgeList() {
  const edges = depEdges(sceneDoc.packages || [], mode);
  if (mode === "overlay") {
    for (const link of changedCallLinks()) edges.push({ from: link.from.pkg.id, to: link.target.pkg.id, change: link.change });
  }
  return edges;
}

function mergeTubes(parts) {
  let verts = 0;
  let idx = 0;
  for (const p of parts) {
    verts += p.geo.attributes.position.count;
    idx += p.geo.index.count;
  }
  const pos = new Float32Array(verts * 3);
  const nor = new Float32Array(verts * 3);
  const col = new Float32Array(verts * 3);
  const index = new Uint32Array(idx);
  let v = 0;
  let i = 0;
  for (const p of parts) {
    const g = p.geo;
    pos.set(g.attributes.position.array, v * 3);
    nor.set(g.attributes.normal.array, v * 3);
    for (let k = 0; k < g.attributes.position.count; k++) col.set([p.color.r, p.color.g, p.color.b], (v + k) * 3);
    const src = g.index.array;
    for (let k = 0; k < src.length; k++) index[i + k] = src[k] + v;
    v += g.attributes.position.count;
    i += src.length;
    g.dispose();
  }
  const out = new THREE.BufferGeometry();
  out.setAttribute("position", new THREE.BufferAttribute(pos, 3));
  out.setAttribute("normal", new THREE.BufferAttribute(nor, 3));
  out.setAttribute("color", new THREE.BufferAttribute(col, 3));
  out.setIndex(new THREE.BufferAttribute(index, 1));
  return out;
}

function buildBird() {
  const key = mode;
  if (birdKey === key && birdGroup.children.length) return;
  clearGroup(birdGroup);
  birdKey = key;
  const overlay = mode === "overlay" && sceneDoc.diff;
  const cut = chooseDistricts(laid, sceneDoc.packages || []);
  const span = citySpan();
  const members = new Map();
  for (const pkg of sceneDoc.packages || []) {
    const d = cut.districtOf.get(pkg.id);
    if (!d) continue;
    if (!members.has(d)) members.set(d, []);
    members.get(d).push(...(pkg.entities || []).filter((e) => e.kind === "type" || e.kind === "variable" || e.kind === "function" || e.kind === "method"));
  }
  const tops = new Map();
  const labels = [];
  const slabGeo = new THREE.BoxGeometry(1, 1, 1);
  for (const d of cut.districts) {
    const top = d.y + d.h;
    tops.set(d.id, top);
    const st = districtStatus(members.get(d.id) || []);
    const color = overlay ? changeColor(st.status).clone() : plinthColor(d.depth, d.synthetic);
    if (d.base) color.multiplyScalar(0.7);
    const slab = new THREE.Mesh(slabGeo, new THREE.MeshStandardMaterial({ color, roughness: 0.85 }));
    slab.scale.set(Math.max(0.01, d.w - 0.3), top, Math.max(0.01, d.d - 0.3));
    slab.position.set(d.x + d.w / 2, top / 2, d.z + d.d / 2);
    birdGroup.add(slab);
    // The stacked bar: added, modified and removed shares along the near edge.
    if (overlay && st.status !== "same") {
      let x = d.x + 0.15;
      const len = d.w - 0.3;
      const depthBar = Math.min(d.d * 0.08, span * 0.01);
      for (const c of ["added", "modified", "removed"]) {
        const share = st.share[c];
        if (!share) continue;
        const bar = new THREE.Mesh(slabGeo, new THREE.MeshBasicMaterial({ color: changeColor(c) }));
        bar.scale.set(len * share, 0.08, depthBar);
        bar.position.set(x + (len * share) / 2, top + 0.05, d.z + d.d - 0.15 - depthBar / 2);
        birdGroup.add(bar);
        x += len * share;
      }
    }
    const text = d.name || d.id;
    const box = labelBox(text, d.w, d.d, span * 0.05);
    const avoid = d.base
      ? cut.districts.filter((o) => o !== d && o.x >= d.x && o.z >= d.z && o.x + o.w <= d.x + d.w && o.z + o.d <= d.z + d.d)
        .map((o) => ({ x0: o.x, x1: o.x + o.w, z0: o.z, z1: o.z + o.d }))
      : [];
    labels.push({ id: "d:" + d.id, text, x: d.x + d.w / 2, z: d.z + d.d / 2, y: top, priority: d.base ? d.w * d.d * 0.01 : d.w * d.d, avoid, ...box });
  }
  const pairs = aggregateEdges(birdEdgeList(), cut.districtOf);
  const byId = new Map(cut.districts.map((d) => [d.id, d]));
  const parts = [];
  for (const p of pairs) {
    const a = byId.get(p.from);
    const b = byId.get(p.to);
    const s = new THREE.Vector3(a.x + a.w / 2, tops.get(a.id) + 0.2, a.z + a.d / 2);
    const e = new THREE.Vector3(b.x + b.w / 2, tops.get(b.id) + 0.2, b.z + b.d / 2);
    const mid = s.clone().add(e).multiplyScalar(0.5);
    mid.y += s.distanceTo(e) * 0.22 + span * 0.01;
    const curve = new THREE.QuadraticBezierCurve3(s, mid, e);
    // Thickness grows with the square root of the count, so a pair standing
    // for 25 imports is about three times as thick as a single one.
    const radius = Math.min(span * 0.006, span * 0.0006 * (1 + Math.sqrt(p.count)));
    parts.push({ geo: new THREE.TubeGeometry(curve, 20, radius, 5, false), color: overlay ? changeColor(p.change) : paint.selection });
    if (p.count > 1) {
      const text = String(p.count);
      const size = span * 0.012;
      const apex = curve.getPoint(0.5);
      labels.push({ id: "e:" + p.from + "\0" + p.to, text, x: apex.x, z: apex.z, y: apex.y, priority: -1 / p.count, size, w: size * text.length * 0.6, h: size });
    }
  }
  if (parts.length) {
    const mesh = new THREE.Mesh(mergeTubes(parts), new THREE.MeshBasicMaterial({ vertexColors: true, transparent: true, opacity: overlay ? 0.85 : 0.45, depthWrite: false }));
    birdGroup.add(mesh);
  }
  const placed = placeLabels(labels, span * 0.006);
  for (const l of labels) {
    const at = placed.get(l.id);
    if (!at || at.hidden) continue;
    const { tex, aspect } = birdLabelTexture(l.text, l.id.startsWith("e:") ? "#f4f4f5" : "#ffffff");
    const h = at.size * 1.25;
    const plane = new THREE.Mesh(new THREE.PlaneGeometry(h * aspect, h), new THREE.MeshBasicMaterial({ map: tex, transparent: true, depthWrite: false, fog: false }));
    plane.rotation.x = -Math.PI / 2;
    plane.position.set(l.x, l.y + 0.3, l.z);
    plane.renderOrder = 5;
    birdGroup.add(plane);
  }
  birdStats = { districts: cut.districts.length, depth: cut.depth, curves: pairs.length, edges: pairs.reduce((n, p) => n + p.count, 0), labels: [...placed.values()].filter((x) => !x.hidden).length };
  window.citydiffBird = birdStats;
}

// birdHide hides what the bird view replaces and remembers each flag.
function birdHide() {
  const keep = BIRD_KEEP();
  if (!birdHidden) {
    birdHidden = new Map();
    for (const o of [...city.children, arcGroup, selectArcs, focusGroup, halo, ring]) birdHidden.set(o, o.visible);
  }
  for (const o of birdHidden.keys()) if (!keep.has(o)) o.visible = false;
  birdGroup.visible = true;
}

function enterBird() {
  if (!laid || !sceneDoc) return;
  if (!bird.enter({ pos: camera.position.toArray(), target: controls.target.toArray(), up: camera.up.toArray(), zoom: camera.zoom,
    extra: { maxDistance: controls.maxDistance, enableRotate: controls.enableRotate, mode } })) return;
  tween = null;
  endIntro();
  held.clear();
  buildBird();
  birdHide();
  const b = laid.bounds;
  const box = { min: [b.minX, 0, b.minZ], max: [b.maxX, b.maxY * 0.3, b.maxZ] };
  const pose = fitPose(box, [0, 1, 0.0005], { fov: viewInsets.fov, width: viewInsets.width, height: viewInsets.height, left: viewInsets.left, right: viewInsets.right, fill: 0.94 });
  controls.maxDistance = Math.max(controls.maxDistance, pose.dist * 1.6);
  controls.enableRotate = false;
  flyTo(new THREE.Vector3(...pose.pos), new THREE.Vector3(...pose.target));
  viewDirty = true;
  requestFrame();
}

function leaveBird() {
  const saved = bird.leave();
  if (!saved) return;
  birdGroup.visible = false;
  for (const [o, v] of birdHidden || []) o.visible = v;
  birdHidden = null;
  camera.up.fromArray(saved.up);
  camera.zoom = saved.zoom;
  camera.updateProjectionMatrix();
  controls.enableRotate = saved.extra.enableRotate;
  flyTo(new THREE.Vector3(...saved.pos), new THREE.Vector3(...saved.target));
  tween.done = () => { controls.maxDistance = saved.extra.maxDistance; };
  if (saved.extra.mode !== mode) applyMode();
  viewDirty = true;
  requestFrame();
}
