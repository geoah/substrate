/** How a scroll the console starts itself should move: smoothly, unless the
 * reader asked the system for less motion. CSS cannot reach a scroll started
 * from script, so every such call asks here. */
export function scrollMotion(): ScrollBehavior {
  const query =
    typeof window !== "undefined" && typeof window.matchMedia === "function"
      ? window.matchMedia("(prefers-reduced-motion: reduce)")
      : undefined
  return query?.matches ? "auto" : "smooth"
}
