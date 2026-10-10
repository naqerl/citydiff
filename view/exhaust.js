// Exhaust for the /launch rocket: flame and smoke as soft round points, and
// the glow at the engines. rocket.js decides where and when they are emitted.
import * as THREE from "three";

// Exhaust colour by age: white-hot at the nozzle, orange, then a dull red.
const HOT = [1.0, 0.95, 0.82];
const ORANGE = [1.0, 0.56, 0.16];
const EMBER = [0.5, 0.1, 0.03];
export function flameRamp(k, out) {
  if (k < 0.22) mix3(HOT, ORANGE, k / 0.22, out);
  else mix3(ORANGE, EMBER, (k - 0.22) / 0.78, out);
  return Math.pow(1 - k, 1.6);
}

// Smoke and steam start lit by the flame and cool to grey; they fade in, then out.
const LIT = [0.95, 0.76, 0.58];
const GREY = [0.62, 0.59, 0.65];
export function smokeRamp(k, out) {
  mix3(LIT, GREY, Math.min(1, k * 2.5), out);
  return Math.min(1, k * 12) * Math.pow(1 - k, 1.3);
}

function mix3(a, b, t, out) {
  out[0] = a[0] + (b[0] - a[0]) * t;
  out[1] = a[1] + (b[1] - a[1]) * t;
  out[2] = a[2] + (b[2] - a[2]) * t;
}

export function glowTexture() {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const ctx = canvas.getContext("2d");
  const g = ctx.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  g.addColorStop(0, "rgba(255, 250, 235, 1)");
  g.addColorStop(0.18, "rgba(255, 210, 140, 0.85)");
  g.addColorStop(0.45, "rgba(255, 130, 40, 0.3)");
  g.addColorStop(1, "rgba(255, 90, 20, 0)");
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, size, size);
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  return tex;
}

const drawSize = new THREE.Vector2();

// makePool is a ring of particles. Each one carries its own velocity, drag
// and lift; one that would sink under the floor is turned out along it
// instead, the way exhaust spreads off the pad.
export function makePool(cap, blending, order) {
  const geo = new THREE.BufferGeometry();
  const pos = new Float32Array(cap * 3);
  const color = new Float32Array(cap * 3);
  const alpha = new Float32Array(cap);
  const size = new Float32Array(cap);
  const attr = (array, n) => new THREE.BufferAttribute(array, n).setUsage(THREE.DynamicDrawUsage);
  geo.setAttribute("position", attr(pos, 3));
  geo.setAttribute("aColor", attr(color, 3));
  geo.setAttribute("aAlpha", attr(alpha, 1));
  geo.setAttribute("aSize", attr(size, 1));
  const material = new THREE.ShaderMaterial({
    uniforms: { uScale: { value: 1 } },
    vertexShader: [
      "attribute vec3 aColor;",
      "attribute float aAlpha;",
      "attribute float aSize;",
      "uniform float uScale;",
      "varying vec3 vColor;",
      "varying float vAlpha;",
      "void main() {",
      "  vec4 mv = modelViewMatrix * vec4(position, 1.0);",
      "  gl_Position = projectionMatrix * mv;",
      "  gl_PointSize = aAlpha > 0.0 ? clamp(aSize * uScale / max(-mv.z, 1e-3), 0.0, 480.0) : 0.0;",
      "  vColor = aColor;",
      "  vAlpha = aAlpha;",
      "}",
    ].join("\n"),
    fragmentShader: [
      "varying vec3 vColor;",
      "varying float vAlpha;",
      "void main() {",
      "  vec2 p = gl_PointCoord * 2.0 - 1.0;",
      "  float r = dot(p, p);",
      "  if (r > 1.0) discard;",
      "  float a = 1.0 - r;",
      "  gl_FragColor = vec4(vColor, vAlpha * a * a);",
      "}",
    ].join("\n"),
    blending,
    transparent: true,
    depthWrite: false,
  });
  const points = new THREE.Points(geo, material);
  points.frustumCulled = false;
  points.renderOrder = order;
  // The point size is in world units: the projection scales it to pixels.
  points.onBeforeRender = (renderer, scene, camera) => {
    material.uniforms.uScale.value = camera.projectionMatrix.elements[5] * 0.5 * renderer.getDrawingBufferSize(drawSize).y;
  };
  const vel = new Float32Array(cap * 3);
  const age = new Float32Array(cap);
  const life = new Float32Array(cap);
  const s0 = new Float32Array(cap);
  const s1 = new Float32Array(cap);
  const a0 = new Float32Array(cap);
  const drag = new Float32Array(cap);
  const lift = new Float32Array(cap);
  const rgb = [0, 0, 0];
  let next = 0;

  function clear() {
    age.fill(0);
    life.fill(0);
    alpha.fill(0);
    geo.attributes.aAlpha.needsUpdate = true;
  }

  function spawn(p, v, born, lifetime, size0, size1, opacity, dragK, liftK) {
    const i = next;
    next = (next + 1) % cap;
    pos[i * 3] = p.x + v.x * born;
    pos[i * 3 + 1] = p.y + v.y * born;
    pos[i * 3 + 2] = p.z + v.z * born;
    vel[i * 3] = v.x;
    vel[i * 3 + 1] = v.y;
    vel[i * 3 + 2] = v.z;
    age[i] = born;
    life[i] = lifetime;
    s0[i] = size0;
    s1[i] = size1;
    a0[i] = opacity;
    drag[i] = dragK;
    lift[i] = liftK;
  }

  // step moves every live particle on by dt. hurry ages them faster than
  // they move, so a cloud can be thinned out on cue.
  function step(dt, ramp, floor, hurry = 1) {
    let live = false;
    for (let i = 0; i < cap; i++) {
      if (age[i] >= life[i]) {
        alpha[i] = 0;
        continue;
      }
      live = true;
      age[i] += dt * hurry;
      const k = Math.min(1, age[i] / life[i]);
      const j = i * 3;
      const damp = Math.exp(-drag[i] * dt);
      vel[j] *= damp;
      vel[j + 1] = vel[j + 1] * damp + lift[i] * dt;
      vel[j + 2] *= damp;
      pos[j] += vel[j] * dt;
      pos[j + 1] += vel[j + 1] * dt;
      pos[j + 2] += vel[j + 2] * dt;
      if (floor && pos[j + 1] < floor.y && vel[j + 1] < 0) {
        let dx = pos[j] - floor.x;
        let dz = pos[j + 2] - floor.z;
        let len = Math.hypot(dx, dz);
        if (len < 1e-6) {
          const a = Math.random() * Math.PI * 2;
          dx = Math.cos(a);
          dz = Math.sin(a);
          len = 1;
        }
        const out = -vel[j + 1] * 0.8;
        vel[j] += (dx / len) * out;
        vel[j + 2] += (dz / len) * out;
        vel[j + 1] = 0;
        pos[j + 1] = floor.y;
      }
      const fade = ramp(k, rgb);
      color[j] = rgb[0];
      color[j + 1] = rgb[1];
      color[j + 2] = rgb[2];
      alpha[i] = k >= 1 ? 0 : a0[i] * fade;
      size[i] = s0[i] + (s1[i] - s0[i]) * Math.sqrt(k);
    }
    for (const name of ["position", "aColor", "aAlpha", "aSize"]) geo.attributes[name].needsUpdate = true;
    return live;
  }

  return { points, spawn, step, clear };
}
