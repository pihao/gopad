// Belt-and-braces zoom suppression for touch devices. The `* { touch-action:
// pan-x pan-y }` CSS rule should block pinch and double-tap zoom on its own,
// but iOS Safari has historically honored touch-action only on the touched
// element (and unevenly across versions), so its proprietary gesture events
// and pinch touchmoves are cancelled here as well. Listeners must be
// non-passive: Safari registers document-level touch listeners as passive by
// default, which silently disables preventDefault.
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
