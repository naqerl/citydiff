// ?gyro: on a phone or a tablet the city turns with the device. It is off
// unless the address asks for it (state.js reads ?gyro) or a plugin turns it
// on, and a screen with a mouse never has it.
// Turning the phone left orbits the camera left around what it looks at, the
// way a window onto the city would turn; tipping the top away raises the
// camera to look down on it. A shake is the 0 key: the camera goes back to
// the whole city. Pure math; main.js listens to the device and moves the
// camera.
//
// The device's attitude comes from deviceorientation as the W3C spells it:
// alpha about the device's z (out of the screen), then beta about its x (to
// the right of the screen), then gamma about its y (to the top of the
// screen), in degrees. Euler angles jump near an upright phone; the
// quaternion they make does not, so the turn is read from quaternions.

// The camera turns twice as far as the phone: a twist of the wrist goes a
// long way round the city.
export const GYRO_GAIN = 2;

const RAD = Math.PI / 180;

// attitude is the device's rotation from its own frame to the earth's (x
// east, y north, z up), as a quaternion [x, y, z, w]: Rz(alpha) Rx(beta)
// Ry(gamma). A missing alpha (a phone with no gyroscope reports only its
// tilt) reads as 0, so the tilt still turns the city.
export function attitude(alpha, beta, gamma) {
  const a = ((alpha || 0) * RAD) / 2;
  const b = ((beta || 0) * RAD) / 2;
  const g = ((gamma || 0) * RAD) / 2;
  const ca = Math.cos(a), sa = Math.sin(a);
  const cb = Math.cos(b), sb = Math.sin(b);
  const cg = Math.cos(g), sg = Math.sin(g);
  return [
    ca * sb * cg - sa * cb * sg,
    ca * cb * sg + sa * sb * cg,
    ca * sb * sg + sa * cb * cg,
    ca * cb * cg - sa * sb * sg,
  ];
}

function multiply(p, q) {
  return [
    p[3] * q[0] + p[0] * q[3] + p[1] * q[2] - p[2] * q[1],
    p[3] * q[1] - p[0] * q[2] + p[1] * q[3] + p[2] * q[0],
    p[3] * q[2] + p[0] * q[1] - p[1] * q[0] + p[2] * q[3],
    p[3] * q[3] - p[0] * q[0] - p[1] * q[1] - p[2] * q[2],
  ];
}

function inverse(q) {
  return [-q[0], -q[1], -q[2], q[3]];
}

// twist is the angle a rotation turns about a unit axis, in (-π, π]: the
// part of it that is a turn about that axis, whatever else it does.
function twist(q, axis) {
  const along = q[0] * axis[0] + q[1] * axis[1] + q[2] * axis[2];
  let angle = 2 * Math.atan2(along, q[3]);
  if (angle > Math.PI) angle -= 2 * Math.PI;
  if (angle <= -Math.PI) angle += 2 * Math.PI;
  return angle;
}

// turnBetween is how far the device turned from one attitude to the next, in
// radians. yaw is the turn about the vertical, positive to the left
// (counter-clockwise seen from above), however the phone is held: upright,
// tipped, or flat on a palm. pitch is the turn about the screen's own left to
// right line, positive when the top comes toward the person holding it. The
// screen's angle (screen.orientation.angle) says which edge is its top once
// the page has turned sideways.
export function turnBetween(prev, next, screenAngle = 0) {
  const world = multiply(next, inverse(prev));
  const body = multiply(inverse(prev), next);
  const s = (screenAngle || 0) * RAD;
  return {
    yaw: twist(world, [0, 0, 1]),
    pitch: twist(body, [Math.cos(s), -Math.sin(s), 0]),
  };
}

// orbitStep turns a camera about the point it looks at: yaw about the
// vertical, positive to the left (fly.js turns the same way), and pitch as a
// change of the polar angle, positive toward the horizon, held between the
// limits the controls keep. Three's frame: y up. The distance stays.
export function orbitStep(position, target, yaw, pitch, limits = {}) {
  const ox = position.x - target.x;
  const oy = position.y - target.y;
  const oz = position.z - target.z;
  const r = Math.hypot(ox, oy, oz);
  if (r < 1e-8) return { x: position.x, y: position.y, z: position.z };
  const min = limits.minPolar ?? 0;
  const max = limits.maxPolar ?? Math.PI;
  const theta = Math.atan2(ox, oz) + yaw;
  const now = Math.acos(Math.max(-1, Math.min(1, oy / r)));
  // A camera already outside the limits is not pulled back by a turn; it only
  // comes no further out.
  const phi = Math.max(Math.min(min, now), Math.min(Math.max(max, now), now + pitch));
  const flat = r * Math.sin(phi);
  return {
    x: target.x + flat * Math.sin(theta),
    y: target.y + r * Math.cos(phi),
    z: target.z + flat * Math.cos(theta),
  };
}

// A shake is the device jerked back and forth: SHAKE_JOLTS jolts harder than
// SHAKE_ACCEL (m/s², gravity left out), each the opposite way to the one
// before, inside SHAKE_WINDOW_MS. One bump, setting the phone down, or a
// brisk turn is one or two jolts, not a shake. After a shake the next waits
// SHAKE_REST_MS, so one shake resets the camera once.
export const SHAKE_ACCEL = 12;
export const SHAKE_JOLTS = 4;
export const SHAKE_WINDOW_MS = 1000;
export const SHAKE_REST_MS = 1500;

// createShake is a shake detector. push takes one devicemotion sample, the
// acceleration without gravity and the time in milliseconds, and is true for
// the sample that completes a shake. A phone that reports only the
// acceleration with gravity is handed to pushWithGravity instead, which takes
// the gravity out itself.
export function createShake(opts = {}) {
  const accel = opts.accel ?? SHAKE_ACCEL;
  const need = opts.jolts ?? SHAKE_JOLTS;
  const windowMs = opts.windowMs ?? SHAKE_WINDOW_MS;
  const restMs = opts.restMs ?? SHAKE_REST_MS;
  let jolts = [];
  let last = null;
  let quietUntil = -Infinity;
  let gravity = null;

  function push(x, y, z, t) {
    if (t < quietUntil) return false;
    if (Math.hypot(x, y, z) < accel) return false;
    // A jolt the same way as the last is the same jolt, still building.
    if (last && last[0] * x + last[1] * y + last[2] * z > 0) return false;
    last = [x, y, z];
    jolts = jolts.filter((at) => t - at <= windowMs);
    jolts.push(t);
    if (jolts.length < need) return false;
    jolts = [];
    last = null;
    quietUntil = t + restMs;
    return true;
  }

  // Gravity is what stays: a slow average of the samples, taken away from
  // each one.
  function pushWithGravity(x, y, z, t) {
    if (!gravity) gravity = [x, y, z];
    gravity = [0.9 * gravity[0] + 0.1 * x, 0.9 * gravity[1] + 0.1 * y, 0.9 * gravity[2] + 0.1 * z];
    return push(x - gravity[0], y - gravity[1], z - gravity[2], t);
  }

  return { push, pushWithGravity };
}
