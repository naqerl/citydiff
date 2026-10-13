// ?ar: on an Android phone the city stands on a real table. The phone's
// camera is the background, the city follows the surface the phone points at
// until a tap puts it down, and from then on it stays where it was put while
// the person walks round it. Pure math; main.js runs the WebXR session.
//
// The city keeps its own units. Instead of shrinking the city, the camera
// rides on a rig that scales the room up: one metre of the room is `scale`
// units of the city. The rig also turns the city so the side the person was
// looking at before AR faces them, and moves it so the spot they tapped is
// the middle of its circle. The session's reference space is in metres, x
// right, y up, z toward the person at the start, like three's world.

// How wide the city's circle stands on the table, in metres.
export const AR_SIZE_M = 0.6;

// arScale is how many city units one metre of the room is, for a circle of
// that radius.
export function arScale(radius, size = AR_SIZE_M) {
  return Math.max(1e-6, (2 * radius) / size);
}

// turn rotates (x, z) about the vertical by yaw, the way three does: positive
// is counter-clockwise seen from above.
function turn(x, z, yaw) {
  const c = Math.cos(yaw);
  const s = Math.sin(yaw);
  return { x: x * c + z * s, z: -x * s + z * c };
}

// facing is the yaw that turns the direction from the spot to the person (in
// the room) onto front (in the city): the side of the city the camera saw
// before AR is the side that faces the person.
export function facing(spot, viewer, front) {
  const dx = viewer.x - spot.x;
  const dz = viewer.z - spot.z;
  const toViewer = Math.hypot(dx, dz) > 1e-6 ? Math.atan2(dx, dz) : 0;
  const toFront = Math.hypot(front.x, front.z) > 1e-6 ? Math.atan2(front.x, front.z) : 0;
  return toFront - toViewer;
}

// arRig is the camera rig's transform: room metres to city units. It is
// T(center) · Ry(yaw) · S(scale) · T(-spot), so the spot lands on the centre,
// and it is given as a position, a yaw and a scale for an Object3D.
export function arRig(spot, center, scale, yaw) {
  const r = turn(spot.x * scale, spot.z * scale, yaw);
  return {
    position: { x: center.x - r.x, y: center.y - spot.y * scale, z: center.z - r.z },
    yaw,
    scale,
  };
}

// roomToCity applies a rig to a point in the room.
export function roomToCity(rig, p) {
  const r = turn(p.x * rig.scale, p.z * rig.scale, rig.yaw);
  return { x: rig.position.x + r.x, y: rig.position.y + p.y * rig.scale, z: rig.position.z + r.z };
}

// level is whether a hit-test pose lies on a surface a city can stand on: its
// normal, the pose's y axis, within 30° of straight up. A wall or the side of
// a cupboard is not.
export function level(orientation, within = Math.PI / 6) {
  const { x, z } = orientation;
  const up = 1 - 2 * (x * x + z * z);
  return up >= Math.cos(within);
}
