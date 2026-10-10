// Optional skin shaders and textures. A skin stays JSON. A vertex or fragment
// file is GLSL inserted into the material Three.js already builds, so lights,
// fog, and colour updates keep working. Leave the files out and the material
// stays as it was.
import * as THREE from "three";
import { assetURL } from "./skin.js";

const SURFACE = ["fog", "plane", "ground", "horizon", "package", "external", "entity"];

export async function loadShade(theme) {
  theme.uTime = { value: 0 };
  const notes = [];
  await Promise.all(SURFACE.map((key) => loadSpec(theme, theme[key], notes)));
  const sources = SURFACE.flatMap((key) => {
    const spec = theme[key];
    return spec ? [spec.vertexSource, spec.fragmentSource] : [];
  });
  theme.live = sources.some((src) => src && src.includes("uTime"));
  if (notes.length) theme.shadeWarning = notes.join(" ");
}

async function loadSpec(theme, spec, notes) {
  if (!spec || typeof spec !== "object") return;
  if (typeof spec.vertex === "string") spec.vertexSource = await loadText(theme, spec.vertex, notes);
  if (typeof spec.fragment === "string") spec.fragmentSource = await loadText(theme, spec.fragment, notes);
  if (typeof spec.map === "string") spec.mapTexture = await loadImage(theme, spec.map, notes);
  if (spec.maps && typeof spec.maps === "object") {
    spec.mapTextures = {};
    for (const [name, file] of Object.entries(spec.maps)) {
      if (typeof file !== "string" || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) continue;
      spec.mapTextures[name] = await loadImage(theme, file, notes);
    }
  }
}

async function loadText(theme, file, notes) {
  const url = assetURL(theme.url, file);
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error(String(res.status));
    return await res.text();
  } catch (err) {
    notes.push("Could not read " + file + ".");
    return "";
  }
}

async function loadImage(theme, file, notes) {
  const url = assetURL(theme.url, file);
  const texture = new THREE.TextureLoader().load(url, undefined, undefined, () => notes.push("Could not read " + file + "."));
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.wrapS = THREE.RepeatWrapping;
  texture.wrapT = THREE.RepeatWrapping;
  return texture;
}

export function dress(material, spec, theme) {
  const vertex = (spec && spec.vertexSource) || "";
  const fragment = (spec && spec.fragmentSource) || "";
  const fog = (theme.fog && theme.fog.fragmentSource) || "";
  if (spec && spec.mapTexture && !uses(fragment, "uMap")) {
    const repeat = spec.repeat || [1, 1];
    spec.mapTexture.repeat.set(repeat[0] || 1, repeat[1] || 1);
    material.map = spec.mapTexture;
  }
  if (!vertex && !fragment && !fog && !(spec && spec.mapTextures)) return material;
  const prev = material.onBeforeCompile;
  material.onBeforeCompile = (shader) => {
    if (prev) prev(shader);
    const headV = [];
    const headF = [];
    if (uses(vertex, "uTime") || uses(fragment, "uTime")) {
      shader.uniforms.uTime = theme.uTime;
      if (uses(vertex, "uTime")) headV.push("uniform float uTime;");
      if (uses(fragment, "uTime")) headF.push("uniform float uTime;");
    }
    if (uses(vertex, "aHouse")) headV.push("attribute vec2 aHouse;");
    if (uses(vertex, "vHouse") || uses(fragment, "vHouse")) {
      headV.push("varying vec2 vHouse;");
      headF.push("varying vec2 vHouse;");
    }
    if (uses(vertex, "vLocalUp") || uses(fragment, "vLocalUp")) {
      headV.push("varying float vLocalUp;");
      headF.push("varying float vLocalUp;");
    }
    if (uses(fog, "vSkinWorld")) {
      headV.push("varying vec3 vSkinWorld;");
      headF.push("varying vec3 vSkinWorld;");
    }
    // Three builds vUv only for a map. Naming vUv still receives the mesh uv.
    const needsUv = uses(vertex, "vUv") || uses(fragment, "vUv");
    if (needsUv) {
      const uvDecl = "#if !defined( USE_UV ) && !defined( USE_ANISOTROPY )\n\tvarying vec2 vUv;\n#endif";
      headV.push(uvDecl);
      headF.push(uvDecl);
    }
    headF.push(...samplerLines(shader, spec, fragment));
    if (headV.length) shader.vertexShader = headV.join("\n") + "\n" + shader.vertexShader;
    if (headF.length) shader.fragmentShader = headF.join("\n") + "\n" + shader.fragmentShader;
    if (needsUv || vertex) {
      let body = "";
      if (needsUv) body += "#if !defined( USE_UV ) && !defined( USE_ANISOTROPY )\n\tvUv = uv;\n#endif\n";
      if (vertex) body += vertex + "\n";
      shader.vertexShader = shader.vertexShader.replace("#include <begin_vertex>", "#include <begin_vertex>\n" + body);
    }
    if (fragment) shader.fragmentShader = shader.fragmentShader.replace("#include <color_fragment>", "#include <color_fragment>\n" + fragment);
    if (uses(fog, "vSkinWorld")) {
      shader.vertexShader = shader.vertexShader.replace(
        "#include <project_vertex>",
        "#include <project_vertex>\nvec4 skinLocal = vec4(transformed, 1.0);\n#ifdef USE_INSTANCING\nskinLocal = instanceMatrix * skinLocal;\n#endif\nvSkinWorld = (modelMatrix * skinLocal).xyz;",
      );
    }
    if (fog) shader.fragmentShader = shader.fragmentShader.replace("#include <fog_fragment>", fog);
  };
  const maps = spec && spec.mapTextures ? Object.keys(spec.mapTextures).join(",") : "";
  material.customProgramCacheKey = () => [vertex, fragment, fog, maps].join("\n");
  return material;
}

function samplerLines(shader, spec, fragment) {
  const lines = [];
  if (spec && spec.mapTexture && uses(fragment, "uMap")) {
    shader.uniforms.uMap = { value: spec.mapTexture };
    lines.push("uniform sampler2D uMap;");
  }
  const textures = (spec && spec.mapTextures) || {};
  for (const [name, texture] of Object.entries(textures)) {
    if (texture && uses(fragment, name)) {
      shader.uniforms[name] = { value: texture };
      lines.push("uniform sampler2D " + name + ";");
    }
  }
  return lines;
}

function uses(source, name) {
  return !!source && new RegExp("\\b" + name + "\\b").test(source);
}
