// The part of the canvas the city is drawn in, between the two sidebars,
// and the camera pose that fits a box into it. Pure math, no three.js.

import { fitDistance } from "./layout.js";

// insets clamps the sidebar widths so at least minFree pixels stay free.
// A sidebar that would leave less than that is treated as not covering.
export function insets(width, left, right, minFree = 160) {
  let l = Math.max(0, left || 0);
  let r = Math.max(0, right || 0);
  if (width - l - r < minFree) {
    if (width - l >= minFree) r = 0;
    else if (width - r >= minFree) l = 0;
    else l = r = 0;
  }
  return { left: l, right: r, free: width - l - r };
}

// viewOffsetX is the x offset for camera.setViewOffset that puts the look-at
// point in the middle of the free area: (left + right_edge) / 2 on screen.
export function viewOffsetX(left, right) {
  return (right - left) / 2;
}

// On a phone the sidebars are one sheet along the bottom. viewOffsetY lifts
// the look-at point into the middle of the area above it, and freeFov is the
// vertical field of view of that area, so a fit lands in it.
export function viewOffsetY(bottom) {
  return Math.max(0, bottom || 0) / 2;
}

export function freeFov(fov, height, bottom) {
  const b = Math.max(0, bottom || 0);
  if (!b || height <= b) return fov;
  const half = (fov * Math.PI) / 360;
  return (Math.atan(Math.tan(half) * (height - b) / height) * 360) / Math.PI;
}

// fitPose places the camera along dir so every corner of box lands inside
// the free area, with fill of it used (0.86 leaves a margin on each side).
// box is {min: [x, y, z], max: [x, y, z]}. The vertical FOV is the camera's;
// the horizontal one is narrowed to the free width. minDist keeps a tiny
// object from filling the screen. minAspect fits a tall, narrow view as if it
// were that wide, so on a phone held upright the city is drawn larger and
// may run past the sides instead of shrinking to the screen's width.
export function fitPose(box, dir, { fov, width, height, left = 0, right = 0, fill = 0.86, minDist = 14, minAspect = 0 }) {
  const free = insets(width, left, right);
  const aspect = Math.max(minAspect, (width / Math.max(1, height)) * (free.free / Math.max(1, width)));
  const [x0, y0, z0] = box.min;
  const [x1, y1, z1] = box.max;
  const look = { x: (x0 + x1) / 2, y: (y0 + y1) / 2, z: (z0 + z1) / 2 };
  const corners = [];
  for (const x of [x0, x1]) for (const y of [y0, y1]) for (const z of [z0, z1]) corners.push({ x, y, z });
  const len = Math.hypot(dir[0], dir[1], dir[2]) || 1;
  const d = { x: dir[0] / len, y: dir[1] / len, z: dir[2] / len };
  const dist = Math.max(minDist, fitDistance(corners, look, d, fov, aspect, fill));
  return {
    target: [look.x, look.y, look.z],
    pos: [look.x + d.x * dist, look.y + d.y * dist, look.z + d.z * dist],
    dist,
    aspect,
  };
}

// boxOf is the bounding box of points given as [x, y, z].
export function boxOf(points) {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  for (const p of points) {
    for (let i = 0; i < 3; i++) {
      if (p[i] < min[i]) min[i] = p[i];
      if (p[i] > max[i]) max[i] = p[i];
    }
  }
  return { min, max };
}
