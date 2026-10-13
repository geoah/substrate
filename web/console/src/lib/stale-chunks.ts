/** A tab left open across a deploy still names the chunk files of the build it
 * loaded. The server answers a missing asset with 404 (spaHandler,
 * internal/api/api.go), so the first lazy import after the deploy (the YAML
 * and Markdown editors, the highlighter) fails, and Vite reports it as
 * `vite:preloadError` before it rejects. One reload loads the new index.html
 * and the chunk names it carries. */

/** Where the time of the last such reload is kept, for this tab only. */
const RELOADED_AT = "substrate.console.staleChunkReloadAt"

/** A chunk that fails again this soon after a reload is missing from the new
 * build too: reloading again would loop, so that failure is left to surface. */
const RELOAD_GUARD_MS = 60_000

/** Reload `win` on a failed chunk load, at most once per guard window. The
 * event is not cancelled, so the import still rejects with the real error
 * while the page reloads. */
export function reloadOnStaleChunk(
  win: Pick<Window, "addEventListener" | "location" | "sessionStorage">,
  now: () => number = Date.now
): void {
  win.addEventListener("vite:preloadError", () => {
    try {
      const last = Number(win.sessionStorage.getItem(RELOADED_AT))
      if (last && now() - last < RELOAD_GUARD_MS) return
      win.sessionStorage.setItem(RELOADED_AT, String(now()))
    } catch {
      // Without storage there is no way to tell a second failure from the
      // first, and a reload that cannot be counted could loop.
      return
    }
    win.location.reload()
  })
}
