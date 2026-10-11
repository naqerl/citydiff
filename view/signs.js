// Package names, drawn as one mesh. Each glyph is painted once, in white, on
// a shared sheet, and every sign is a row of quads cut from it. The ink is the
// material colour and the plate behind it a uniform, so a theme change or a
// fade writes two colours and uploads nothing, and the whole city's signs are
// one draw call however many packages there are.
import * as THREE from "three";

// A glyph is painted at this size and then stretched over its plate: a big
// glyph is what keeps the text sharp now that a plate grows with its building.
export const SIGN_FONT = 96;
const SIGN_PAD = 28;
const SIGN_HEIGHT = Math.round(SIGN_FONT * 1.5);
const MAX_CHARS = 22;
// Transparent space around each glyph on the sheet, so the smaller mip levels
// blend a glyph with nothing rather than with its neighbour.
const GUTTER = 24;

function signFont() {
  return "600 " + SIGN_FONT + "px ui-monospace, monospace";
}

export function signText(label) {
  const text = String(label || "");
  return text.length > MAX_CHARS ? text.slice(0, MAX_CHARS - 1) + "…" : text;
}

// A sign is as big as its wall allows: as wide as the face it sits on, and no
// taller than the building can carry. A tall package gets a big sign, a flat
// one a small one, and the two pairs of faces get a size each because the
// front of a building is usually wider than its side.
export function plateSize(span, boxHeight, aspect) {
  const width = Math.max(0.6, span * 0.94);
  const maxHeight = Math.max(0.5, Math.min(boxHeight * 0.62, 4));
  const height = Math.min(width / aspect, maxHeight);
  return { width: height * aspect, height };
}

export function hasPlate(box) {
  return !(Math.max(box.w, box.d) < 5 || Math.min(box.w, box.d) < 2.2);
}

// The glyph sheet: every character the signs use, measured and painted once.
function paintSheet(texts, maxSize) {
  const probe = document.createElement("canvas").getContext("2d");
  probe.font = signFont();
  const glyphs = new Map();
  for (const text of texts) {
    for (const ch of text) {
      if (glyphs.has(ch)) continue;
      const m = probe.measureText(ch);
      const adv = m.width;
      // Ink past the advance on either side gets room on the sheet, so it never
      // lands in a neighbouring cell.
      const left = Math.max(0, Math.ceil(m.actualBoundingBoxLeft || 0));
      const right = Math.max(0, Math.ceil((m.actualBoundingBoxRight || 0) - adv));
      glyphs.set(ch, { ch, adv, left, right, ink: ch.trim() !== "", x: 0, y: 0 });
    }
  }
  // Shelf-pack the cells, widening the sheet until they fit.
  const inked = [...glyphs.values()].filter((g) => g.ink);
  let width = 512;
  let height = 0;
  for (;;) {
    let x = GUTTER;
    let y = GUTTER;
    for (const g of inked) {
      const w = Math.ceil(g.left + g.adv + g.right);
      if (x + w + GUTTER > width) {
        x = GUTTER;
        y += SIGN_HEIGHT + GUTTER;
      }
      g.x = x;
      g.y = y;
      x += w + GUTTER;
    }
    height = y + SIGN_HEIGHT + GUTTER;
    if (height <= width || width >= maxSize) break;
    width *= 2;
  }
  height = Math.min(maxSize, Math.ceil(height / 4) * 4);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  ctx.font = signFont();
  ctx.fillStyle = "#ffffff";
  ctx.textBaseline = "middle";
  for (const g of inked) {
    if (g.y + SIGN_HEIGHT > height) continue; // past the largest sheet: drawn blank
    ctx.fillText(g.ch, g.x + g.left, g.y + SIGN_HEIGHT / 2);
  }
  return { canvas, glyphs, probe };
}

// buildSigns lays every plate out in world space. items is a list of
// { box, text }, one per package that carries a sign; the result's set(i, a)
// fades or hides item i.
export function buildSigns(items, renderer, ink) {
  const maxSize = Math.min(4096, renderer.capabilities.maxTextureSize || 4096);
  const sheet = paintSheet(items.map((item) => item.text), maxSize);
  const W = sheet.canvas.width;
  const H = sheet.canvas.height;
  // Count the quads first. On each face the sign is cut into cells that tile
  // it edge to edge: a margin, one cell per character, a margin. An inked
  // cell is its glyph over its piece of plate, the way a canvas composites
  // fillText over fillRect, so a dimmed sign fades as one picture instead of
  // showing plate through the letters. A monospace glyph keeps its ink inside
  // its advance, which is the cell.
  let glyphQuads = 0;
  let plateQuads = 0;
  for (const item of items) {
    let inked = 0;
    let blank = 0;
    for (const ch of item.text) {
      if (sheet.glyphs.get(ch).ink) inked++;
      else blank++;
    }
    glyphQuads += inked * 4;
    plateQuads += (2 + blank) * 4;
  }
  const quads = plateQuads + glyphQuads;
  const position = new Float32Array(quads * 12);
  const uv = new Float32Array(quads * 8);
  // x: 1 on a plate piece, 0 on a glyph; y: the sign's opacity.
  const sign = new Float32Array(quads * 8);
  const index = quads * 4 > 65535 ? new Uint32Array(quads * 6) : new Uint16Array(quads * 6);
  // Plate pieces fill the front of the buffers and glyphs the back, so a
  // theme without a plate skips them.
  let plateAt = 0;
  let glyphAt = plateQuads;
  const ranges = [];
  const quad = (q, at, x0, x1, uvs, isPlate) => {
    const corners = [at(x0, SIGN_HEIGHT), at(x1, SIGN_HEIGHT), at(x0, 0), at(x1, 0)];
    for (let k = 0; k < 4; k++) {
      position.set(corners[k], q * 12 + k * 3);
      uv[q * 8 + k * 2] = uvs[k][0];
      uv[q * 8 + k * 2 + 1] = uvs[k][1];
      sign[q * 8 + k * 2] = isPlate ? 1 : 0;
      sign[q * 8 + k * 2 + 1] = 1;
    }
    const v = q * 4;
    index.set([v, v + 1, v + 2, v + 2, v + 1, v + 3], q * 6);
  };
  const blank = [[0, 0], [0, 0], [0, 0], [0, 0]];
  const plate = (at, x0, x1) => {
    if (x1 > x0) quad(plateAt, at, x0, x1, blank, true);
    plateAt++;
  };
  for (const item of items) {
    const { box, text } = item;
    const textW = Math.ceil(sheet.probe.measureText(text).width);
    // A canvas width is an integer: the old per-sign canvas truncated it too.
    const canvasW = Math.floor(Math.max(SIGN_FONT * 1.34, textW + SIGN_PAD));
    const aspect = canvasW / SIGN_HEIGHT;
    const cx = box.x + box.w / 2;
    const cy = box.y + box.h / 2;
    const cz = box.z + box.d / 2;
    const gap = 0.045;
    // Outward offset, then the right-hand direction of a reader facing the wall.
    const faces = [
      [0, box.d / 2 + gap, 1, 0, box.w],
      [0, -box.d / 2 - gap, -1, 0, box.w],
      [box.w / 2 + gap, 0, 0, -1, box.d],
      [-box.w / 2 - gap, 0, 0, 1, box.d],
    ];
    const range = { plate: [plateAt, 0], glyph: [glyphAt, 0] };
    for (const [ox, oz, rx, rz, span] of faces) {
      const size = plateSize(span, box.h, aspect);
      const fx = cx + ox;
      const fz = cz + oz;
      const fy = cy + box.h / 2 - size.height / 2 - 0.06;
      // Sign pixel (px, py), y down, to a world corner on this face.
      const at = (px, py) => {
        const lx = (px / canvasW - 0.5) * size.width;
        const ly = (0.5 - py / SIGN_HEIGHT) * size.height;
        return [fx + rx * lx, fy + ly, fz + rz * lx];
      };
      let pen = SIGN_PAD / 2;
      plate(at, 0, pen);
      for (const ch of text) {
        const g = sheet.glyphs.get(ch);
        if (g.ink) {
          const u0 = (g.x + g.left) / W;
          const u1 = (g.x + g.left + g.adv) / W;
          const v0 = 1 - (g.y + SIGN_HEIGHT) / H;
          const v1 = 1 - g.y / H;
          quad(glyphAt++, at, pen, pen + g.adv, [[u0, v0], [u1, v0], [u0, v1], [u1, v1]], false);
        } else {
          plate(at, pen, pen + g.adv);
        }
        pen += g.adv;
      }
      plate(at, pen, canvasW);
    }
    range.plate[1] = plateAt - range.plate[0];
    range.glyph[1] = glyphAt - range.glyph[0];
    ranges.push(range);
  }
  const geo = new THREE.BufferGeometry();
  geo.setAttribute("position", new THREE.BufferAttribute(position, 3));
  geo.setAttribute("uv", new THREE.BufferAttribute(uv, 2));
  const signAttr = new THREE.BufferAttribute(sign, 2);
  geo.setAttribute("aSign", signAttr);
  geo.setIndex(new THREE.BufferAttribute(index, 1));
  geo.computeBoundingSphere();

  const tex = new THREE.CanvasTexture(sheet.canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.anisotropy = renderer.capabilities.getMaxAnisotropy();
  const plateColor = { value: new THREE.Vector4(0, 0, 0, 0) };
  const material = new THREE.MeshBasicMaterial({ map: tex, color: ink, transparent: true, depthWrite: false, fog: false });
  // A glyph cell is the ink over the plate. Without a plate it is the ink
  // alone, exactly what the old one-canvas-per-sign texture drew.
  material.onBeforeCompile = (shader) => {
    shader.uniforms.uSignPlate = plateColor;
    shader.vertexShader = "attribute vec2 aSign;\nvarying vec2 vSign;\n" +
      shader.vertexShader.replace("#include <begin_vertex>", "#include <begin_vertex>\n\tvSign = aSign;");
    shader.fragmentShader = "uniform vec4 uSignPlate;\nvarying vec2 vSign;\n" +
      shader.fragmentShader.replace("#include <map_fragment>", [
        "vec4 signInk = texture2D( map, vMapUv );",
        "float signAlpha = signInk.a + uSignPlate.a * ( 1.0 - signInk.a );",
        "vec3 signRgb = diffuseColor.rgb * signInk.rgb * signInk.a + uSignPlate.rgb * uSignPlate.a * ( 1.0 - signInk.a );",
        "diffuseColor = vec4( signRgb / max( signAlpha, 1e-5 ), opacity * signAlpha );",
        "diffuseColor = mix( diffuseColor, vec4( uSignPlate.rgb, opacity * uSignPlate.a ), vSign.x );",
        "diffuseColor.a *= vSign.y;",
      ].join("\n\t"));
  };
  const mesh = new THREE.Mesh(geo, material);
  mesh.userData.kind = "signs";
  // Plates never take a pointer: picking goes to the package under them.
  mesh.raycast = () => {};

  const plateIndices = plateQuads * 6;
  return {
    mesh,
    material,
    // The plate colour behind the ink, as a CSS colour's linear rgb and alpha.
    // A plate nobody can see is not drawn at all.
    setPlate(color, alpha) {
      plateColor.value.set(color.r, color.g, color.b, alpha);
      geo.setDrawRange(alpha > 0.001 ? 0 : plateIndices, Infinity);
    },
    // Hide (0), dim, or show (1) the sign of item i.
    set(i, alpha) {
      const range = ranges[i];
      if (!range) return;
      let changed = false;
      for (const [start, count] of [range.plate, range.glyph]) {
        for (let q = start; q < start + count; q++) {
          for (let k = 0; k < 4; k++) {
            const o = q * 8 + k * 2 + 1;
            if (sign[o] !== alpha) {
              sign[o] = alpha;
              changed = true;
            }
          }
        }
      }
      if (changed) signAttr.needsUpdate = true;
    },
  };
}
