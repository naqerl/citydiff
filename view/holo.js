// The hologram: a sphere of light over the selected module, with its name
// curved round the equator. In a tight city the rooftop plate is a few pixels
// of sign hidden behind the neighbours' shoulders, and the name is the one
// thing a selection has to say, so the hologram is drawn at a fixed size on
// screen whatever the tower is and however far the camera stands. It rises out
// of the roof when the module is picked, clear of the arcs that bow over it.
//
// The band turns to face the eye on both axes, so the name is legible from any
// pitch, and it is drawn without a depth test: a label that a tower can stand
// in front of is not a label. Everything else in it is light — the shell, the
// band and the shockwaves are additive, so nothing needs sorting, a depth write
// or a second pass, and the band carries the name as white ink on a transparent
// sheet with the material's colour as the ink, the same trick the rooftop
// plates use and the reason a skin repaints the whole hologram by writing two
// colour uniforms.
import * as THREE from "three";
import { signText } from "./signs.js";

// The sphere's diameter on screen, in CSS pixels. Wide enough that a 16
// character module name is still read at a glance.
export const HOLO_PX = 180;
// The band covers this much of the equator, centred on the eye: enough
// curvature to read as a ring of text round a sphere, and the ends stay
// inside the silhouette instead of wrapping behind it.
const BAND_ARC = (150 * Math.PI) / 180;
const BAND_R = 1.015;
const BAND_H = 0.5;
// The sphere's centre above its foot, in radii, once it has risen.
const RISE = 1.05;
// The shockwaves that expand from under it when the module is picked: how
// many, the seconds between one starting and the next, and how long one runs.
const WAVES = 2;
const WAVE_GAP = 0.17;
const WAVE_S = 0.85;
const WAVE_MAX = 2.5;
const APPEAR_S = 0.5;
const GONE_S = 0.3;
const SWEEP_S = 2.6;
const PULSE_S = 1.8;
// A sphere drawn at a fixed size on screen grows without limit as the camera
// pulls out: over a whole city it would be a planet over a shed. Between these
// two world radii it gives up the constant size and keeps to the scene.
const MAX_RADIUS = 8;
const MIN_RADIUS = 0.35;
// The band's sheet: 5.33 to 1, the arc's length over its height, so a texel is
// square and the name is as sharp as the plate's own ink.
const SHEET_W = 2048;
const SHEET_H = 384;
const FONT = "600 ###px ui-monospace, monospace";

function sheetCtx() {
  const canvas = document.createElement("canvas");
  canvas.width = SHEET_W;
  canvas.height = SHEET_H;
  return canvas.getContext("2d");
}

// The name, painted across the band's whole sheet: a long one shrinks to fit
// the arc instead of running off its ends. A glow pass behind the ink is what
// makes the letters read as light rather than as a decal on the sphere.
export function paintBand(ctx, name, ink) {
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
  ctx.fillStyle = ink;
  ctx.shadowColor = ink;
  ctx.shadowBlur = size * 0.34;
  ctx.fillText(text, SHEET_W / 2, SHEET_H / 2);
  ctx.shadowBlur = 0;
  ctx.fillText(text, SHEET_W / 2, SHEET_H / 2);
}

// The shell: a thin fresnel rim, the latitude and longitude lines that make it
// read as a globe rather than a bubble, and the sweep that travels up it. The
// lines are drawn in the shader, so the sphere is one draw call and no lines
// are built per selection.
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

function shellMaterial(ink) {
  return new THREE.ShaderMaterial({
    uniforms: {
      uColor: { value: ink.clone() },
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
  const ctx = sheetCtx();
  const texture = new THREE.CanvasTexture(ctx.canvas);
  texture.colorSpace = THREE.SRGBColorSpace;

  const ink = new THREE.Color(0x23afd0);
  const bandInk = new THREE.Color(0xffffff);
  const group = new THREE.Group();
  group.visible = false;
  group.renderOrder = 6;

  const shell = new THREE.Mesh(new THREE.SphereGeometry(1, 64, 44), shellMaterial(ink));
  shell.frustumCulled = false;
  group.add(shell);

  // The band is turned to point at the eye every frame, its up kept the
  // world's so the text stays upright however the view is pitched, and it is
  // drawn over everything: a tower in front of the hologram must not swallow
  // the name it is there to show.
  const band = new THREE.Mesh(
    new THREE.CylinderGeometry(BAND_R, BAND_R, BAND_H, 128, 1, true, -BAND_ARC / 2, BAND_ARC),
    new THREE.MeshBasicMaterial({
      map: texture, transparent: true, depthWrite: false, depthTest: false, fog: false, toneMapped: false,
      color: bandInk, blending: THREE.AdditiveBlending, side: THREE.FrontSide,
    }),
  );
  band.frustumCulled = false;
  band.renderOrder = 8;
  band.material.color.copy(bandInk);
  const pivot = new THREE.Group();
  pivot.add(band);
  group.add(pivot);

  // The shockwaves ride with the sphere, at its foot: a projector disc under
  // the hologram rather than a ring left behind on the roof it rose from.
  const base = new THREE.Group();
  base.position.y = -(RISE - 0.03);
  group.add(base);
  const waves = [];
  for (let i = 0; i < WAVES; i++) {
    const wave = new THREE.Mesh(
      new THREE.RingGeometry(0.86, 1, 128),
      new THREE.MeshBasicMaterial({
        color: bandInk, transparent: true, depthWrite: false, fog: false, toneMapped: false,
        opacity: 0, blending: THREE.AdditiveBlending, side: THREE.DoubleSide,
      }),
    );
    wave.rotation.x = -Math.PI / 2;
    wave.renderOrder = 7;
    wave.visible = false;
    base.add(wave);
    waves.push(wave);
  }

  let id = null;
  let last = null;
  let shown = false;
  let show = 0; // how far the appear or the leave has got, 0 to 1
  let age = 0; // seconds since this selection was picked
  let clock = 0;
  let radius = 1;

  // The skin's own two colours: the selection's accent for the sphere and the
  // shockwave, and the sign ink for the name itself, which has to stay legible
  // on any background a skin puts behind it.
  function setInk(shellColor, nameColor) {
    ink.set(shellColor);
    bandInk.set(nameColor);
    shell.material.uniforms.uColor.value.copy(ink);
    band.material.color.copy(bandInk);
    for (const wave of waves) wave.material.color.copy(ink);
  }

  return {
    group,
    setInk,
    // Which module the hologram is for. The same one again — applyMode runs
    // for every sidebar change — leaves the animation where it is; a new one
    // rises and breaks the roof again.
    show(next, name) {
      const fresh = next !== id;
      id = next;
      if (fresh || name !== last) {
        last = name;
        // The sheet is white on white, the way the rooftop plates are: the
        // material's colour is the ink, so a skin changes the name without
        // the sheet being painted again.
        paintBand(ctx, name || "", "#ffffff");
        texture.needsUpdate = true;
      }
      if (fresh) {
        show = 0;
        age = 0;
      }
      shown = true;
      group.visible = true;
    },
    hide() {
      shown = false;
    },
    // True while the hologram has something left to do, so the render loop
    // keeps asking for frames until it has faded out for good.
    live() {
      return show > 0.002 || shown;
    },
    // Put it over the module at the size the screen asks for. `spot` is the
    // roof the module is drawn with and `spot.apex` the highest the module's
    // own call arcs bow over it; the hologram rests clear of both, so it never
    // stands in the way of the curves it is sitting above. `height` is the
    // canvas in CSS pixels, which is what a size fixed on screen is measured in.
    place(camera, spot, height) {
      if (!spot || !group.visible) return;
      const dist = camera.position.distanceTo(spot);
      const perPixel = (2 * Math.tan((camera.fov * Math.PI) / 360) * dist) / Math.max(1, height);
      radius = Math.min(MAX_RADIUS, Math.max(MIN_RADIUS, perPixel * HOLO_PX * 0.5));
      const rest = Math.max(spot.y, spot.apex || 0) + radius * RISE;
      const rise = 1 - Math.pow(1 - show, 3);
      group.position.set(spot.x, spot.y + (rest - spot.y) * rise, spot.z);
      group.scale.setScalar(radius);
      pivot.lookAt(camera.position);
    },
    // A frame's worth of show. The shell is never hidden while the sphere is
    // up: its sweep and its breath are what say the hologram is alive.
    tick(dt) {
      const step = dt > 0 ? dt : 0;
      clock += step;
      if (shown) age += step;
      show = Math.min(1, Math.max(0, show + (shown ? step / APPEAR_S : -step / GONE_S)));
      if (!shown && show <= 0.002) {
        group.visible = false;
        return;
      }
      const fade = 1 - Math.pow(1 - show, 3);
      shell.material.uniforms.uFade.value = fade;
      shell.material.uniforms.uTime.value = clock;
      // The sweep runs from below the sphere to above it and round again.
      shell.material.uniforms.uSweep.value = (((clock / SWEEP_S) * 2.6) % 2.6) - 1.3;
      shell.scale.setScalar(1 + 0.035 * Math.sin((clock / PULSE_S) * Math.PI * 2));
      // The name is the one thing that must not flicker: the band fades in and
      // out with the rest, and nothing else touches its opacity.
      band.material.opacity = fade;
      for (let i = 0; i < waves.length; i++) {
        const wave = waves[i];
        const at = age - i * WAVE_GAP;
        const q = at <= 0 || at >= WAVE_S ? -1 : at / WAVE_S;
        wave.visible = q >= 0;
        if (!wave.visible) continue;
        wave.scale.setScalar(0.5 + WAVE_MAX * q);
        wave.material.opacity = Math.pow(1 - q, 1.7) * 0.7 * fade;
      }
    },
  };
}
