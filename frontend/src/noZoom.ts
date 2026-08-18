// Pinch-zoom suppression for iOS Safari, which ignores the viewport meta's
// user-scalable=no. Double-tap zoom is handled separately by the CSS
// `* { touch-action: manipulation }` rule, but `manipulation` still allows
// pinch, so the proprietary gesture events and pinch touchmoves are
// cancelled here. Listeners must be non-passive: Safari registers
// document-level touch listeners as passive by default, which silently
// disables preventDefault.
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
