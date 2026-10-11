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

// Smallest turn first. Positive is tried before the matching negative, and
// 180 is last because a nearer side view is the one we want.
const YAW_DEGREES = [20, -20, 40, -40, 60, -60, 80, -80, 100, -100, 120, -120, 140, -140, 160, -160, 180];

function boxCentre(box) {
  return [
    (box.min[0] + box.max[0]) / 2,
    (box.min[1] + box.max[1]) / 2,
    (box.min[2] + box.max[2]) / 2,
  ];
}

// Yaw around Y. The height and the length stay put, so the elevation is unchanged.
function yawDir(dir, deg) {
  if (!deg) return dir;
  const a = (deg * Math.PI) / 180;
  const c = Math.cos(a);
  const s = Math.sin(a);
  return [dir[0] * c + dir[2] * s, dir[1], -dir[0] * s + dir[2] * c];
}

// The camera swung around the subject by the same yaw as the view direction.
function orbitEye(eye, subject, deg) {
  if (!deg) return eye;
  const c = boxCentre(subject);
  const spun = yawDir([eye[0] - c[0], eye[1] - c[1], eye[2] - c[2]], deg);
  return [c[0] + spun[0], c[1] + spun[1], c[2] + spun[2]];
}

// t along origin → end (1 is end) where the ray first meets the box.
// 0 when the origin is already inside. Null when the ray misses.
function entryT(origin, end, box) {
  let tmin = 0;
  let tmax = Infinity;
  for (let i = 0; i < 3; i++) {
    const d = end[i] - origin[i];
    if (Math.abs(d) < 1e-12) {
      if (origin[i] < box.min[i] || origin[i] > box.max[i]) return null;
      continue;
    }
    let a = (box.min[i] - origin[i]) / d;
    let b = (box.max[i] - origin[i]) / d;
    if (a > b) {
      const swap = a;
      a = b;
      b = swap;
    }
    if (a > tmin) tmin = a;
    if (b < tmax) tmax = b;
    if (tmin > tmax) return null;
  }
  return tmin;
}

// Hidden when an obstacle is entered strictly before the subject box.
// The subject's own box fails that test, so it is not an occluder.
function occluded(eye, subject, obstacles) {
  const centre = boxCentre(subject);
  const gate = entryT(eye, centre, subject);
  for (const box of obstacles) {
    const t = entryT(eye, centre, box);
    if (t == null) continue;
    if (gate == null ? t < 1 : t < gate) return true;
  }
  return false;
}

// Twice the rise, same heading, normalised. Steeper than the fit, not overhead.
export function steepDir(dir) {
  const y = dir[1] * 2;
  const len = Math.hypot(dir[0], y, dir[2]) || 1;
  return [dir[0] / len, y / len, dir[2] / len];
}

// clearDir keeps dir when the subject is already visible and otherwise yaws
// it around Y. dir points from the look point toward the camera. eye is that
// camera, or a function (dir) => position when the caller places each yaw
// itself (a refit). obstacles are {min, max} boxes, or a function (eye) =>
// true when that eye cannot see the subject. Null when every yaw is blocked.
export function clearDir(dir, eye, subject, obstacles) {
  const place = typeof eye === "function" ? (d) => eye(d) : (d, deg) => orbitEye(eye, subject, deg);
  const blocked = typeof obstacles === "function" ? obstacles : (at) => occluded(at, subject, obstacles || []);
  if (!blocked(place(dir, 0))) return dir;
  for (const deg of YAW_DEGREES) {
    const turned = yawDir(dir, deg);
    if (!blocked(place(turned, deg))) return turned;
  }
  return null;
}
