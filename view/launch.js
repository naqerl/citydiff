// /launch: a command the list does not show. With the starship skin on, the
// camera drops to the skyline, Starship on Super Heavy stands on its launch
// mount behind the city, a countdown runs from 5, and the stack flies: a slow
// heavy climb, hot staging, the ship out of the picture, then the camera goes
// back to where it was. Pure timing and flight math; rocket.js draws it.

export const LAUNCH_COMMAND = "/launch";
export const LAUNCH_SKIN = "starship";

// Show time, in seconds from the command.
export const APPROACH_S = 2.4; // the camera swings down to the skyline
export const COUNT_FROM = 5;
export const LIFTOFF = APPROACH_S + COUNT_FROM; // the countdown's 0
export const ARMS_OPEN = [APPROACH_S + 0.5, APPROACH_S + 2.5]; // the chopsticks swing clear
export const IGNITE_S = 1.4; // the engines light this long before liftoff

// Ascent time, in seconds from liftoff.
export const STAGE_S = 8; // hot staging: the ship lights its engines on the booster
export const SEP_S = 0.5; // and the booster lets go this much later
export const FOLLOW_S = [2.2, 4.4]; // the camera eases from the pad onto the stack
export const RELEASE_S = STAGE_S + 1.6; // and stops; the ship leaves the picture
export const GIVE_UP_S = RELEASE_S + 6; // the camera goes home even if the ship is still seen
export const RETURN_S = 2.6; // the way back to where the camera was

// It climbs straight past the tower, then pitches over.
const TURN_AT = 1.5;
// The radius of the turn at its start, in stack heights: a larger one is a
// more vertical arc.
const TURN_RADIUS = 10;
// The ship's clock runs ahead of the stack's once it is free: it is light,
// and its engines push it along the same arc ever faster.
const SHIP_PUSH = 0.6;

// isLaunch: the search box value that runs it, typed in full.
export function isLaunch(value) {
  return String(value || "").trim().toLowerCase() === LAUNCH_COMMAND;
}

// countdown is the number on screen at show time t, or null outside the
// count. Each number holds for a second; the 0 lands on liftoff.
export function countdown(t) {
  const k = t - APPROACH_S;
  if (k < 0 || k >= COUNT_FROM + 1) return null;
  return COUNT_FROM - Math.floor(k);
}

// boosterThrottle: the Raptors light in the last second and a half of the
// count and are at full thrust just before the clamps let go. At staging all
// but a few shut down; those stop when the booster lets go.
export function boosterThrottle(t) {
  const s = t - LIFTOFF;
  if (s < -IGNITE_S) return 0;
  if (s < STAGE_S) return Math.min(1, (s + IGNITE_S) / (IGNITE_S - 0.2));
  if (s < STAGE_S + SEP_S) return 0.15;
  return 0;
}

export function shipThrottle(t) {
  const s = t - LIFTOFF;
  if (s < STAGE_S) return 0;
  return Math.min(1, (s - STAGE_S) / 0.25);
}

// ascent is the stack s seconds after liftoff, in stack heights. A heavy
// rocket leaves slowly: it takes about three and a half seconds to clear its
// own height, then the climb runs away as the propellant burns off. Past
// TURN_AT the downrange distance grows with the square of the climb, a
// parabola, and the nose follows its tangent: tan(pitch) = d(downrange)/d(climb).
export function ascent(s) {
  const u = Math.max(0, s);
  const climb = 0.06 * u * u + 0.012 * u * u * u;
  const past = Math.max(0, climb - TURN_AT);
  return {
    climb,
    downrange: (past * past) / (2 * TURN_RADIUS),
    pitch: Math.atan(past / TURN_RADIUS),
  };
}

// shipClock is where along the arc the ship is at ascent time s: the stack's
// own time until the booster lets go, then ahead of it.
export function shipClock(s) {
  const free = Math.max(0, s - STAGE_S - SEP_S);
  return s + SHIP_PUSH * free * free;
}

// follow is how far the camera has moved from the pad onto the rocket, 0 to 1.
export function follow(s) {
  const k = Math.min(1, Math.max(0, (s - FOLLOW_S[0]) / (FOLLOW_S[1] - FOLLOW_S[0])));
  return k * k * (3 - 2 * k);
}

// rumble shakes the camera, 0 to 1: the engines' thrust, fading as the stack
// climbs away from the pad.
export function rumble(t) {
  const thrust = boosterThrottle(t);
  if (!thrust) return 0;
  return thrust / (1 + 0.8 * ascent(t - LIFTOFF).climb);
}

// outOfSight: nothing of a stage is drawn, outside the frustum or under a pixel.
export function outOfSight({ inView, pixels }) {
  return !inView || pixels < 1;
}
