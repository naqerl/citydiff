// Horizontal flight, orbit, and zoom for the city camera.
// Positive yaw orbits the camera left around the look point: counter-clockwise from above.
// The look point stays put, camera height stays put, and distance stays put.
// A camera at (0, 5, 10) looking at the origin, yawed +90°, lands at (10, 5, 0).
// Positive zoom moves the camera toward the look point along the full view ray.
// One second at zoom 1 leaves about 37% of the distance (exp(-1)).
// Forward is the look direction from the start of the step, flattened onto the ground.
// Right is forward × up. A camera looking down −z strafes toward +x.

export function flyStep(position, target, direction, axes, dt, limits) {
  const pos = { x: position.x, y: position.y, z: position.z };
  const look = { x: target.x, y: target.y, z: target.z };
  const yaw = (axes.yaw || 0) * dt;
  if (yaw) {
    const ox = pos.x - look.x;
    const oz = pos.z - look.z;
    const c = Math.cos(yaw);
    const s = Math.sin(yaw);
    pos.x = look.x + ox * c + oz * s;
    pos.z = look.z - ox * s + oz * c;
  }
  let ox = pos.x - look.x;
  let oy = pos.y - look.y;
  let oz = pos.z - look.z;
  let dist = Math.hypot(ox, oy, oz);
  const zoom = axes.zoom || 0;
  if (zoom && dist > 1e-8) {
    let next = dist * Math.exp(-zoom * dt);
    if (limits && limits.minDistance != null) next = Math.max(limits.minDistance, next);
    if (limits && limits.maxDistance != null) next = Math.min(limits.maxDistance, next);
    const scale = next / dist;
    pos.x = look.x + ox * scale;
    pos.y = look.y + oy * scale;
    pos.z = look.z + oz * scale;
    dist = next;
  }
  let fx = direction.x;
  let fz = direction.z;
  const flat = Math.hypot(fx, fz);
  if (flat < 1e-8) {
    fx = 0;
    fz = -1;
  } else {
    fx /= flat;
    fz /= flat;
  }
  const rx = -fz;
  const rz = fx;
  const step = Math.max(dist, 4) * 0.6 * dt;
  const dx = (fx * (axes.forward || 0) + rx * (axes.strafe || 0)) * step;
  const dz = (fz * (axes.forward || 0) + rz * (axes.strafe || 0)) * step;
  pos.x += dx;
  pos.z += dz;
  look.x += dx;
  look.z += dz;
  return { position: pos, target: look };
}
