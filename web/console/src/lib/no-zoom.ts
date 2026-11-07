/** The console is an app, not a document, and does not zoom. The viewport tag
 * in index.html caps the scale (which also stops iOS zooming into a focused
 * field), and `touch-action` in index.css stops double-tap zoom, but Safari
 * and WKWebView ignore `user-scalable=no` for a pinch, so the pinch's own
 * gesture events are refused here. */
export function preventPinchZoom(win: Pick<Window, "addEventListener">) {
  const refuse = (event: Event) => event.preventDefault()
  for (const type of ["gesturestart", "gesturechange", "gestureend"]) {
    win.addEventListener(type, refuse, { passive: false })
  }
}
