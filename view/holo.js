// The holograms: a sphere of light over every module the city is highlighting,
// with the module's name curved round its equator. In a tight city the rooftop
// plate is a few pixels of sign hidden behind the neighbours' shoulders, and
// the name is the one thing a selection has to say — so the sphere is drawn at
// a fixed size on screen whatever the tower is and however far the camera
// stands, and every module in the lit set gets one, not just the one that was
// clicked: watching a function's callers, the packages on both ends of every
// arc are named at once.
//
// The sphere the user selected is the primary: full size, with the two
// shockwaves that break the roof when it appears. The rest are half a size
// smaller so the subject still reads as the subject, and they fade without a
// wave.
//
// Everything in it is light. The shell, the band and the shockwaves are
// additive, so nothing needs sorting, a depth write or a second pass, and the
// band carries the name as white ink on a transparent sheet with the material's
// colour as the ink — the same trick the rooftop plates use, and the reason a
// skin repaints every hologram by writing two colour uniforms. The band turns
// to face the eye on both axes so the name is legible from any pitch, and it is
// drawn without a depth test: a label that a tower can stand in front of is not
// a label.
import * as THREE from "three";
import { signText } from "./signs.js";

// The primary sphere's diameter on screen, in CSS pixels. Wide enough that a
// 16 character module name is still read at a glance.
export const HOLO_PX = 180;
// A halo's, so the module the user picked still reads as the subject.
export const HALO_PX = 146;
// How many spheres can be up at once. Past this the far end of a wide lit set
// would be a sky of glowing balls: the pool never grows beyond it, and a module
// that cannot get a sphere is simply not drawn.
export const MAX_HOLOS = 18;
// Two spheres are too close on screen when their centres are nearer than this
// much of a diameter: a little overlap reads as a cluster of light, a sphere
// inside another reads as neither name. The one that cannot have its own place
// is the one that is not drawn (the selection always has one).
const CROWD = 0.84;
// How fast a sphere gives way to, or takes back, a place on screen, in seconds.
const CROWD_S = 0.15;
// The band covers this much of the equator, centred on the eye: enough
// curvature to read as a ring of text round a sphere, and the ends stay
// inside the silhouette instead of wrapping behind it.
const BAND_ARC = (150 * Math.PI) / 180;
const BAND_R = 1.015;
const BAND_H = 0.5;
// The sphere's centre above its foot, in radii, once it has risen.
const RISE = 1.05;
// The shockwaves the primary breaks the floor with: how many, the seconds
// between one starting and the next, and how long one runs.
const WAVES = 2;
const WAVE_GAP = 0.17;
const WAVE_S = 0.85;
const WAVE_MAX = 2.5;
const APPEAR_S = 0.5;
const GONE_S = 0.3;
const SWEEP_S = 2.6;
// A sphere drawn at a fixed size on screen grows without limit as the camera
// pulls out: over a whole city it would be a planet over a shed. Between these
// two world radii it gives up the constant size and keeps to the scene.
const MAX_RADIUS = 8;
const MIN_RADIUS = 0.35;
// A module's sheet: 5.33 to 1, the arc's length over its height, as sharp as
// the sphere is wide on screen and no sharper.
const SHEET_W = 1024;
const SHEET_H = 192;
const FONT = "600 ###px ui-monospace, monospace";

function sheetCtx() {
  const canvas = document.createElement("canvas");
  canvas.width = SHEET_W;
  canvas.height = SHEET_H;
  return canvas.getContext("2d");
}

// The name, painted across the band's whole sheet: a long one shrinks to fit
// the arc instead of running off its ends. A glow pass behind the ink is what
// makes the letters read as light rather than as a decal on the sphere. The ink
// is white whatever the skin says: the material's colour is the ink, so a skin
// changes every name without a sheet being painted again.
export function paintBand(ctx, name) {
  const text = signText(name);
  ctx.clearRect(0, 0, SHEET_W, SHEET_H);
  const font = (size) => FONT.replace("###", size.toFixed(1));
  let size = SHEET_H * 0.72;
  ctx.font = font(size);
  const room = SHEET_W * 0.82;
  const want = ctx.measureText(text).width;
  if (want > room) {
    size *= room / want;
    ctx.font = font(size);
  }
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillStyle = "#ffffff";
  ctx.shadowColor = "#ffffff";
  ctx.shadowBlur = size * 0.34;
  ctx.fillText(text, SHEET_W / 2, SHEET_H / 2);
  ctx.shadowBlur = 0;
  ctx.fillText(text, SHEET_W / 2, SHEET_H / 2);
}

// The shell: a thin fresnel rim, the latitude and longitude lines that make it
// read as a globe rather than a bubble, and the sweep that travels up it. The
// lines are drawn in the shader, so a sphere is one draw call and no lines are
// built per selection.
const SHELL_VERTEX = `
  varying vec3 vNormal;
  varying vec3 vView;
  varying vec3 vPoint;
  void main() {
    vNormal = normalize(normalMatrix * normal);
    vec4 mv = modelViewMatrix * vec4(position, 1.0);
    vView = normalize(-mv.xyz);
    vPoint = position;
    gl_Position = projectionMatrix * mv;
  }
`;

const SHELL_FRAGMENT = `
  uniform vec3 uColor;
  uniform float uFade;
  uniform float uSweep;
  uniform float uTime;
  varying vec3 vNormal;
  varying vec3 vView;
  varying vec3 vPoint;
  // A line at every whole step of x, one pixel wide however close the sphere
  // is: the derivative is the pixel's own footprint in x.
  float line(float x) {
    float step = max(fwidth(x), 1e-5);
    float d = min(fract(x), 1.0 - fract(x)) / step;
    return 1.0 - smoothstep(0.0, 1.1, d);
  }
  void main() {
    float rim = pow(1.0 - abs(dot(normalize(vNormal), normalize(vView))), 2.2);
    float lat = line(vPoint.y * 3.0);
    float lon = line(atan(vPoint.z, vPoint.x) * 1.9098593);
    // The lines are only painted on the sphere's edge: over the middle they
    // would sit on top of the name, and light over light is mush.
    float grid = max(lat, lon) * (0.3 + 0.7 * rim);
    // The sweep: a band of light that climbs the sphere once every cycle, and
    // a slow breath under everything else.
    float sweep = exp(-pow((vPoint.y - uSweep) * 4.5, 2.0));
    float breath = 0.88 + 0.12 * sin(uTime * 6.2831853 / 1.8);
    float a = (rim * 0.44 + grid * 0.34 + sweep * 0.28) * uFade * breath;
    if (a < 0.003) discard;
    gl_FragColor = vec4(uColor, a);
    #include <colorspace_fragment>
  }
`;

// One sphere: its own sheet and name, its own share of the clock, and its own
// place under the camera. The geometry is shared with every other sphere — the
// pool is bounded, so the draw calls are what costs, not the buffers.
function shellMaterial() {
  return new THREE.ShaderMaterial({
    uniforms: {
      uColor: { value: new THREE.Color(0x23afd0) },
      uFade: { value: 0 },
      uSweep: { value: 2.4 },
      uTime: { value: 0 },
    },
    vertexShader: SHELL_VERTEX,
    fragmentShader: SHELL_FRAGMENT,
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
    fog: false,
  });
}

export function createHolo() {
  // Everything the holograms are made of is built once and shared: one sphere,
  // one band, one shockwave ring.
  const shellGeom = new THREE.SphereGeometry(1, 64, 44);
  const bandGeom = new THREE.CylinderGeometry(BAND_R, BAND_R, BAND_H, 128, 1, true, -BAND_ARC / 2, BAND_ARC);
  const waveGeom = new THREE.RingGeometry(0.86, 1, 96);

  const group = new THREE.Group();
  group.renderOrder = 6;
  const pool = new Map();
  const ink = new THREE.Color(0x23afd0);
  const bandInk = new THREE.Color(0xffffff);
  const centre = new THREE.Vector3();
  let clock = 0;

  function make(id) {
    const ctx = sheetCtx();
    const texture = new THREE.CanvasTexture(ctx.canvas);
    texture.colorSpace = THREE.SRGBColorSpace;
    const shell = new THREE.Mesh(shellGeom, shellMaterial());
    shell.frustumCulled = false;
    const band = new THREE.Mesh(bandGeom, new THREE.MeshBasicMaterial({
      map: texture, transparent: true, depthWrite: false, depthTest: false, fog: false, toneMapped: false,
      color: bandInk, blending: THREE.AdditiveBlending, side: THREE.FrontSide,
    }));
    band.frustumCulled = false;
    band.renderOrder = 8;
    band.material.color.copy(bandInk);
    const pivot = new THREE.Group();
    pivot.add(band);
    const base = new THREE.Group();
    base.position.y = -(RISE - 0.03);
    const waves = [];
    for (let i = 0; i < WAVES; i++) {
      const wave = new THREE.Mesh(waveGeom, new THREE.MeshBasicMaterial({
        color: bandInk, transparent: true, depthWrite: false, fog: false, toneMapped: false,
        opacity: 0, blending: THREE.AdditiveBlending, side: THREE.DoubleSide,
      }));
      wave.rotation.x = -Math.PI / 2;
      wave.renderOrder = 7;
      wave.visible = false;
      base.add(wave);
      waves.push(wave);
    }
    const holder = new THREE.Group();
    holder.visible = false;
    holder.renderOrder = 6;
    holder.add(shell, pivot, base);
    group.add(holder);
    const inst = {
      id, ctx, texture, shell, band, pivot, base, waves, group: holder,
      name: null, spot: null, radius: 1, px: HOLO_PX,
      shown: false, show: 0, age: 0, primary: false,
      rank: 0, crowd: 1, crowdWant: 1,
    };
    paintBand(ctx, "");
    pool.set(id, inst);
    return inst;
  }

  // The sphere for a module, or none when the pool is full and every sphere it
  // holds is still up: a module that cannot have one is not drawn.
  function instanceFor(id) {
    const had = pool.get(id);
    if (had) return had;
    if (pool.size < MAX_HOLOS) return make(id);
    for (const [oldId, inst] of pool) {
      if (inst.shown || inst.show > 0.002) continue;
      pool.delete(oldId);
      inst.id = id;
      pool.set(id, inst);
      return inst;
    }
    return null;
  }

  return {
    group,
    // The skin's own two colours: the selection's accent for the sphere and the
    // shockwaves, and the sign ink for the name itself, which has to stay
    // legible on any background a skin puts behind it.
    setInk(shellColor, nameColor) {
      ink.set(shellColor);
      bandInk.set(nameColor);
      for (const inst of pool.values()) {
        inst.shell.material.uniforms.uColor.value.copy(ink);
        inst.band.material.color.copy(bandInk);
        for (const wave of inst.waves) wave.material.color.copy(ink);
      }
    },
    // The modules the city is highlighting, in the order they were named. One
    // of them is the primary — the module the user picked — and only it breaks
    // the floor. Everything else that was up and is not named here fades out.
    sync(list) {
      const wanted = new Set();
      for (const item of list) {
        if (wanted.size >= MAX_HOLOS) break;
        const inst = instanceFor(item.id);
        if (!inst) continue;
        wanted.add(item.id);
        if (inst.name !== item.name) {
          inst.name = item.name;
          paintBand(inst.ctx, item.name);
          inst.texture.needsUpdate = true;
        }
        inst.rank = wanted.size - 1;
        inst.primary = !!item.primary;
        inst.px = item.primary ? HOLO_PX : HALO_PX;
        inst.spot = { x: item.x, y: item.y, z: item.z, apex: item.apex || 0 };
        if (!inst.shown) {
          inst.show = 0;
          inst.age = 0;
        }
        inst.shown = true;
        inst.group.visible = true;
      }
      for (const inst of pool.values()) if (!wanted.has(inst.id)) inst.shown = false;
    },
    // True while any sphere has something left to do, so the render loop keeps
    // asking for frames until the last one has faded out for good.
    live() {
      for (const inst of pool.values()) {
        if (inst.shown || inst.show > 0.002) return true;
      }
      return false;
    },
    // Put every sphere over its module at the size the screen asks for: the
    // top of the module's own call arcs and the tallest roof the city is drawn
    // with are the floor it stands on, so a name never sits in the way of the
    // thing it is naming, and nothing it is showing off ends up buried in a
    // tower beside it. `width` and `height` are the canvas in CSS pixels, which
    // is what a size fixed on screen is measured in.
    //
    // Neighbours stand on the same floor and can end up on the same spot: two
    // modules side by side are a hundred pixels apart when the camera is on
    // them. The spheres are placed in priority order — the selection first,
    // then the order they were named — and one that has no room of its own is
    // the one that gives way, the way the bird view's district labels do. It
    // fades rather than blinking out, so a camera move that frees a place is
    // not a flicker.
    place(camera, width, height) {
      const px = Math.max(1, height);
      const wide = Math.max(1, width);
      const seen = [];
      const order = [...pool.values()].filter((inst) => inst.group.visible && inst.spot).sort((a, b) => a.rank - b.rank);
      for (const inst of order) {
        const spot = inst.spot;
        const dist = camera.position.distanceTo(spot);
        const perPixel = (2 * Math.tan((camera.fov * Math.PI) / 360) * dist) / px;
        inst.radius = Math.min(MAX_RADIUS, Math.max(MIN_RADIUS, perPixel * inst.px * 0.5));
        const rest = Math.max(spot.y, spot.apex) + inst.radius * RISE;
        const rise = 1 - Math.pow(1 - inst.show, 3);
        inst.group.position.set(spot.x, spot.y + (rest - spot.y) * rise, spot.z);
        inst.group.scale.setScalar(inst.radius);
        inst.pivot.lookAt(camera.position);
        inst.crowdWant = 1;
        const at = centre.set(spot.x, spot.y + (rest - spot.y) * rise, spot.z).project(camera);
        const sx = (at.x * 0.5 + 0.5) * wide;
        const sy = (-at.y * 0.5 + 0.5) * px;
        const room = inst.px * 0.5;
        const crowd = inst.rank > 0 && seen.some((p) => Math.abs(p.x - sx) < p.room + room && Math.abs(p.y - sy) < p.room + room);
        if (crowd) {
          inst.crowdWant = 0;
          continue;
        }
        if (inst.shown) seen.push({ x: sx, y: sy, room: room * CROWD });
      }
    },
    // A frame's worth of show, for every sphere that is up or fading.
    tick(dt) {
      const step = dt > 0 ? dt : 0;
      clock += step;
      for (const inst of pool.values()) {
        if (!inst.group.visible && !inst.shown) continue;
        if (inst.shown) inst.age += step;
        inst.show = Math.min(1, Math.max(0, inst.show + (inst.shown ? step / APPEAR_S : -step / GONE_S)));
        if (!inst.shown && inst.show <= 0.002) {
          inst.group.visible = false;
          continue;
        }
        const fade = 1 - Math.pow(1 - inst.show, 3);
        const crowd = inst.crowd + (inst.crowdWant - inst.crowd) * Math.min(1, step / CROWD_S);
        inst.crowd = crowd;
        const lit = fade * crowd;
        inst.shell.material.uniforms.uFade.value = lit;
        inst.shell.material.uniforms.uTime.value = clock;
        // The sweep runs from below the sphere to above it and round again.
        inst.shell.material.uniforms.uSweep.value = (((clock / SWEEP_S) * 2.6) % 2.6) - 1.3;
        inst.shell.scale.setScalar(1 + 0.035 * Math.sin((clock / 1.8) * Math.PI * 2));
        // The name is the one thing that must not flicker: the band fades in
        // and out with the rest, and nothing else touches its opacity.
        inst.band.material.opacity = lit;
        for (let i = 0; i < inst.waves.length; i++) {
          const wave = inst.waves[i];
          const at = inst.age - i * WAVE_GAP;
          const q = inst.primary && at > 0 && at < WAVE_S ? at / WAVE_S : -1;
          wave.visible = q >= 0;
          if (!wave.visible) continue;
          wave.scale.setScalar(0.5 + WAVE_MAX * q);
          wave.material.opacity = Math.pow(1 - q, 1.7) * 0.7 * lit;
        }
      }
    },
  };
}
