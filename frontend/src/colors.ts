// User colors: the wire format stays a single HSL hue (UserInfo.hue), but
// the pool and every CSS color derived from it live here, calibrated toward
// perceptual evenness. HSL's hue axis is not perceptually uniform — the
// yellow-green span (60°–110°) reads as one indistinct chartreuse while the
// blues stretch far apart — and a fixed lightness makes ambers glare and
// blues turn muddy on the dark background. So the pool skips the chartreuse
// span, spaces the rest roughly evenly to the eye, and each entry carries a
// lightness correction toward equal perceived brightness.
const POOL: { hue: number; lightnessShift: number }[] = [
  { hue: 0, lightnessShift: 0 }, // red
  { hue: 25, lightnessShift: 0 }, // orange
  { hue: 50, lightnessShift: -6 }, // amber (glares at the base lightness)
  { hue: 120, lightnessShift: -4 }, // green
  { hue: 170, lightnessShift: -2 }, // teal
  { hue: 205, lightnessShift: 0 }, // sky
  { hue: 235, lightnessShift: 7 }, // blue (muddy at the base lightness)
  { hue: 285, lightnessShift: 5 }, // purple
  { hue: 320, lightnessShift: 2 }, // pink
];

export const HUES = POOL.map((e) => e.hue);

/** The pool's smallest gap; excluding a hue blocks only the entries it sits on. */
export const MIN_HUE_DISTANCE = 25;

/** Distance between two hues on the 360° color wheel. */
function hueDistance(a: number, b: number): number {
  const d = Math.abs(((a % 360) + 360) % 360 - ((b % 360) + 360) % 360);
  return Math.min(d, 360 - d);
}

// Arbitrary hues (stored identities predating the pool, or other clients)
// borrow the calibration of the nearest pool entry.
function lightnessShift(hue: number): number {
  let best = POOL[0];
  for (const e of POOL) {
    if (hueDistance(e.hue, hue) < hueDistance(best.hue, hue)) best = e;
  }
  return best.lightnessShift;
}

export function caretColor(hue: number): string {
  return `hsl(${hue}, 90%, ${55 + lightnessShift(hue)}%)`;
}

export function nameColor(hue: number): string {
  return `hsl(${hue}, 90%, ${65 + lightnessShift(hue)}%)`;
}

export function labelBackground(hue: number): string {
  return `hsl(${hue}, 60%, 30%)`;
}

export function selectionBackground(hue: number): string {
  return `hsla(${hue}, 90%, 50%, 0.22)`;
}

/**
 * Pick a random hue from the pool, avoiding hues close to any in `exclude`
 * (other users' colors, or the caller's current one to force a visible
 * change). If every pool hue is taken, fall back to the one farthest from
 * the excluded set.
 */
export function randomHue(exclude: number[] = []): number {
  const free = HUES.filter((h) => exclude.every((e) => hueDistance(h, e) >= MIN_HUE_DISTANCE));
  if (free.length > 0) return free[Math.floor(Math.random() * free.length)];
  let best = HUES[0];
  let bestDist = -1;
  for (const h of HUES) {
    const d = Math.min(...exclude.map((e) => hueDistance(h, e)));
    if (d > bestDist) {
      bestDist = d;
      best = h;
    }
  }
  return best;
}
