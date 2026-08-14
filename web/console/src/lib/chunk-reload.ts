/** Recovering a tab that outlived its build.
 *
 * The lazy lenses (the YAML editor, shiki) are content-hashed chunks named by
 * the index.html the tab loaded. A rebuild renames them, so the first click on
 * a lens in a tab that was open across the deploy imports a URL the server no
 * longer has: Vite fires `vite:preloadError` and the lens never mounts. Nothing
 * in the running page can fix that, because the page holds the old names; only
 * a reload fetches the new index.html and with it the new hashes.
 *
 * So: reload, ONCE per tab session. A chunk that is genuinely missing (a broken
 * deploy, a half-uploaded build) would otherwise reload forever, and a reload
 * loop is a worse failure than the error it hides. The second preload error in
 * the same session is left to throw, where the browser console reports it. */

const reloadedMark = "substrate.chunk-reload"

/** The window surface this needs, so a test can hand it a fake rather than
 * navigate jsdom. */
export interface ChunkReloadWindow {
  addEventListener(type: string, listener: (event: Event) => void): void
  removeEventListener(type: string, listener: (event: Event) => void): void
  location: { reload: () => void }
  sessionStorage: Pick<Storage, "getItem" | "setItem">
}

/** Installs the one-shot reload. Returns the uninstaller (tests use it; the app
 * keeps the listener for the life of the document). */
export function installChunkReload(
  win: ChunkReloadWindow = window
): () => void {
  const onPreloadError = (event: Event) => {
    if (alreadyReloaded(win)) return
    // Vite rethrows the error to the window unless the event is cancelled, and
    // an uncaught error racing a reload is noise about a state we are already
    // leaving.
    event.preventDefault()
    win.location.reload()
  }
  win.addEventListener("vite:preloadError", onPreloadError)
  return () => win.removeEventListener("vite:preloadError", onPreloadError)
}

function alreadyReloaded(win: ChunkReloadWindow): boolean {
  try {
    if (win.sessionStorage.getItem(reloadedMark) !== null) return true
    win.sessionStorage.setItem(reloadedMark, "1")
    return false
  } catch {
    // No storage (private mode, a sandboxed frame) means no memory to guard
    // with, and an unguarded reload can loop. Take the error instead.
    return true
  }
}
