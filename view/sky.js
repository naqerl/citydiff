import * as THREE from "three";
import { assetURL, skyboxKind } from "./skin.js";

export function applyGradient(scene, sky) {
  if (current) {
    current.dispose();
    current = null;
  }
  scene.background = gradientCube(sky);
  current = scene.background;
}

let current = null;

const FACE = [
  (u, v) => [1, v, -u],
  (u, v) => [-1, v, u],
  (u, v) => [u, 1, -v],
  (u, v) => [u, -1, v],
  (u, v) => [u, v, 1],
  (u, v) => [-u, v, -1],
];

export function applySky(scene, skin) {
  if (current) {
    current.dispose();
    current = null;
  }
  const background = skin.background || {};
  const kind = skyboxKind(background.skybox);
  if (kind === "gradient") {
    applyGradient(scene, background.skybox);
    return;
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

function gradientCube(sky) {
  const size = 64;
  const top = rgb(sky.top);
  const horizon = rgb(sky.horizon);
  const bottom = rgb(sky.bottom);
  const images = FACE.map((dirOf) => paintFace(size, dirOf, top, horizon, bottom));
  const tex = new THREE.CubeTexture(images);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.needsUpdate = true;
  return tex;
}

function paintFace(size, dirOf, top, horizon, bottom) {
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");
  const img = ctx.createImageData(size, size);
  const data = img.data;
  for (let y = 0; y < size; y++) {
    const v = 1 - ((y + 0.5) / size) * 2;
    for (let x = 0; x < size; x++) {
      const u = ((x + 0.5) / size) * 2 - 1;
      const [dx, dy, dz] = dirOf(u, v);
      const len = Math.hypot(dx, dy, dz) || 1;
      const t = dy / len;
      const rgb = t >= 0 ? mix(horizon, top, t) : mix(horizon, bottom, -t);
      const i = (y * size + x) * 4;
      data[i] = rgb[0];
      data[i + 1] = rgb[1];
      data[i + 2] = rgb[2];
      data[i + 3] = 255;
    }
  }
  ctx.putImageData(img, 0, 0);
  return canvas;
}

function rgb(hex) {
  const raw = String(hex || "").trim().replace("#", "");
  const s = raw.length === 3 ? raw.replace(/./g, (ch) => ch + ch) : raw;
  if (!/^[0-9a-fA-F]{6}$/.test(s)) return [0, 0, 0];
  return [parseInt(s.slice(0, 2), 16), parseInt(s.slice(2, 4), 16), parseInt(s.slice(4, 6), 16)];
}

function mix(a, b, t) {
  return [
    Math.round(a[0] + (b[0] - a[0]) * t),
    Math.round(a[1] + (b[1] - a[1]) * t),
    Math.round(a[2] + (b[2] - a[2]) * t),
  ];
}
