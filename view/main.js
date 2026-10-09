import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { layoutCity, arcBetween, methodDrawY, fitDistance } from "./layout.js";
import { rankMatches } from "./search.js";
import { flyStep } from "./fly.js";

// shadcn zinc. The city stays in this grayscale.
const STONE = new THREE.Color(0xf4f4f5);
const TYPE_COLOR = new THREE.Color(0xd4d4d8);
const FUNC_COLOR = new THREE.Color(0xfafafa);
const METHOD_COLOR = new THREE.Color(0xa1a1aa);
// git's default diff colors: old red, new green, changed yellow, moved magenta, hunk cyan.
const ADD = new THREE.Color(0x00cd00);
const REMOVE = new THREE.Color(0xcd0000);
const MODIFY = new THREE.Color(0xcdcd00);
const MOVED = new THREE.Color(0xcd00cd);
const ARC = new THREE.Color(0x00cdcd);
// A standard-library edge is quieter than a project edge.
const STD_LINE = new THREE.Color(0xd4d4d8);

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
  crumb: document.querySelector("#crumb"),
  note: document.querySelector("#note"),
  detail: document.querySelector("#detail"),
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

let sceneDoc = null;
let laid = null;
let mode = "overview";
let selected = null;
let entered = null;
let focus = null;
let returnPose = null;
let cityPose = null;
let pointerDown = null;

const byEntity = new Map();
const byPackage = new Map();
const byDecl = new Map();
const catalog = [];
let searchHits = [];
let searchCursor = 0;
let showUnchanged = false;
const marchStops = new Set();

function stopMarches() {
  for (const stop of marchStops) stop();
  marchStops.clear();
}

function clipText(label) {
  const clip = document.createElement("span");
  clip.className = "clip";
  const text = document.createElement("span");
  text.className = "marquee";
  text.textContent = label;
  clip.append(text);
  return clip;
}

function attachMarquee(host, clip) {
  const text = clip.querySelector(".marquee");
  let timer = 0;
  const stop = () => {
    if (timer) clearInterval(timer);
    timer = 0;
    text.style.transform = "";
    marchStops.delete(stop);
  };
  host.addEventListener("mouseenter", () => {
    stop();
    const extra = text.scrollWidth - clip.clientWidth;
    if (extra <= 1) return;
    const chars = Math.max((text.textContent || "").length, 1);
    const ch = text.scrollWidth / chars;
    const steps = Math.max(1, Math.ceil(extra / ch));
    let i = 0;
    timer = setInterval(() => {
      i = i >= steps ? 0 : i + 1;
      text.style.transform = i ? "translateX(" + (-i * ch) + "px)" : "";
    }, 140);
    marchStops.add(stop);
  });
  host.addEventListener("mouseleave", stop);
}

function setSide(open) {
  hud.side.classList.toggle("is-collapsed", !open);
  requestAnimationFrame(resizeView);
}
let arcSubject = null;
let entitySubject = null;
let lit = null;
let linkPoints = null;
const held = new Set();
const flyDir = new THREE.Vector3();
let lastFrame = 0;
let lastActivity = 0;
const IDLE_SPIN_MS = 15000;
// Q and E orbit at 1.4 rad/s. The idle orbit is a fifth of that.
const IDLE_YAW = 0.28;
let idleSpin = false;
// The city turns as soon as it appears. A click, a move key, or a zoom ends that turn.
// b and ? do not. After the view has been still, the slow orbit returns in every
// view: overview, a selected package, and a call focus.
let introSpin = true;
let viewDirty = true;
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

function requestFrame() {
  if (frameQueued) return;
  frameQueued = true;
  clearTimeout(idleTimer);
  requestAnimationFrame(animate);
}

function parkLoop() {
  clearTimeout(idleTimer);
  const remain = lastActivity ? IDLE_SPIN_MS - (performance.now() - lastActivity) : IDLE_SPIN_MS;
  idleTimer = setTimeout(requestFrame, Math.max(40, remain));
}
const plinths = [];
const entitySlots = [];
const slotById = new Map();
const openByPkg = new Map();

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

function resizeView() {
  const w = viewEl.clientWidth;
  const h = viewEl.clientHeight;
  if (w < 2 || h < 2) return;
  camera.aspect = w / h;
  const side = document.querySelector("#side");
  const cover = side && !side.classList.contains("is-collapsed") ? side.getBoundingClientRect().width : 0;
  if (cover > 8 && w > cover + 40) {
    // Look-at screen X is w/2 - offsetX. The open viewbox center is (w + cover) / 2.
    camera.setViewOffset(w, h, -cover / 2, 0, w, h);
  } else {
    camera.clearViewOffset();
  }
  camera.updateProjectionMatrix();
  renderer.setSize(w, h, false);
  placeResults();
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

const raycaster = new THREE.Raycaster();
const pointer = new THREE.Vector2(-2, -2);

const city = new THREE.Group();
scene.add(city);
const arcGroup = new THREE.Group();
scene.add(arcGroup);
const selectArcs = new THREE.Group();
scene.add(selectArcs);
const focusGroup = new THREE.Group();
scene.add(focusGroup);

let solidMesh = null;
let solidMat = null;
let tween = null;
const ring = new THREE.LineLoop(
  circlePositions(72),
  new THREE.LineBasicMaterial({ color: 0xa1a1aa, transparent: true, opacity: 0.35 }),
);
ring.visible = false;
scene.add(ring);

function boot() {
  const hemi = new THREE.HemisphereLight(0xf4f4f5, 0x27272a, 0.55);
  scene.add(hemi);
  const key = new THREE.DirectionalLight(0xfafafa, 0.8);
  key.position.set(70, 150, 90);
  scene.add(key);
  const rim = new THREE.DirectionalLight(0xd4d4d8, 0.18);
  rim.position.set(-90, 50, -20);
  scene.add(rim);
}

function tintEmissive(material) {
  material.onBeforeCompile = (shader) => {
    shader.fragmentShader = shader.fragmentShader.replace(
      "#include <color_fragment>",
      "#include <color_fragment>\n\ttotalEmissiveRadiance *= diffuseColor.rgb;",
    );
  };
}

async function main() {
  boot();
  let response;
  try {
    response = await fetch("./scene.json");
    if (!response.ok) throw new Error(response.statusText);
    sceneDoc = await response.json();
  } catch (err) {
    hud.note.textContent = "Could not read the scene. " + err.message;
    requestFrame();
    return;
  }
  laid = layoutCity(sceneDoc.packages || []);
  indexScene();
  buildCity();
  buildArcs();
  cityPose = frameCity();
  camera.position.copy(cityPose.pos);
  controls.target.copy(cityPose.target);
  applyFitLimits(cityPose);
  controls.update();
  applyMode();
  applyQuery();
  requestFrame();
}

function applyQuery() {
  const params = new URLSearchParams(location.search);
  if (params.get("mode") === "changes") mode = "overlay";
  const enter = params.get("enter");
  if (enter && byPackage.has(enter)) selectPackage(enter, true);
  const want = params.get("fn");
  if (!want) {
    if (mode === "overlay") applyMode();
    return;
  }
  const picks = [...byEntity.values()].filter((item) => item.box && (item.entity.kind === "function" || item.entity.kind === "method"));
  const named = want === "1" ? null : picks.find((item) => item.entity.id === want);
  const changed = picks.find((item) => (item.entity.calls || []).some((step) => step.change !== "same"));
  const busy = picks.find((item) => (item.entity.calls || []).length > 3 && (item.entity.calls || []).length < 40);
  const pick = named || (want === "1" ? changed || busy || picks[0] : null);
  if (pick) enterFocus(pick);
  else if (mode === "overlay") applyMode();
}

function indexScene() {
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
      if (entity.kind === "type" || entity.kind === "function" || entity.kind === "method") {
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

function frameCity() {
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
  const aspect = camera.aspect > 0.05 ? camera.aspect : 1;
  const dist = fitDistance(points, look, dir, camera.fov, aspect, 0.92);
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

function buildCity() {
  const span = citySpan();
  scene.fog.density = 0.07 / span;
  const groundRadius = span * 0.95;
  const ground = new THREE.Mesh(
    new THREE.CircleGeometry(groundRadius, 72),
    new THREE.MeshLambertMaterial({ color: 0x18181b }),
  );
  ground.rotation.x = -Math.PI / 2;
  ground.position.y = -0.04;
  city.add(ground);
  const horizon = new THREE.Mesh(
    new THREE.RingGeometry(groundRadius * 0.92, groundRadius * 0.935, 80),
    new THREE.MeshBasicMaterial({ color: 0x3f3f46, side: THREE.DoubleSide, transparent: true, opacity: 0.7 }),
  );
  horizon.rotation.x = -Math.PI / 2;
  horizon.position.y = 0.01;
  city.add(horizon);

  for (const box of laid.packages) {
    const pkg = byPackage.get(box.id);
    const geom = new THREE.BoxGeometry(box.w, box.h, box.d);
    const material = new THREE.MeshLambertMaterial({
      color: plinthColor(box.depth, box.synthetic),
      emissive: new THREE.Color(0xffffff),
      emissiveIntensity: 0.2,
    });
    tintEmissive(material);
    const mesh = new THREE.Mesh(geom, material);
    mesh.position.set(box.x + box.w / 2, box.y + box.h / 2, box.z + box.d / 2);
    mesh.userData = { kind: "package", id: box.id };
    city.add(mesh);
    const plate = namePlate(box);
    if (plate) mesh.add(plate);
    plinths.push({ id: box.id, box, mesh, material, plate, pkg });
  }

  for (const box of laid.externals) {
    const mesh = new THREE.Mesh(
      new THREE.OctahedronGeometry(0.9, 0),
      new THREE.MeshStandardMaterial({ color: 0x3f3f46, metalness: 0.35, roughness: 0.6, emissive: 0x000000, emissiveIntensity: 0.2 }),
    );
    mesh.position.set(box.x, box.y + 1.2, box.z);
    mesh.userData = { kind: "external", id: box.id };
    city.add(mesh);
    plinths.push({ id: box.id, box, mesh, material: mesh.material, plate: null, external: true });
  }

  const solids = [];
  for (const box of laid.packages) {
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
  }

  const geo = coloredBox();
  solidMat = new THREE.MeshLambertMaterial({
    color: 0xffffff,
    emissive: 0xffffff,
    emissiveIntensity: 0.28,
    vertexColors: true,
  });
  tintEmissive(solidMat);
  solidMesh = makeInstances(geo, solidMat, solids);
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

function makeInstances(geo, material, slots) {
  if (!slots.length) return null;
  const mesh = new THREE.InstancedMesh(geo, material, slots.length);
  mesh.instanceColor = new THREE.InstancedBufferAttribute(new Float32Array(slots.length * 3), 3);
  const color = new THREE.Color();
  slots.forEach((slot, index) => {
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

function meshSize(slot, open) {
  if (slot.entity.change === "removed" && mode !== "overlay") {
    return { w: 0.001, h: 0.001, d: 0.001 };
  }
  const factor = 0.045 + 0.955 * open;
  return {
    w: Math.max(slot.w, 0.05),
    h: Math.max(slot.h * factor, 0.05),
    d: Math.max(slot.d, 0.05),
  };
}

function slotSize(slot, open) {
  const hidden = slot.entity.change === "removed" && mode !== "overlay";
  const shut = !hidden && lit && open === 0;
  if (hidden) return { w: 0.001, h: 0.001, d: 0.001 };
  if (shut) return { w: Math.max(slot.w * 0.4, 0.04), h: 0.03, d: Math.max(slot.d * 0.4, 0.04) };
  return meshSize(slot, open);
}

// The drawn box. A method's layout y is the type's full roof. While the type
// is still short, that y is above the roof, so the method is seated on the
// height the type has actually grown to.
function visualBox(slot, open) {
  const size = slotSize(slot, open);
  let y = slot.y;
  const parentId = slot.entity && slot.entity.kind === "method" ? slot.entity.parent : "";
  if (parentId) {
    const parent = slotById.get(parentId);
    if (parent) y = methodDrawY(slot.y, parent.h, slotSize(parent, slotOpen(parent)).h);
  }
  return { x: slot.x, y, z: slot.z, w: size.w, h: size.h, d: size.d };
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

function signTexture(label) {
  const font = "600 48px ui-monospace, monospace";
  const probe = document.createElement("canvas").getContext("2d");
  probe.font = font;
  const text = label.length > 22 ? label.slice(0, 21) + "…" : label;
  const textW = Math.ceil(probe.measureText(text).width);
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(64, textW + 28);
  canvas.height = 72;
  const ctx = canvas.getContext("2d");
  ctx.fillStyle = "#09090b";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.font = font;
  ctx.fillStyle = "#fafafa";
  ctx.textBaseline = "middle";
  ctx.fillText(text, 14, 36);
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
  return { tex, aspect: canvas.width / canvas.height };
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
function namePlate(box) {
  if (Math.max(box.w, box.d) < 5 || Math.min(box.w, box.d) < 2.2) return null;
  const { tex, aspect } = signTexture(box.name || box.id);
  const limit = Math.min(box.w, box.d) * 0.86;
  let height = Math.min(1.35, Math.max(0.42, limit * 0.2));
  let width = height * aspect;
  if (width > limit) {
    width = limit;
    height = width / aspect;
  }
  const material = new THREE.MeshBasicMaterial({ map: tex, transparent: true, depthWrite: false, fog: false });
  const geo = new THREE.PlaneGeometry(width, height);
  const group = new THREE.Group();
  const y = box.h / 2 - height / 2 - 0.06;
  const gap = 0.045;
  const faces = [
    [0, y, box.d / 2 + gap, 0],
    [0, y, -box.d / 2 - gap, Math.PI],
    [box.w / 2 + gap, y, 0, Math.PI / 2],
    [-box.w / 2 - gap, y, 0, -Math.PI / 2],
  ];
  for (const [x, py, z, rot] of faces) {
    const mesh = new THREE.Mesh(geo, material);
    mesh.position.set(x, py, z);
    mesh.rotation.y = rot;
    group.add(mesh);
  }
  return group;
}

function plinthColor(depth, synthetic) {
  if (synthetic) return new THREE.Color(0x18181b);
  const steps = [0x27272a, 0x3f3f46, 0x52525b, 0x71717a];
  return new THREE.Color(steps[Math.min(Math.max(depth, 0), steps.length - 1)]);
}

function entityColor(entity) {
  if (mode === "overlay" && entity.change && entity.change !== "same") {
    return changeColor(entity.change);
  }
  const base = entity.kind === "type" ? TYPE_COLOR : entity.kind === "method" ? METHOD_COLOR : FUNC_COLOR;
  return vary(entity.id, base);
}

function vary(id, color) {
  let hash = 0;
  for (let i = 0; i < id.length; i++) hash = (hash * 33 + id.charCodeAt(i)) >>> 0;
  const shift = ((hash % 17) - 8) / 220;
  return color.clone().offsetHSL(0, 0, shift);
}

function buildArcs() {
  for (const pkg of sceneDoc.packages || []) {
    if (pkg.external) continue;
    for (const dep of pkg.deps || []) {
      if (dep.change !== "added" && dep.change !== "removed") continue;
      const ends = depEnds(pkg.id, dep.to);
      if (!ends) continue;
      addArc(arcGroup, ends.from, ends.to, changeColor(dep.change), ends.lift, {
        kind: "dep", id: dep.to, label: dep.to, change: dep.change,
      });
    }
  }
}

// The arc meets each module. Clearance lifts only the middle, and only enough
// to pass the roofs between the two ends.
const LAND = 0.85;

function roofClear(box) {
  return Math.max(1.2, Math.min(Math.max(box.w, box.d) * 0.045, 3));
}

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

function packageAnchor(id) {
  const box = laid.packages.find((item) => item.id === id);
  // Own roof only. A parent's district top sits on its tallest child, so the
  // arc would stop in the air instead of on the module that was imported.
  if (box) return [box.x + box.w / 2, shownOwnTop(id) + LAND, box.z + box.d / 2];
  const ext = laid.externals.find((item) => item.id === id);
  if (ext) return [ext.x, ext.y + ext.h + 0.7, ext.z];
  return null;
}

function rectContainsXZ(box, x, z) {
  return x >= box.x && x <= box.x + box.w && z >= box.z && z <= box.z + box.d;
}

// t along the ground segment where the line is over the rectangle.
function segmentRectSpan(from, to, box) {
  const dx = to[0] - from[0];
  const dz = to[2] - from[2];
  let t0 = 0;
  let t1 = 1;
  const slabs = [
    [dx, from[0], box.x, box.x + box.w],
    [dz, from[2], box.z, box.z + box.d],
  ];
  for (const [d, p, min, max] of slabs) {
    if (Math.abs(d) < 1e-9) {
      if (p < min || p > max) return null;
      continue;
    }
    let a = (min - p) / d;
    let b = (max - p) / d;
    if (a > b) {
      const swap = a;
      a = b;
      b = swap;
    }
    t0 = Math.max(t0, a);
    t1 = Math.min(t1, b);
    if (t0 > t1) return null;
  }
  if (t1 <= 0 || t0 >= 1) return null;
  return [t0, t1];
}

// lift is the quadratic control offset. The bow already used by arcBetween is
// the floor. Extra lift is only what a roof along the span still needs, and it
// is capped so one tall neighbor cannot throw the whole fan into the sky.
function clearanceLift(from, to) {
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  const bow = Math.max(3, Math.min(dist * 0.22, 18));
  let lift = bow;
  const y0 = from[1];
  const y1 = to[1];
  for (const box of laid.packages) {
    if (rectContainsXZ(box, from[0], from[2]) || rectContainsXZ(box, to[0], to[2])) continue;
    const span = segmentRectSpan(from, to, box);
    if (!span) continue;
    const floor = shownOwnTop(box.id) + roofClear(box);
    const t0 = Math.max(span[0], 0.08);
    const t1 = Math.min(span[1], 0.92);
    if (t0 > t1) continue;
    for (let k = 0; k <= 5; k++) {
      const t = t0 + (t1 - t0) * (k / 5);
      const u = 1 - t;
      const base = u * u * y0 + 2 * u * t * ((y0 + y1) / 2) + t * t * y1;
      const coef = 2 * u * t;
      if (coef < 0.08) continue;
      const need = (floor - base) / coef;
      if (need > lift) lift = need;
    }
  }
  return Math.min(lift, 22);
}

function depEnds(fromId, toId) {
  const from = packageAnchor(fromId);
  const to = packageAnchor(toId);
  if (!from || !to) return null;
  return { from, to, lift: clearanceLift(from, to) };
}

// The call leaves the rendered top of the caller and lands on the callee.
// Layout height is the open tower; a shut sibling is not an endpoint.
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

function entityAnchor(found) {
  const top = towerTop(found);
  if (top) return top;
  if (found && found.pkg) return packageAnchor(found.pkg.id);
  return null;
}

function makeLink(from, to, color, lift) {
  if (!from || !to) return null;
  const dist = Math.hypot(to[0] - from[0], to[1] - from[1], to[2] - from[2]);
  if (dist < 0.35) return null;
  const points = arcBetween(from, to, lift).map((p) => new THREE.Vector3(p[0], p[1], p[2]));
  const curve = new THREE.CatmullRomCurve3(points);
  const radius = Math.max(0.07, Math.min(dist * 0.0028, 0.22));
  const geo = new THREE.TubeGeometry(curve, Math.max(10, points.length), radius, 5, false);
  const mat = new THREE.MeshBasicMaterial({ color, fog: false });
  const mesh = new THREE.Mesh(geo, mat);
  mesh.frustumCulled = false;
  return mesh;
}

// Go's standard library is an external import whose first path element has no dot.
function isStdPackage(id) {
  const pkg = byPackage.get(id);
  if (!pkg || !pkg.external) return false;
  const head = String(pkg.id || "").split("/")[0];
  return head.length > 0 && !head.includes(".");
}

const flows = [];
const flowScratch = [0, 0, 0];

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
  const raw = arcBetween(from, to, lift);
  const pathCount = raw.length;
  if (pathCount < 2) return null;
  const path = new Float32Array(pathCount * 3);
  const lengths = new Float32Array(pathCount);
  let total = 0;
  for (let i = 0; i < pathCount; i++) {
    const p = raw[i];
    path[i * 3] = p[0];
    path[i * 3 + 1] = p[1];
    path[i * 3 + 2] = p[2];
    if (i > 0) {
      total += Math.hypot(p[0] - raw[i - 1][0], p[1] - raw[i - 1][1], p[2] - raw[i - 1][2]);
    }
    lengths[i] = total;
  }
  if (!(total > 0)) return null;
  const count = Math.max(5, Math.min(18, Math.round(dist / 5)));
  const positions = new Float32Array(count * 3);
  const geo = new THREE.BufferGeometry();
  geo.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  const mat = new THREE.PointsMaterial({
    color,
    size: Math.max(0.7, Math.min(dist * 0.014, 2.1)),
    sizeAttenuation: true,
    transparent: true,
    opacity: 0.95,
    depthWrite: false,
    fog: false,
  });
  const mesh = new THREE.Points(geo, mat);
  mesh.frustumCulled = false;
  const flow = {
    path, lengths, pathCount, total, count, time: 0,
    speed: Math.min(0.45, Math.max(0.12, 14 / Math.max(dist, 1))),
  };
  mesh.userData.flow = flow;
  const attr = geo.attributes.position;
  for (let i = 0; i < count; i++) {
    flowAt(flow, i / count, flowScratch);
    attr.setXYZ(i, flowScratch[0], flowScratch[1], flowScratch[2]);
  }
  flows.push(mesh);
  return mesh;
}

function flowAt(flow, u, out) {
  const pos = flow.path;
  const lengths = flow.lengths;
  const n = flow.pathCount;
  let d = u * flow.total;
  if (!(d > 0)) {
    out[0] = pos[0];
    out[1] = pos[1];
    out[2] = pos[2];
    return;
  }
  if (d >= flow.total) {
    const last = (n - 1) * 3;
    out[0] = pos[last];
    out[1] = pos[last + 1];
    out[2] = pos[last + 2];
    return;
  }
  let lo = 0;
  let hi = n - 1;
  while (lo + 1 < hi) {
    const mid = (lo + hi) >> 1;
    if (lengths[mid] < d) lo = mid;
    else hi = mid;
  }
  const span = lengths[hi] - lengths[lo] || 1;
  const t = (d - lengths[lo]) / span;
  const a = lo * 3;
  const b = hi * 3;
  out[0] = pos[a] + (pos[b] - pos[a]) * t;
  out[1] = pos[a + 1] + (pos[b + 1] - pos[a + 1]) * t;
  out[2] = pos[a + 2] + (pos[b + 2] - pos[a + 2]) * t;
}

function flowShown(mesh) {
  let node = mesh;
  while (node) {
    if (!node.visible) return false;
    node = node.parent;
  }
  return true;
}

function tickFlows(dt) {
  const step = Number.isFinite(dt) && dt > 0 ? dt : 0;
  let shown = false;
  for (const mesh of flows) {
    if (!flowShown(mesh)) continue;
    shown = true;
    const flow = mesh.userData.flow;
    flow.time = wrapUnit(flow.time + step * flow.speed);
    const attr = mesh.geometry.attributes.position;
    for (let i = 0; i < flow.count; i++) {
      flowAt(flow, wrapUnit(flow.time + i / flow.count), flowScratch);
      attr.setXYZ(i, flowScratch[0], flowScratch[1], flowScratch[2]);
    }
    attr.needsUpdate = true;
  }
  return shown;
}

function addArc(group, from, to, color, lift, data) {
  const mesh = makeLink(from, to, color, lift);
  if (!mesh) return;
  mesh.userData = data;
  group.add(mesh);
  const flow = makeFlow(from, to, color, lift);
  if (flow) group.add(flow);
}

function applyMode() {
  syncLit();
  const overlay = mode === "overlay";
  for (const plinth of plinths) {
    const change = (byPackage.get(plinth.id) || {}).change || "same";
    const removed = change === "removed";
    const show = plinth.external || !removed || overlay;
    plinth.mesh.visible = show;
    const role = packageRole(plinth.id);
    const marked = overlay && change !== "same";
    if (plinth.external) {
      if (marked) plinth.material.color.copy(changeColor(change));
      else if (role === "dep") plinth.material.color.copy(ARC);
      else plinth.material.color.set(0x3f3f46);
      if (role === "dep" || marked) plinth.material.emissive.copy(plinth.material.color);
      else plinth.material.emissive.set(0x000000);
      plinth.material.emissiveIntensity = role === "dep" ? 0.7 : 0.2;
      if (role === "dim") {
        plinth.material.color.multiplyScalar(0.22);
        plinth.material.emissive.multiplyScalar(0.22);
      }
      continue;
    }
    if (!plinth.material) continue;
    if (role === "dep" && !marked) plinth.material.color.copy(ARC);
    else plinth.material.color.copy(marked ? changeColor(change) : plinthColor(plinth.box.depth, plinth.box.synthetic));
    plinth.material.emissive.set(0xffffff);
    plinth.material.emissiveIntensity = role === "dep" ? 0.62 : (marked ? 0.22 : 0.06);
    plinth.material.transparent = false;
    plinth.material.opacity = 1;
    plinth.material.depthWrite = true;
    plinth.material.wireframe = false;
    if (plinth.plate) plinth.plate.visible = !removed || overlay;
    if (role === "dim") {
      plinth.material.color.multiplyScalar(0.2);
      plinth.material.emissiveIntensity = 0.02;
      setPlateOpacity(plinth.plate, 0.35);
    } else {
      setPlateOpacity(plinth.plate, 1);
    }
  }
  recolor(solidMesh);
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
    else if (arcSubject) drawSelectionArcs(arcSubject.id, arcSubject.inbound);
    else clearGroup(selectArcs);
  }
  updateHUD();
  viewDirty = true;
  requestFrame();
}

// "dep" is a module on the other end of a module arc. It stays bright and
// takes the arc color. "dim" is everyone else while a selection is up.
function packageRole(id) {
  if (!lit) return "idle";
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
  if (!lit) return true;
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
    const links = [];
    for (const other of byEntity.values()) {
      if (other.entity.kind !== "method" || other.entity.parent !== found.entity.id || !other.box) continue;
      links.push({ target: other, change: other.entity.change || "same", step: null });
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

function litForEntity(found) {
  if (!found) return null;
  const entities = new Set([found.entity.id]);
  for (const link of entityLinks(found)) entities.add(link.target.entity.id);
  return { entities, packages: null };
}

function rawPackageCalls(id) {
  if (mode !== "overlay") return [];
  const links = [];
  for (const found of byEntity.values()) {
    if (!found.box || !found.pkg || found.pkg.id !== id) continue;
    const kind = found.entity.kind;
    if (kind !== "function" && kind !== "method") continue;
    for (const link of entityLinks(found)) {
      if (!link.change || link.change === "same") continue;
      links.push({ from: found, target: link.target, change: link.change, step: link.step });
    }
  }
  return links;
}

// Several callers of one function share a single arc.
// Added and deleted together read as changed.
function mergeCallLinks(links) {
  const byTarget = new Map();
  const rank = { same: 0, modified: 1, removed: 2, added: 3 };
  for (const link of links) {
    const key = link.target.entity.id;
    const prev = byTarget.get(key);
    if (!prev) {
      byTarget.set(key, { from: link.from, target: link.target, change: link.change, step: link.step, changes: new Set([link.change]) });
      continue;
    }
    prev.changes.add(link.change);
    if ((rank[link.change] || 0) >= (rank[prev.change] || 0)) {
      prev.from = link.from;
      prev.step = link.step;
      prev.change = link.change;
    }
  }
  for (const link of byTarget.values()) {
    if (link.changes.has("added") && link.changes.has("removed")) link.change = "modified";
  }
  return [...byTarget.values()];
}

function litForPackage(id, inbound) {
  const packages = new Set([id]);
  if (inbound) {
    for (const edge of packageEdges(id, true)) packages.add(edge.from);
    return { packages, entities: null, browse: null, links: null };
  }
  const raw = rawPackageCalls(id);
  if (raw.length) {
    const entities = new Set();
    for (const link of raw) {
      entities.add(link.from.entity.id);
      entities.add(link.target.entity.id);
      if (link.target.pkg) packages.add(link.target.pkg.id);
    }
    return { packages, entities, browse: null, links: mergeCallLinks(raw) };
  }
  for (const edge of packageEdges(id, false)) packages.add(edge.to);
  return { packages, entities: null, browse: id, links: null };
}

function syncLit() {
  if (focus) lit = litForEntity(focus);
  else if (entitySubject) lit = litForEntity(byEntity.get(entitySubject));
  else if (arcSubject) lit = litForPackage(arcSubject.id, arcSubject.inbound);
  else lit = null;
}

function drawEntityLinks(group, found) {
  const links = entityLinks(found).map((link) => ({ from: found, target: link.target, change: link.change, step: link.step }));
  drawCallLinks(group, links);
  if (!linkPoints || !linkPoints.length) {
    const top = towerTop(found);
    linkPoints = top ? [top] : [];
  }
}

function drawCallLinks(group, links) {
  clearGroup(group);
  const points = [];
  for (const link of links) {
    const from = towerTop(link.from);
    const to = towerTop(link.target);
    if (!from || !to) continue;
    if (points.length === 0) points.push(from);
    const std = !!(link.target.pkg && isStdPackage(link.target.pkg.id));
    const color = linkColor(link.change, std);
    const data = { kind: "call", entityId: link.target.entity.id, step: link.step, label: entityLabel(link.target.entity) };
    addArc(group, from, to, color, undefined, data);
    points.push(to);
  }
  linkPoints = points;
}

function changeColor(change) {
  if (change === "added") return ADD;
  if (change === "removed") return REMOVE;
  if (change === "modified") return MODIFY;
  if (change === "moved") return MOVED;
  return STONE;
}

function recolor(mesh) {
  if (!mesh) return;
  const color = new THREE.Color();
  mesh.userData.slots.forEach((slot, index) => {
    color.copy(entityColor(slot.entity));
    if (!entityIsLit(slot.id, slot.pkgId)) color.multiplyScalar(0.04);
    mesh.setColorAt(index, color);
  });
  mesh.instanceColor.needsUpdate = true;
}

function openness(box) {
  if (!lit && entered === box.id) return 1;
  const center = scratch.a.set(box.x + box.w / 2, box.y + box.h, box.z + box.d / 2);
  const dist = camera.position.distanceTo(center);
  const span = Math.max(box.w, box.d, 4);
  const near = span * 0.9;
  const far = span * 1.75;
  if (dist <= near) return 1;
  if (dist >= far) return 0;
  const t = (dist - near) / (far - near);
  const s = t * t * (3 - 2 * t);
  return 1 - s;
}

// One called function does not open the rest of its package.
// Only the functions on the call stay up. The others shut.
function slotOpen(slot) {
  if (!lit) return openByPkg.get(slot.pkgId) || 0;
  if (lit.entities) return lit.entities.has(slot.id) ? 1 : 0;
  if (lit.browse && slot.pkgId === lit.browse) return 1;
  return 0;
}

function refreshInstances() {
  if (!lit) {
    for (const box of laid.packages) openByPkg.set(box.id, openness(box));
  }
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
  arcSubject = { id, inbound: false };
  applyMode();
  if (fly) flyToPackage(id);
}

function selectExternal(id) {
  selected = { kind: "external", id };
  entered = null;
  entitySubject = null;
  arcSubject = { id, inbound: true };
  applyMode();
  const ext = laid.externals.find((item) => item.id === id);
  if (ext) flyTo(new THREE.Vector3(ext.x + 8, ext.y + 7, ext.z + 10), new THREE.Vector3(ext.x, ext.y, ext.z));
}

function selectEntity(id) {
  const found = byEntity.get(id);
  if (!found || !found.box) return;
  if (selected && selected.kind === "entity" && selected.id === id && (found.entity.kind === "function" || found.entity.kind === "method")) {
    enterFocus(found);
    return;
  }
  selected = { kind: "entity", id };
  entered = found.pkg.id;
  entitySubject = id;
  arcSubject = null;
  applyMode();
  const points = linkPoints && linkPoints.length ? linkPoints : [entityAnchor(found)];
  const pose = framePose(points, points[0]);
  flyTo(pose.pos, pose.target);
}

function enterFocus(found) {
  if (!returnPose) returnPose = { pos: camera.position.clone(), target: controls.target.clone() };
  focus = found;
  selected = { kind: "entity", id: found.entity.id };
  entered = found.pkg.id;
  entitySubject = found.entity.id;
  arcSubject = null;
  applyMode();
  const cam = focusCamera();
  flyTo(cam.pos, cam.target);
}

function dropFocus() {
  focus = null;
  returnPose = null;
  clearGroup(focusGroup);
  selectArcs.visible = true;
  arcGroup.visible = mode === "overlay";
}

function exitFocus() {
  const back = returnPose;
  focus = null;
  returnPose = null;
  clearGroup(focusGroup);
  applyMode();
  if (back) flyTo(back.pos, back.target);
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
  if (std) return STD_LINE;
  return ARC;
}

function drawSelectionArcs(id, inbound) {
  clearGroup(selectArcs);
  // One point for the whole fan. Per-edge clearance used to raise each end to
  // a different height, so the arcs neither met nor landed on the far module.
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
    addArc(selectArcs, from, to, color, clearanceLift(from, to), {
      kind: "dep",
      id: farId,
      label: (target && (target.name || target.id)) || farId,
      external: !!(target && target.external),
    });
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
  }
  // The bow crowns above the higher end. Keep it inside the frame with the landings.
  let top = origin[1];
  for (const p of points) top = Math.max(top, p[1]);
  points.push([origin[0], top + 14, origin[2]]);
  const pose = frameFan(points);
  flyTo(pose.pos, pose.target);
}

function frameFan(points) {
  let cx = 0;
  let cy = 0;
  let cz = 0;
  for (const p of points) {
    cx += p[0];
    cy += p[1];
    cz += p[2];
  }
  const n = points.length || 1;
  const look = { x: cx / n, y: cy / n, z: cz / n };
  const dir = { x: 0.32, y: 0.48, z: 0.78 };
  const aspect = camera.aspect > 0.05 ? camera.aspect : 1;
  const dist = fitDistance(points.map((p) => ({ x: p[0], y: p[1], z: p[2] })), look, dir, camera.fov, aspect, 0.84);
  const len = Math.hypot(dir.x, dir.y, dir.z) || 1;
  return {
    pos: new THREE.Vector3(look.x + (dir.x / len) * dist, look.y + (dir.y / len) * dist, look.z + (dir.z / len) * dist),
    target: new THREE.Vector3(look.x, look.y, look.z),
  };
}

function framePose(points, lookAt) {
  let minX = Infinity;
  let minY = Infinity;
  let minZ = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  let maxZ = -Infinity;
  for (const p of points) {
    minX = Math.min(minX, p[0]);
    minY = Math.min(minY, p[1]);
    minZ = Math.min(minZ, p[2]);
    maxX = Math.max(maxX, p[0]);
    maxY = Math.max(maxY, p[1]);
    maxZ = Math.max(maxZ, p[2]);
  }
  const span = Math.max(maxX - minX, maxZ - minZ, (maxY - minY) * 1.4, points.length < 2 ? 24 : 18);
  const look = lookAt
    ? new THREE.Vector3(lookAt[0], lookAt[1] + span * 0.08, lookAt[2])
    : new THREE.Vector3((minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2);
  const dist = span * 0.92;
  // In front of the city (+z) and a little to the right (+x), above the arcs.
  return {
    pos: new THREE.Vector3(look.x + dist * 0.32, look.y + dist * 0.48, look.z + dist * 0.78),
    target: look,
  };
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
  if (k === 1) tween = null;
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
  const from = linkPoints && linkPoints[0];
  if (from) addBodyBars(from, found.entity, mode === "overlay");
  focus.points = linkPoints ? linkPoints.slice() : [];
}

function addBodyBars(origin, entity, overlay) {
  const before = entity.bodyBytesBefore == null ? null : entity.bodyBytesBefore;
  const after = entity.change === "removed" ? 0 : (entity.bodyBytes || 0);
  const maxBytes = Math.max(before || 0, after, 1);
  const bar = (bytes, x, color) => {
    const height = 1.4 + 4.2 * (bytes / maxBytes);
    const mesh = new THREE.Mesh(
      new THREE.BoxGeometry(1.1, height, 1.1),
      new THREE.MeshLambertMaterial({ color, emissive: color, emissiveIntensity: 0.45 }),
    );
    mesh.position.set(origin[0] + x, origin[1] + height / 2, origin[2]);
    focusGroup.add(mesh);
  };
  if (entity.change !== "removed") bar(after, -2.6, overlay && entity.change !== "same" ? changeColor(entity.change) : STONE);
  if (overlay && before != null && (entity.change === "modified" || entity.change === "removed")) {
    bar(before || 0.001, entity.change === "removed" ? -2.6 : -4.4, REMOVE);
  }
}

function focusCamera() {
  const points = (focus && focus.points) || [];
  const look = points[0] || [0, 0, 0];
  return framePose(points.length ? points : [look], look);
}

function clearGroup(group) {
  for (const child of [...group.children]) {
    child.traverse((obj) => {
      if (obj.userData && obj.userData.flow) {
        const index = flows.indexOf(obj);
        if (index >= 0) flows.splice(index, 1);
      }
      if (obj.geometry) obj.geometry.dispose();
      if (obj.material) {
        const list = Array.isArray(obj.material) ? obj.material : [obj.material];
        for (const mat of list) {
          if (mat.map) mat.map.dispose();
          mat.dispose();
        }
      }
    });
    group.remove(child);
  }
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
  if (idleSpin) {
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
  if (found.kind === "call") {
    const target = found.entityId ? byEntity.get(found.entityId) : null;
    if (!target) return;
    focus = null;
    clearGroup(focusGroup);
    if (target.entity.kind === "function" || target.entity.kind === "method") enterFocus(target);
    else {
      returnPose = null;
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
  if (focus) return;
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
  if (sceneDoc && !sceneDoc.diff) {
    hud.note.textContent = mode === "overlay" ? "This view is one snapshot. Pass -range to lay a diff on the city." : "";
  } else if (mode === "overlay") {
    hud.note.textContent = "Added is green, deleted is red, changed is yellow.";
  } else {
    hud.note.textContent = "";
  }
  renderCrumb();
  renderDetail();
}

function renderCrumb() {
  const parts = [];
  const root = sceneDoc && sceneDoc.root;
  if (root) parts.push({ id: root, label: byPackage.get(root)?.name || root, kind: "package" });
  if (entered && entered !== root) {
    const pkg = byPackage.get(entered);
    parts.push({ id: entered, label: pkg ? pkg.name : entered, kind: "package" });
  }
  if (focus) parts.push({ id: focus.entity.id, label: entityLabel(focus.entity), kind: "entity" });
  hud.crumb.replaceChildren();
  parts.forEach((part, index) => {
    if (index) hud.crumb.append("  /  ");
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = part.label;
    button.addEventListener("click", () => {
      if (part.kind === "entity") return;
      if (focus) exitFocus();
      selectPackage(part.id, true);
    });
    hud.crumb.append(button);
  });
}

function renderDetail() {
  stopMarches();
  hud.detail.replaceChildren();
  if (focus) {
    hud.detail.append(focusDetail(focus.entity));
    hud.detail.append(roster(focus.pkg));
    return;
  }
  if (selected && selected.kind === "entity") {
    const found = byEntity.get(selected.id);
    if (found) {
      hud.detail.append(entityDetail(found.entity));
      hud.detail.append(roster(found.pkg));
    }
    return;
  }
  const id = (selected && selected.id) || (sceneDoc && sceneDoc.root);
  const pkg = id ? byPackage.get(id) : null;
  if (pkg) hud.detail.append(packageDetail(pkg));
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
  wrap.append(roster(pkg));
  return wrap;
}

function declaredEntities(pkg) {
  return (pkg.entities || []).filter((entity) => entity.kind === "type" || entity.kind === "function" || entity.kind === "method");
}

function changeTally(entities) {
  const line = document.createElement("p");
  line.className = "tally";
  line.append(entities.length + " declarations");
  if (mode !== "overlay") return line;
  const added = entities.filter((entity) => entity.change === "added").length;
  const removed = entities.filter((entity) => entity.change === "removed").length;
  const modified = entities.filter((entity) => entity.change === "modified").length;
  if (added) line.append(tallySpan("added", added + " added"));
  if (removed) line.append(tallySpan("removed", removed + " deleted"));
  if (modified) line.append(tallySpan("modified", modified + " changed"));
  return line;
}

function tallySpan(kind, text) {
  const span = document.createElement("span");
  span.className = kind;
  span.textContent = text;
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

function roster(pkg) {
  const wrap = document.createElement("div");
  if (!pkg) return wrap;
  const children = childPackages(pkg.id);
  children.sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id));
  const direct = splitChanges(children, (item) => item.change);
  const nested = changedInSubtree(pkg).filter((item) => item.parent !== pkg.id);
  const packageList = [...direct.hot, ...nested];
  packageList.sort((a, b) => rowLabel(a, pkg.id).localeCompare(rowLabel(b, pkg.id)));
  const sections = [{
    label: "Packages",
    hot: mode === "overlay" ? packageList : children,
    same: mode === "overlay" ? direct.same : [],
    row: (item) => packageRow(item, pkg.id),
  }];
  const groups = [
    ["Types", "type"],
    ["Functions", "function"],
    ["Methods", "method"],
  ];
  for (const [label, kind] of groups) {
    const list = declaredEntities(pkg).filter((entity) => entity.kind === kind);
    list.sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)));
    const split = splitChanges(list, (item) => item.change);
    sections.push({ label, hot: split.hot, same: split.same, row: entityRow });
  }
  const parts = sections;
  const paint = (pick) => {
    for (const section of parts) {
      const rows = section[pick];
      if (!rows.length) continue;
      const heading = document.createElement("h3");
      heading.textContent = section.label;
      wrap.append(heading);
      for (const item of rows) wrap.append(section.row(item));
    }
  };
  paint("hot");
  const hidden = parts.reduce((count, section) => count + section.same.length, 0);
  if (mode === "overlay" && hidden) {
    const fold = document.createElement("button");
    fold.type = "button";
    fold.className = "fold";
    fold.textContent = showUnchanged ? "hide unchanged" : "expand unchanged";
    const count = document.createElement("span");
    count.textContent = String(hidden);
    fold.append(" ", count);
    fold.addEventListener("click", () => {
      showUnchanged = !showUnchanged;
      renderDetail();
    });
    wrap.append(fold);
    if (showUnchanged) paint("same");
  }
  return wrap;
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

function packageRow(pkg, ownerId) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "row";
  if (selected && selected.id === pkg.id) button.classList.add("on");
  const name = clipText(rowLabel(pkg, ownerId));
  button.append(name);
  attachMarquee(button, name);
  const mark = changeMark(pkg.change);
  if (mark) {
    mark.classList.add("mark");
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
  const mark = changeMark(entity.change);
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

function entityDetail(entity) {
  const wrap = document.createElement("div");
  const title = document.createElement("h2");
  const titleClip = clipText(entityLabel(entity));
  title.append(titleClip);
  attachMarquee(title, titleClip);
  const mark = changeMark(entity.change);
  if (mark) title.append(mark);
  wrap.append(title);
  const meta = document.createElement("p");
  meta.textContent = [entity.kind, entity.file].filter(Boolean).join(" · ");
  wrap.append(meta);
  if (entity.kind === "function" || entity.kind === "method") {
    const size = document.createElement("p");
    size.textContent = sizeText(entity);
    wrap.append(size);
    const hint = document.createElement("p");
    hint.textContent = "Click again or press enter to open the call diff.";
    wrap.append(hint);
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
  wrap.append(title);
  const meta = document.createElement("p");
  const steps = (entity.calls || []).filter((step) => declaredTarget(step));
  const added = steps.filter((step) => step.change === "added").length;
  const removed = steps.filter((step) => step.change === "removed").length;
  meta.append(sizeText(entity));
  if (mode === "overlay") {
    meta.append("  ");
    const plus = document.createElement("span");
    plus.className = "added";
    plus.textContent = "+" + added;
    const minus = document.createElement("span");
    minus.className = "removed";
    minus.textContent = " −" + removed;
    meta.append(plus, minus, " calls");
  } else {
    meta.append("   " + steps.length + " calls");
  }
  wrap.append(meta);
  const list = document.createElement("ul");
  const show = mode === "overlay" ? steps.filter((step) => step.change !== "same") : steps.slice(0, 40);
  if (!show.length) {
    const empty = document.createElement("p");
    empty.textContent = steps.length ? "The call list is unchanged." : "No calls to a declaration in this codebase.";
    wrap.append(empty);
  }
  for (const step of show) {
    const li = document.createElement("li");
    li.className = mode === "overlay" ? step.change : "";
    const mark = step.change === "added" ? "+ " : step.change === "removed" ? "− " : "";
    const line = clipText((mode === "overlay" ? mark : "") + (step.expr || step.name || "call"));
    li.append(line);
    attachMarquee(li, line);
    list.append(li);
  }
  if (mode !== "overlay" && steps.length > 40) {
    const more = document.createElement("p");
    more.textContent = steps.length - 40 + " more calls continue along the path.";
    wrap.append(more);
  }
  wrap.append(list);
  return wrap;
}

function sizeText(entity) {
  const after = entity.change === "removed" ? 0 : (entity.bodyBytes || 0);
  if (mode === "overlay" && entity.bodyBytesBefore != null && entity.bodyBytesBefore !== after) {
    return "body  " + entity.bodyBytesBefore + " → " + after + " bytes";
  }
  if (entity.kind === "function" || entity.kind === "method") return "body  " + (entity.bodyBytes || 0) + " bytes";
  return "";
}

function changeMark(change) {
  if (!change || change === "same" || mode !== "overlay") return null;
  const span = document.createElement("span");
  if (change === "added") {
    span.className = "added";
    span.textContent = "added";
  } else if (change === "removed") {
    span.className = "removed";
    span.textContent = "deleted";
  } else if (change === "modified") {
    span.className = "modified";
    span.textContent = "changed";
  } else if (change === "moved") {
    span.className = "moved";
    span.textContent = "moved";
  } else {
    return null;
  }
  return span;
}

function resetView() {
  focus = null;
  returnPose = null;
  clearGroup(focusGroup);
  entered = null;
  entitySubject = null;
  arcSubject = null;
  selected = null;
  ring.visible = false;
  applyMode();
  cityPose = frameCity();
  applyFitLimits(cityPose);
  flyTo(cityPose.pos, cityPose.target);
}

function goBack() {
  if (focus) {
    exitFocus();
    return;
  }
  if (entered || entitySubject || arcSubject) {
    entered = null;
    entitySubject = null;
    arcSubject = null;
    selected = sceneDoc.root ? { kind: "package", id: sceneDoc.root } : null;
    ring.visible = false;
    applyMode();
    cityPose = frameCity();
    applyFitLimits(cityPose);
    flyTo(cityPose.pos, cityPose.target);
    return;
  }
  selected = null;
  ring.visible = false;
  entitySubject = null;
  arcSubject = null;
  applyMode();
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

function axis(positive, negative) {
  const pos = positive.some((key) => held.has(key));
  const neg = negative.some((key) => held.has(key));
  return (pos ? 1 : 0) - (neg ? 1 : 0);
}

function flyCamera(dt, now) {
  if (typingSearch()) {
    idleSpin = false;
    return false;
  }
  // ArrowUp is W. ArrowRight is D. Looking down −z, right is +x.
  const forward = axis(["w", "arrowup"], ["s", "arrowdown"]);
  const strafe = axis(["d", "arrowright"], ["a", "arrowleft"]);
  let yaw = (held.has("q") ? 1 : 0) - (held.has("e") ? 1 : 0);
  const zoom = (held.has("+") ? 1 : 0) - (held.has("-") ? 1 : 0);
  const userMove = forward || strafe || yaw || zoom;
  if (userMove) endIntro();
  const idleReady = !introSpin && lastActivity && now - lastActivity >= IDLE_SPIN_MS;
  idleSpin = !userMove && !tween && ((introSpin && !!cityPose) || idleReady);
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
  if ((moved || viewDirty) && laid) {
    refreshInstances();
    settledPos.copy(camera.position);
    settledTarget.copy(controls.target);
  }
  const flowing = tickFlows(dt);
  if (idleSpin) {
    if (pointerDirty) onHover(null);
    pointerDirty = false;
  } else if (pointerDirty || moved) {
    onHover(hitTest());
    pointerDirty = false;
  }
  if (moved || flowing || viewDirty) {
    renderer.render(scene, camera);
    viewDirty = false;
  }
  if (moved || flowing || introSpin || tween || held.size) requestFrame();
  else parkLoop();
}

hud.overview.addEventListener("click", () => { mode = "overview"; applyMode(); });
hud.changes.addEventListener("click", () => { mode = "overlay"; applyMode(); });

renderer.domElement.addEventListener("pointerdown", (event) => {
  pointerDown = { x: event.clientX, y: event.clientY };
  tween = null;
});
renderer.domElement.addEventListener("pointermove", (event) => {
  const rect = renderer.domElement.getBoundingClientRect();
  pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
  pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
  pointerDirty = true;
  requestFrame();
});
renderer.domElement.addEventListener("pointerup", (event) => {
  if (!pointerDown) return;
  const moved = Math.hypot(event.clientX - pointerDown.x, event.clientY - pointerDown.y);
  pointerDown = null;
  if (moved > 5) return;
  activate(describeHit(hitTest()));
});

function closeSearch() {
  searchHits = [];
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
}

function onSearch() {
  searchHits = rankMatches(catalog, hud.search.value, 12);
  searchCursor = 0;
  renderSearch();
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

window.addEventListener("pointerdown", () => { noteActivity(); endIntro(); requestFrame(); });
window.addEventListener("pointermove", noteActivity);
window.addEventListener("wheel", () => { noteActivity(); endIntro(); requestFrame(); }, { passive: true });

window.addEventListener("keydown", (event) => {
  requestFrame();
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
  const flyKey = flyToken(event.key);
  if (!typing && hud.legend.hidden && flyKey && !event.metaKey && !event.ctrlKey && !event.altKey) {
    held.add(flyKey);
    event.preventDefault();
  }
  if (typing) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!searchHits.length) return;
      const step = event.key === "ArrowDown" ? 1 : -1;
      searchCursor = (searchCursor + step + searchHits.length) % searchHits.length;
      renderSearch();
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      if (searchHits[searchCursor]) goToResult(searchHits[searchCursor]);
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
  if (event.key === "1") { mode = "overview"; applyMode(); }
  if (event.key === "2") { mode = "overlay"; applyMode(); }
  if (event.key === "Escape") goBack();
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
function setLegend(open) {
  hud.legend.hidden = !open;
}

hud.collapse.addEventListener("mouseenter", () => setSide(false));
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

main();
