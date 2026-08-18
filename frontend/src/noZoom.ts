// Zoom suppression for iOS Safari, which ignores the viewport meta's
// user-scalable=no. The CSS `* { touch-action: manipulation }` rule kills
// double-tap smart zoom, and `manipulation` still allows pinch, so the
// proprietary gesture events and pinch touchmoves are cancelled here. The
// sidebar additionally cancels the second tap of a double-tap (below):
// WebKit bugs let double-tap zoom slip through even with touch-action,
// notably on fixed-position elements. Listeners must be non-passive:
// Safari registers document-level touch listeners as passive by default,
// which silently disables preventDefault.
for (const type of ["gesturestart", "gesturechange", "gestureend"]) {
  document.addEventListener(type, (e) => e.preventDefault(), { passive: false });
}

// iOS TouchEvents carry a proprietary `scale`; any value but 1 is a pinch.
document.addEventListener(
  "touchmove",
  (e) => {
    const scale = (e as TouchEvent & { scale?: number }).scale;
    if (scale !== undefined && scale !== 1) e.preventDefault();
  },
  { passive: false },
);

// --- Sidebar double-tap zoom suppression --------------------------------
//
// `touch-action: manipulation` is the standard fix, but WebKit has open
// bugs where iOS still zooms: "touch-action: manipulation" does not prevent
// double-tap-to-zoom (bugs.webkit.org/319664), and double-tap zoom ignores
// touch-action on absolutely positioned elements — the mobile sidebar is
// position: fixed (bugs.webkit.org/218015). As a belt-and-braces fallback,
// the second tap of a rapid pair landing in the sidebar is cancelled, which
// stops the browser from recognizing a double-tap zoom. Scoped to the
// sidebar so the editor keeps its double-tap word selection and taps
// elsewhere stay untouched.

const DOUBLE_TAP_MS = 300;
const DOUBLE_TAP_FINGER_SLOP = 30; // px of drift tolerated between the two taps

// `performance.now()` is measured from navigation start, so a sentinel far in
// the past guarantees the very first tap never counts as a second tap.
let lastTapAt = -Infinity;
let lastTapX = 0;
let lastTapY = 0;

function isSidebarTouch(e: TouchEvent): boolean {
  return (e.target as Element | null)?.closest?.("#sidebar, #sidebar-toggle") != null;
}

document.addEventListener(
  "touchend",
  (e) => {
    const touch = e.changedTouches[0];
    if (!touch || !isSidebarTouch(e)) return;
    const now = performance.now();
    const dx = touch.clientX - lastTapX;
    const dy = touch.clientY - lastTapY;
    if (now - lastTapAt <= DOUBLE_TAP_MS && dx * dx + dy * dy <= DOUBLE_TAP_FINGER_SLOP ** 2) {
      e.preventDefault(); // second tap of a double-tap: cancel it, no zoom
    }
    lastTapAt = now;
    lastTapX = touch.clientX;
    lastTapY = touch.clientY;
  },
  { passive: false },
);
