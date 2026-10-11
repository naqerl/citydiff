// The /launch show: the launch site behind the city, Starship on Super Heavy,
// the countdown, the climb, staging and the way back. The models ship with the
// starship skin; launch.js has the timing and the flight, exhaust.js the flame
// and smoke.
import * as THREE from "three";
import {
  APPROACH_S, ARMS_OPEN, LIFTOFF, STAGE_S, FOLLOW_S, SEP_S, RELEASE_S, GIVE_UP_S, RETURN_S,
  countdown, boosterThrottle, shipThrottle, ascent, shipClock, follow, rumble, outOfSight,
} from "./launch.js";
import { makePool, flameRamp, smokeRamp, glowTexture } from "./exhaust.js";

const MODELS = "/skins/starship/";
// Model units, shared by all three: the booster stands 1.456 tall on its
// engines, the ship 1 tall, and the stack stands on the launch table at 0.4.
const BOOSTER_H = 1.456;
const STACK_H = BOOSTER_H + 1;
const BOOSTER_R = 0.108;
const SHIP_R = 0.09;
const MOUNT_Y = 0.4;
const PAD_HALF = 1.3;
const TOWER_X = 0.56;
const TOWER_TOP = 3.22;
// The chopsticks' two arms hug the booster until they swing open about posts
// at their roots. An arm is a triangle on one side of z = 0, short of the
// carriage's far face.
const ARM_REACH_X = 0.39;
const ARM_HINGE = [0.37, 1.7, 0.125];
const ARM_SWING = 1.2;
// The stack against the city.
const STACK_OF_SPAN = 0.3825;
// The camera's eye on the skyline: just over the tallest roof.
const EYE_OVER_ROOFS = 1.1;
// Through the count the camera pushes in this share of the way to the pad.
const PUSH_IN = 0.16;

const FLAME_CAP = 3072;
const SMOKE_CAP = 4096;
const BOOSTER_FLAME_RATE = 1300; // particles per second at full throttle
const SHIP_FLAME_RATE = 520;
const PAD_CLOUD_RATE = 260;
const TRAIL_RATE = 110;
const DUST_RING = 260;
const EXHAUST = 3.0; // exhaust speed against the engines, stack heights per second
const GRAVITY = 0.9; // stack heights per second squared, on the falling booster

const UP = new THREE.Vector3(0, 1, 0);
// The ship's flaps point across the view, the way it is always pictured.
const QUARTER_TURN = new THREE.Quaternion().setFromAxisAngle(UP, Math.PI / 2);

export function createRocket({ scene, camera, renderer, frame, overlay, insets }) {
  const frustum = new THREE.Frustum();
  const projView = new THREE.Matrix4();
  const sphere = new THREE.Sphere();
  const flame = makePool(FLAME_CAP, THREE.AdditiveBlending, 3);
  const smoke = makePool(SMOKE_CAP, THREE.NormalBlending, 2);
  const boosterGlow = makeGlow();
  const shipGlow = makeGlow();
  // Added on the first launch and kept at zero after: a light coming and
  // going changes every lit material's program, and each change recompiles.
  const light = new THREE.PointLight(0xff9a40, 0, 0, 0);
  const count = makeCountdown(overlay);
  const out = { pos: new THREE.Vector3(), target: new THREE.Vector3() };
  let kit = null;
  let show = null;
  let starting = false;

  // launch swings the camera down to the skyline and stands the site behind
  // the city, on the line the camera already looks along. from is where the
  // camera is now; done runs once it is back there. One show at a time.
  async function launch({ bounds, from, done }) {
    if (starting || show) return false;
    starting = true;
    try {
      if (!kit) kit = await buildKit(renderer);
      const span = Math.max(bounds.maxX - bounds.minX, bounds.maxZ - bounds.minZ, 20);
      const H = span * STACK_OF_SPAN;
      const k = H / STACK_H;
      // The camera turns to the nearest of the city's own axes, so the
      // skyline is square to the view and the streets run away from it.
      const look = camera.getWorldDirection(new THREE.Vector3());
      const fwd = Math.abs(look.x) > Math.abs(look.z)
        ? new THREE.Vector3(Math.sign(look.x) || 1, 0, 0)
        : new THREE.Vector3(0, 0, Math.sign(look.z) || -1);
      const right = new THREE.Vector3().crossVectors(fwd, UP).normalize();
      const center = new THREE.Vector3((bounds.minX + bounds.maxX) / 2, 0, (bounds.minZ + bounds.maxZ) / 2);
      const reach = Math.abs(fwd.x) * (bounds.maxX - bounds.minX) / 2 + Math.abs(fwd.z) * (bounds.maxZ - bounds.minZ) / 2;
      const site = center.clone().addScaledVector(fwd, reach + PAD_HALF * k + span * 0.06);
      // The tower stands on the left of the stack, and the stack flies right.
      const siteX = right.clone().negate();
      const siteQ = new THREE.Quaternion().setFromRotationMatrix(
        new THREE.Matrix4().makeBasis(siteX, UP, new THREE.Vector3().crossVectors(siteX, UP)));
      kit.stand.position.copy(site);
      kit.stand.quaternion.copy(siteQ);
      kit.stand.scale.setScalar(k);
      for (const arm of kit.arms) arm.rotation.y = 0;
      kit.booster.scale.setScalar(k);
      kit.ship.scale.setScalar(k);
      for (const mat of kit.materials) mat.opacity = 1;
      const pts = [];
      for (const x of [bounds.minX, bounds.maxX]) for (const z of [bounds.minZ, bounds.maxZ]) {
        pts.push([x, 0, z], [x, bounds.maxY, z]);
      }
      for (const a of [-1, 1]) for (const b of [-1, 1]) {
        pts.push(site.clone().addScaledVector(siteX, a * PAD_HALF * k).addScaledVector(fwd, b * PAD_HALF * k).toArray());
      }
      pts.push(site.clone().addScaledVector(siteX, TOWER_X * k).addScaledVector(UP, TOWER_TOP * k).toArray());
      // Fitted from level, then lowered to the roofs: the camera looks a
      // little up, across the city at the stack behind it.
      const pose = frame(pts, [-fwd.x, 0, -fwd.z]);
      pose.pos.y = Math.max(bounds.maxY * EYE_OVER_ROOFS, span * 0.03);
      show = {
        t: 0,
        H,
        k,
        mount: site.clone().addScaledVector(UP, MOUNT_Y * k),
        floor: site.clone().setY(site.y + 0.005 * H),
        dir: right,
        pitchAxis: new THREE.Vector3().crossVectors(UP, right).normalize(),
        restQ: siteQ.clone().multiply(QUARTER_TURN),
        from: { pos: from.pos.clone(), target: from.target.clone() },
        pose: { pos: pose.pos.clone(), target: pose.target.clone(), dist: pose.pos.distanceTo(pose.target) },
        done,
        digit: undefined,
        dusted: false,
        padDebt: 0,
        trailDebt: 0,
        booster: stage(),
        ship: stage(),
        hold: null,
        returning: null,
        view: { pos: from.pos.clone(), target: from.target.clone() },
      };
      placeStack(show, 0);
      show.focusRest = focusOf(show, 0);
      show.booster.prev.copy(show.booster.nozzle);
      show.ship.prev.copy(show.ship.nozzle);
      light.distance = span * 1.5;
      light.intensity = 0;
      if (!light.parent) scene.add(light);
      boosterGlow.visible = shipGlow.visible = false;
      scene.add(kit.stand, kit.booster, kit.ship, flame.points, smoke.points, boosterGlow, shipGlow);
      try {
        await renderer.compileAsync(scene, camera);
      } catch { /* the first frame compiles it instead */ }
      return true;
    } finally {
      starting = false;
    }
  }

  function stage() {
    return {
      alive: true,
      free: false,
      nozzle: new THREE.Vector3(),
      prev: new THREE.Vector3(),
      vel: new THREE.Vector3(),
      axis: new THREE.Vector3(0, 1, 0),
      debt: 0,
      // The booster once it is free: its middle, its velocity, how long it
      // has been free and the pitch it let go at.
      center: new THREE.Vector3(),
      speed: new THREE.Vector3(),
      freeT: 0,
      pitch0: 0,
    };
  }

  // tick runs one frame of the show and returns where the camera is, or null
  // when there is no show.
  function tick(dt) {
    if (!show) return null;
    const f = show;
    const step = dt > 0 ? dt : 0;
    f.t += step;
    const t = f.t;
    const s = t - LIFTOFF;
    if (!f.returning) {
      paintCount(f, countdown(t));
      const open = smooth((t - ARMS_OPEN[0]) / (ARMS_OPEN[1] - ARMS_OPEN[0]));
      kit.arms[0].rotation.y = ARM_SWING * open;
      kit.arms[1].rotation.y = -ARM_SWING * open;
      fly(f, s, step);
      burn(f, t, s, step);
      if (f.hold && f.ship.alive && outOfSight(sight(focusOf(f, s), f.k * 0.6))) {
        scene.remove(kit.ship, shipGlow);
        f.ship.alive = false;
      }
      if (!f.ship.alive || s > GIVE_UP_S) startReturn(f);
    }
    let hurry = 1;
    if (f.returning) {
      f.returning.t += step;
      hurry = 3;
      const fade = 1 - smooth(f.returning.t / (RETURN_S * 0.6));
      for (const mat of kit.materials) mat.opacity = fade;
    }
    flame.step(step, flameRamp, f.floor, hurry);
    smoke.step(step, smokeRamp, f.floor, hurry);
    aim(f, t, s);
    if (f.returning && f.returning.t >= RETURN_S) {
      finish(f);
      out.pos.copy(f.from.pos);
      out.target.copy(f.from.target);
    }
    return out;
  }

  // abort cuts the show short: the camera goes home and the site fades.
  function abort() {
    if (show && !show.returning) startReturn(show);
  }

  function startReturn(f) {
    f.returning = { t: 0, pos: f.view.pos.clone(), target: f.view.target.clone() };
    paintCount(f, null);
    boosterGlow.visible = shipGlow.visible = false;
    light.intensity = 0;
  }

  function finish(f) {
    scene.remove(kit.stand, kit.booster, kit.ship, flame.points, smoke.points, boosterGlow, shipGlow);
    flame.clear();
    smoke.clear();
    light.intensity = 0;
    show = null;
    f.done();
  }

  // The stack on its arc at ascent time s, both stages together.
  function pathAt(f, s, base, q) {
    const a = ascent(s);
    base.copy(f.mount).addScaledVector(UP, a.climb * f.H).addScaledVector(f.dir, a.downrange * f.H);
    q.setFromAxisAngle(f.pitchAxis, a.pitch).multiply(f.restQ);
    return a;
  }

  function placeStack(f, s) {
    const q = new THREE.Quaternion();
    const base = new THREE.Vector3();
    pathAt(f, s, base, q);
    setStage(f.booster, kit.booster, base, q, f.H);
    const top = base.clone().addScaledVector(f.booster.axis, BOOSTER_H * f.k);
    setStage(f.ship, kit.ship, top, q, f.H);
  }

  function setStage(st, obj, base, q, H) {
    obj.position.copy(base);
    obj.quaternion.copy(q);
    st.axis.copy(UP).applyQuaternion(q);
    st.nozzle.copy(base).addScaledVector(st.axis, -0.004 * H);
  }

  function fly(f, s, step) {
    const b = f.booster;
    const sh = f.ship;
    b.prev.copy(b.nozzle);
    sh.prev.copy(sh.nozzle);
    if (!b.free && s >= STAGE_S + SEP_S) letGo(f, s);
    if (!b.free) {
      placeStack(f, s);
    } else {
      // The ship runs ahead along the same arc.
      const q = new THREE.Quaternion();
      const base = new THREE.Vector3();
      pathAt(f, shipClock(s), base, q);
      const axis = UP.clone().applyQuaternion(q);
      setStage(sh, kit.ship, base.addScaledVector(axis, BOOSTER_H * f.k), q, f.H);
      if (b.alive) coast(f, step);
    }
    if (step > 0) {
      b.vel.copy(b.nozzle).sub(b.prev).divideScalar(step);
      sh.vel.copy(sh.nozzle).sub(sh.prev).divideScalar(step);
    }
  }

  // The booster lets go with the stack's speed and a shove back from the
  // ship's engines, flips over, and burns back toward the pad.
  function letGo(f, s) {
    const b = f.booster;
    const now = new THREE.Vector3();
    const before = new THREE.Vector3();
    const a = pathAt(f, s, now, new THREE.Quaternion());
    pathAt(f, s - 0.02, before, new THREE.Quaternion());
    b.free = true;
    b.freeT = 0;
    b.pitch0 = a.pitch;
    b.center.copy(now).addScaledVector(b.axis, BOOSTER_H * f.k / 2);
    b.speed.copy(now).sub(before).divideScalar(0.02).addScaledVector(b.axis, -0.12 * f.H);
  }

  function coast(f, step) {
    const b = f.booster;
    b.freeT += step;
    const flip = smooth((b.freeT - 0.4) / 2);
    const pitch = b.pitch0 + (-1.75 - b.pitch0) * flip;
    const q = new THREE.Quaternion().setFromAxisAngle(f.pitchAxis, pitch).multiply(f.restQ);
    const axis = UP.clone().applyQuaternion(q);
    b.speed.y -= GRAVITY * f.H * step;
    const burnK = boostback(b.freeT);
    if (burnK) b.speed.addScaledVector(axis, 1.6 * f.H * burnK * step);
    b.center.addScaledVector(b.speed, step);
    const base = b.center.clone().addScaledVector(axis, -BOOSTER_H * f.k / 2);
    setStage(b, kit.booster, base, q, f.H);
    if (b.freeT > 0.6 && outOfSight(sight(b.center, BOOSTER_H * f.k * 0.55))) {
      scene.remove(kit.booster, boosterGlow);
      b.alive = false;
    }
  }

  // Engines, smoke and light for this frame.
  function burn(f, t, s, step) {
    const b = f.booster;
    const sh = f.ship;
    const H = f.H;
    const climb = ascent(s).climb;
    const flicker = 0.82 + 0.1 * Math.sin(t * 53) + 0.08 * Math.sin(t * 31 + 1.3);
    const bt = !b.alive ? 0 : b.free ? 0.35 * boostback(b.freeT) : boosterThrottle(t);
    const st = sh.alive ? shipThrottle(t) : 0;
    emitFlame(f, b, bt, step, { rate: BOOSTER_FLAME_RATE, r: BOOSTER_R * f.k * 0.85, grow: climb });
    // Hot staging: the ship's engines fire through the vented ring on top of
    // the booster before it lets go.
    emitFlame(f, sh, st, step, { rate: SHIP_FLAME_RATE, r: SHIP_R * f.k * 0.7, grow: climb, vent: !b.free });
    if (!b.free) clouds(f, s, climb, bt, step);
    boosterGlow.visible = bt > 0;
    if (bt > 0) {
      boosterGlow.position.copy(b.nozzle).addScaledVector(b.axis, -0.09 * H);
      boosterGlow.scale.setScalar(H * 0.42 * bt * flicker * (1 + Math.min(1, climb * 0.04)));
    }
    shipGlow.visible = st > 0;
    if (st > 0) {
      const flash = s < STAGE_S + 0.7 ? 1 + 2.4 * (1 - (s - STAGE_S) / 0.7) : 1;
      shipGlow.position.copy(sh.nozzle).addScaledVector(sh.axis, -0.05 * H);
      shipGlow.scale.setScalar(H * 0.24 * st * flicker * flash);
    }
    // The flame lights the city: brightest as the engines catch, then
    // flickering with them. The ship's engines take over after staging.
    const catching = Math.max(0, 1 - Math.abs(s + 1.1) / 0.35);
    if (!b.free && bt > 0) {
      light.position.copy(b.nozzle).addScaledVector(b.axis, -0.2 * H);
      light.intensity = 2.4 * bt * flicker + 2.5 * catching;
    } else if (st > 0) {
      light.position.copy(sh.nozzle);
      light.intensity = 1.2 * st * flicker;
    } else {
      light.intensity = 0;
    }
  }

  // Raptors fire down the stage's axis from a disc under its engines. The
  // spawn points are spread along the path the engines took this frame, so a
  // fast stage still leaves a continuous plume. Higher up, in thinner air,
  // the plume grows longer and wider.
  function emitFlame(f, st, thr, dt, { rate, r, grow, vent = false }) {
    if (thr <= 0) return;
    const H = f.H;
    st.debt += rate * thr * dt;
    const n = Math.floor(st.debt);
    st.debt -= n;
    if (!n) return;
    const [u, w] = basis(st.axis);
    const lifeK = 1 + Math.min(1.5, grow * 0.08);
    const sizeK = 1 + Math.min(2, grow * 0.12);
    const p = new THREE.Vector3();
    const v = new THREE.Vector3();
    for (let i = 0; i < n; i++) {
      const frac = (i + Math.random()) / n;
      const a = Math.random() * Math.PI * 2;
      const rr = r * Math.sqrt(Math.random());
      const cos = Math.cos(a);
      const sin = Math.sin(a);
      p.lerpVectors(st.prev, st.nozzle, frac).addScaledVector(u, cos * rr).addScaledVector(w, sin * rr);
      const speed = EXHAUST * H * (0.85 + Math.random() * 0.3) * (0.55 + 0.45 * thr);
      if (vent) {
        v.copy(u).multiplyScalar(cos * speed * 0.45).addScaledVector(w, sin * speed * 0.45).addScaledVector(st.axis, -speed * 0.1);
      } else {
        v.copy(st.axis).multiplyScalar(-speed)
          .addScaledVector(u, cos * speed * 0.07 * Math.random())
          .addScaledVector(w, sin * speed * 0.07 * Math.random());
      }
      v.addScaledVector(st.vel, 0.5);
      const life = (0.22 + Math.random() * 0.26) * lifeK * (vent ? 0.6 : 1);
      const s0 = r * (1.1 + Math.random() * 0.6) * sizeK;
      flame.spawn(p, v, (1 - frac) * dt, life, s0, s0 * 3.2, 0.5 * thr, 0.6, 0);
    }
  }

  // Steam boils off the pad while the engines are near it, a ring of dust
  // runs out across the ground as the clamps let go, and a trail hangs where
  // the stack has been.
  function clouds(f, s, climb, thr, dt) {
    if (thr <= 0) return;
    const H = f.H;
    const p = new THREE.Vector3();
    const v = new THREE.Vector3();
    const near = Math.max(0, 1 - climb / 2.5);
    f.padDebt += PAD_CLOUD_RATE * thr * near * dt;
    let n = Math.floor(f.padDebt);
    f.padDebt -= n;
    for (let i = 0; i < n; i++) {
      const a = Math.random() * Math.PI * 2;
      const cos = Math.cos(a);
      const sin = Math.sin(a);
      p.copy(f.floor);
      p.x += cos * H * (0.06 + Math.random() * 0.22);
      p.z += sin * H * (0.06 + Math.random() * 0.22);
      p.y += H * 0.02;
      const speed = H * (0.35 + Math.random() * 0.8);
      v.set(cos * speed, H * (0.04 + Math.random() * 0.18), sin * speed);
      const s0 = H * (0.07 + Math.random() * 0.06);
      smoke.spawn(p, v, Math.random() * dt, 4 + Math.random() * 4, s0, s0 * 5.5, 0.6, 0.5, H * 0.025);
    }
    if (!f.dusted && s >= 0) {
      f.dusted = true;
      for (let i = 0; i < DUST_RING; i++) {
        const a = (i / DUST_RING) * Math.PI * 2 + Math.random() * 0.05;
        const cos = Math.cos(a);
        const sin = Math.sin(a);
        p.copy(f.floor);
        p.x += cos * H * 0.25;
        p.z += sin * H * 0.25;
        const speed = H * (2.2 + Math.random() * 1.2);
        v.set(cos * speed, H * 0.05, sin * speed);
        const s0 = H * (0.04 + Math.random() * 0.03);
        smoke.spawn(p, v, 0, 2 + Math.random(), s0, s0 * 6, 0.35, 1.8, 0);
      }
    }
    if (s <= 0) return;
    const st = f.booster;
    f.trailDebt += TRAIL_RATE * dt;
    n = Math.floor(f.trailDebt);
    f.trailDebt -= n;
    for (let i = 0; i < n; i++) {
      const frac = (i + Math.random()) / n;
      p.lerpVectors(st.prev, st.nozzle, frac).addScaledVector(st.axis, -H * (0.35 + Math.random() * 0.3));
      v.set(Math.random() - 0.5, Math.random() - 0.5, Math.random() - 0.5).multiplyScalar(H * 0.12);
      v.addScaledVector(st.axis, -H * 0.25);
      const s0 = H * (0.05 + Math.random() * 0.03);
      smoke.spawn(p, v, (1 - frac) * dt, 2.2 + Math.random() * 1.6, s0, s0 * 4.5, 0.32, 0.7, H * 0.02);
    }
  }

  // The point the camera follows: the middle of the stack, sliding up to the
  // middle of the ship once the ship has lit.
  function focusOf(f, s) {
    const k = smooth((s - STAGE_S) / 1.0);
    return kit.ship.position.clone().addScaledVector(f.ship.axis, f.k * (-0.228 + 0.728 * k));
  }

  // The camera: down to the skyline, a hold on the pad through the count,
  // then onto the rocket as it climbs, and still again once the ship is
  // free. The engines shake it while they are close.
  function aim(f, t, s) {
    const v = f.view;
    if (f.returning) {
      const k = smooth(f.returning.t / RETURN_S);
      v.pos.lerpVectors(f.returning.pos, f.from.pos, k);
      v.target.lerpVectors(f.returning.target, f.from.target, k);
    } else if (t < APPROACH_S) {
      const k = smooth(t / APPROACH_S);
      v.pos.lerpVectors(f.from.pos, f.pose.pos, k);
      v.target.lerpVectors(f.from.target, f.pose.target, k);
    } else if (f.hold) {
      v.pos.copy(f.hold.pos);
      v.target.copy(f.hold.target);
    } else {
      const w = follow(s);
      const focus = focusOf(f, s);
      const d = focus.clone().sub(f.focusRest);
      const push = PUSH_IN * smooth((t - APPROACH_S) / (LIFTOFF + FOLLOW_S[0] - APPROACH_S));
      v.pos.lerpVectors(f.pose.pos, f.pose.target, push);
      v.pos.add(d.set(d.x * 0.6, d.y * 0.92, d.z * 0.6).multiplyScalar(w));
      v.target.copy(f.pose.target).lerp(focus, w);
      if (s >= RELEASE_S) f.hold = { pos: v.pos.clone(), target: v.target.clone() };
    }
    out.pos.copy(v.pos);
    out.target.copy(v.target);
    const amp = f.returning ? 0 : rumble(t) * f.pose.dist * 0.005;
    if (amp > 0) {
      const jx = Math.sin(t * 37.1) * 0.6 + Math.sin(t * 61.7 + 1.1) * 0.4;
      const jy = Math.sin(t * 43.3 + 2.3) * 0.6 + Math.sin(t * 71.9 + 0.4) * 0.4;
      const jz = Math.sin(t * 29.7 + 4.1) * 0.6 + Math.sin(t * 53.3 + 2.9) * 0.4;
      out.pos.x += jx * amp * 0.5;
      out.pos.y += jy * amp * 0.5;
      out.target.x += jz * amp;
      out.target.y += jx * amp;
      out.target.z += jy * amp;
    }
  }

  // What the camera sees of a sphere: whether it is in the frustum, and how
  // many pixels across it is drawn.
  function sight(center, radius) {
    camera.updateMatrixWorld();
    projView.multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse);
    frustum.setFromProjectionMatrix(projView);
    const inView = frustum.intersectsSphere(sphere.set(center, radius));
    const dist = Math.max(1e-6, center.distanceTo(camera.position));
    const pixels = (2 * radius * camera.projectionMatrix.elements[5] * 0.5 * renderer.domElement.height) / dist;
    return { inView, pixels };
  }

  // The count sits in the middle of the top half of the free area. Each
  // number lands, then shrinks and fades through its second.
  function paintCount(f, n) {
    if (n === f.digit) return;
    f.digit = n;
    if (n === null) {
      count.box.style.display = "none";
      return;
    }
    const side = insets();
    count.box.style.left = side.left + "px";
    count.box.style.right = side.right + "px";
    count.box.style.display = "flex";
    count.digit.textContent = String(n);
    for (const anim of count.digit.getAnimations()) anim.cancel();
    count.digit.animate([
      { transform: "scale(1.3)", opacity: 0 },
      { transform: "scale(1)", opacity: 1, offset: 0.1 },
      { transform: "scale(0.35)", opacity: 0 },
    ], { duration: 1000, easing: "cubic-bezier(0.4, 0, 0.7, 1)", fill: "forwards" });
  }

  return { launch, tick, abort, showing: () => !!show || starting };
}

// The boostback burn, as a throttle on the booster's free time: after the flip.
function boostback(freeT) {
  return freeT > 2.6 && freeT < 5 ? 1 : 0;
}

function smooth(x) {
  const k = Math.min(1, Math.max(0, x));
  return k * k * (3 - 2 * k);
}

function basis(axis) {
  const u = Math.abs(axis.y) < 0.9 ? new THREE.Vector3(0, 1, 0) : new THREE.Vector3(1, 0, 0);
  u.cross(axis).normalize();
  const w = new THREE.Vector3().crossVectors(axis, u).normalize();
  return [u, w];
}

function makeGlow() {
  const glow = new THREE.Sprite(new THREE.SpriteMaterial({
    map: glowTexture(),
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    fog: false,
    toneMapped: false,
  }));
  glow.renderOrder = 4;
  return glow;
}

function makeCountdown(parent) {
  const box = document.createElement("div");
  box.setAttribute("aria-hidden", "true");
  box.style.cssText = "position:absolute;top:25%;left:0;right:0;height:0;display:none;" +
    "align-items:center;justify-content:center;pointer-events:none;z-index:2;";
  const digit = document.createElement("span");
  digit.style.cssText = "font-size:clamp(72px,16vh,168px);font-weight:700;line-height:1;" +
    "color:var(--fg);text-shadow:0 0 28px var(--accent),0 0 6px var(--accent);opacity:0;";
  box.append(digit);
  parent.append(box);
  return { box, digit };
}

let models = null;

// The loader and the models are fetched on the first launch, not with the page.
function loadModels() {
  if (!models) {
    models = import("three/addons/loaders/GLTFLoader.js")
      .then(({ GLTFLoader }) => {
        const loader = new GLTFLoader();
        return Promise.all(["superheavy.glb", "starship.glb", "launchstand.glb"].map((name) => loader.loadAsync(MODELS + name)));
      })
      .catch((err) => {
        models = null;
        throw err;
      });
  }
  return models;
}

// The models carry no normals. The stages get smooth ones; the site keeps
// the flat facets the loader gives it, which suit its boxes and lattice.
// Stainless steel lit only by the city's lights reads black, so everything
// reflects a small sky of its own. Every material can fade, for the way back.
async function buildKit(renderer) {
  const [booster, ship, stand] = await loadModels();
  const env = steelSky(renderer);
  const materials = new Set();
  const dress = (root, smooth) => root.traverse((obj) => {
    if (!obj.isMesh) return;
    if (smooth && !obj.geometry.attributes.normal) obj.geometry.computeVertexNormals();
    const mat = obj.material;
    if (smooth) mat.flatShading = false;
    mat.envMap = env;
    mat.envMapIntensity = 1.1;
    mat.fog = !smooth;
    mat.transparent = true;
    mat.needsUpdate = true;
    materials.add(mat);
  });
  dress(booster.scene, true);
  dress(ship.scene, true);
  dress(stand.scene, false);
  return {
    booster: booster.scene,
    ship: ship.scene,
    stand: stand.scene,
    arms: splitArms(stand.scene),
    materials: [...materials],
  };
}

// splitArms takes the two arms out of the chopsticks mesh and hangs each from
// a post at its root, so it can swing open.
function splitArms(stand) {
  const chop = stand.getObjectByName("chopsticks");
  const posts = [new THREE.Group(), new THREE.Group()];
  if (!chop) return posts;
  const src = chop.geometry.index ? chop.geometry.toNonIndexed() : chop.geometry;
  const pos = src.attributes.position;
  const keep = [[], [], []];
  for (let i = 0; i < pos.count; i += 3) {
    let maxX = -Infinity;
    let minZ = Infinity;
    let maxZ = -Infinity;
    for (let j = i; j < i + 3; j++) {
      maxX = Math.max(maxX, pos.getX(j));
      minZ = Math.min(minZ, pos.getZ(j));
      maxZ = Math.max(maxZ, pos.getZ(j));
    }
    const side = maxX < ARM_REACH_X && minZ > 0 ? 0 : maxX < ARM_REACH_X && maxZ < 0 ? 1 : 2;
    keep[side].push(i);
  }
  const pick = (tris, shift) => {
    const geo = new THREE.BufferGeometry();
    for (const name of Object.keys(src.attributes)) {
      const attr = src.attributes[name];
      const n = attr.itemSize;
      const array = new Float32Array(tris.length * 3 * n);
      tris.forEach((first, t) => {
        for (let j = 0; j < 3; j++) for (let c = 0; c < n; c++) array[(t * 3 + j) * n + c] = attr.getComponent(first + j, c);
      });
      geo.setAttribute(name, new THREE.BufferAttribute(array, n));
    }
    if (shift) geo.translate(-shift[0], -shift[1], -shift[2]);
    return geo;
  };
  [0, 1].forEach((side) => {
    const hinge = [ARM_HINGE[0], ARM_HINGE[1], side === 0 ? ARM_HINGE[2] : -ARM_HINGE[2]];
    posts[side].position.set(...hinge);
    posts[side].add(new THREE.Mesh(pick(keep[side], hinge), chop.material));
    chop.parent.add(posts[side]);
  });
  chop.geometry = pick(keep[2], null);
  return posts;
}

function steelSky(renderer) {
  const sky = new THREE.Scene();
  const geo = new THREE.SphereGeometry(10, 32, 16);
  const pos = geo.attributes.position;
  const colors = new Float32Array(pos.count * 3);
  const low = new THREE.Color(0.04, 0.035, 0.05);
  const mid = new THREE.Color(0.42, 0.38, 0.46);
  const high = new THREE.Color(0.95, 0.94, 0.98);
  const c = new THREE.Color();
  for (let i = 0; i < pos.count; i++) {
    const y = pos.getY(i) / 10;
    if (y < 0) c.copy(mid).lerp(low, Math.min(1, -y * 2.5));
    else c.copy(mid).lerp(high, Math.pow(y, 0.6));
    c.toArray(colors, i * 3);
  }
  geo.setAttribute("color", new THREE.BufferAttribute(colors, 3));
  sky.add(new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ vertexColors: true, side: THREE.BackSide })));
  const pmrem = new THREE.PMREMGenerator(renderer);
  const target = pmrem.fromScene(sky, 0.03);
  pmrem.dispose();
  geo.dispose();
  return target.texture;
}
