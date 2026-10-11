import * as THREE from "three";
import { assetURL, skyboxKind } from "./skin.js";

// A theme fade calls this every frame, so the cube is made once and repainted
// in place: the same six small canvases, uploaded again, never a new texture.
export function applyGradient(scene, sky) {
  if (current && current !== gradient?.tex) {
    current.dispose();
    current = null;
  }
  if (!gradient) gradient = gradientCube();
  paintGradient(gradient, sky);
  scene.background = gradient.tex;
  current = gradient.tex;
}

let current = null;
let gradient = null;

const FACE = [
  (u, v) => [1, v, -u],
  (u, v) => [-1, v, u],
  (u, v) => [u, 1, -v],
  (u, v) => [u, -1, v],
  (u, v) => [u, v, 1],
  (u, v) => [-u, v, -1],
];

export function applySky(scene, skin) {
  const background = skin.background || {};
  const kind = skyboxKind(background.skybox);
  if (kind === "gradient") {
    applyGradient(scene, background.skybox);
    return;
  }
  if (current) {
    current.dispose();
    current = null;
    gradient = null;
  }
  if (kind === "equirect") {
    const tex = new THREE.TextureLoader().load(assetURL(skin.url, background.skybox));
    tex.mapping = THREE.EquirectangularReflectionMapping;
    tex.colorSpace = THREE.SRGBColorSpace;
    scene.background = tex;
    current = tex;
    return;
  }
  if (kind === "cube") {
    const urls = background.skybox.map((ref) => assetURL(skin.url, ref));
    const tex = new THREE.CubeTextureLoader().load(urls);
    tex.colorSpace = THREE.SRGBColorSpace;
    scene.background = tex;
    current = tex;
    return;
  }
  scene.background = new THREE.Color(background.color || "#000000");
}

function gradientCube() {
  const size = 64;
  const faces = FACE.map((dirOf) => {
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext("2d");
    // Where each pixel looks, as the height of its direction: the colours
    // change, the directions never do.
    const rise = new Float32Array(size * size);
    for (let y = 0; y < size; y++) {
      const v = 1 - ((y + 0.5) / size) * 2;
      for (let x = 0; x < size; x++) {
        const u = ((x + 0.5) / size) * 2 - 1;
        const [dx, dy, dz] = dirOf(u, v);
        rise[y * size + x] = dy / (Math.hypot(dx, dy, dz) || 1);
      }
    }
    return { canvas, ctx, img: ctx.createImageData(size, size), rise };
  });
  const tex = new THREE.CubeTexture(faces.map((face) => face.canvas));
  tex.colorSpace = THREE.SRGBColorSpace;
  return { tex, faces };
}

function paintGradient(cube, sky) {
  const top = rgb(sky.top);
  const horizon = rgb(sky.horizon);
  const bottom = rgb(sky.bottom);
  for (const face of cube.faces) {
    const data = face.img.data;
    const rise = face.rise;
    for (let p = 0; p < rise.length; p++) {
      const t = rise[p];
      const to = t >= 0 ? top : bottom;
      const k = t >= 0 ? t : -t;
      const i = p * 4;
      data[i] = Math.round(horizon[0] + (to[0] - horizon[0]) * k);
      data[i + 1] = Math.round(horizon[1] + (to[1] - horizon[1]) * k);
      data[i + 2] = Math.round(horizon[2] + (to[2] - horizon[2]) * k);
      data[i + 3] = 255;
    }
    face.ctx.putImageData(face.img, 0, 0);
  }
  cube.tex.needsUpdate = true;
}

function rgb(hex) {
  const raw = String(hex || "").trim().replace("#", "");
  const s = raw.length === 3 ? raw.replace(/./g, (ch) => ch + ch) : raw;
  if (!/^[0-9a-fA-F]{6}$/.test(s)) return [0, 0, 0];
  return [parseInt(s.slice(0, 2), 16), parseInt(s.slice(2, 4), 16), parseInt(s.slice(4, 6), 16)];
}
