/** Recovering a tab that outlived its build.
 *
 * The lazy lenses (the YAML editor, shiki) are content-hashed chunks named by
 * the index.html the tab loaded. A rebuild renames them, so the first click on
 * a lens in a tab that was open across the deploy imports a URL the server no
 * longer has: Vite fires `vite:preloadError` and the lens never mounts. Nothing
 * in the running page can fix that, because the page holds the old names; only
 * a reload fetches the new index.html and with it the new hashes.
 *
 * THE GUARD, and the trade it makes (#54.5). A reload has to be limited,
 * because a chunk that is genuinely missing — a broken deploy, a half-uploaded
 * build — would otherwise reload forever, and a reload loop is a worse failure
 * than the error it hides. This used to be "once per tab session", which is
 * the safest possible rule and slightly too strict: a long-lived tab that
 * survives TWO deploys self-recovered from the first and was stuck on the
 * second, for the life of the tab.
 *
 * So the guard is now a WINDOW and a CEILING, which separate the two cases on
 * the thing that actually distinguishes them — time. A reload loop is
 * seconds apart; a second real deploy is minutes or days apart.
 *
 *   - at most one reload per RELOAD_WINDOW_MS, so a loop cannot form; and
 *   - at most MAX_RELOADS in the tab's life, so a permanently broken deploy
 *     costs a bounded number of reloads rather than one every window forever.
 *
 * A tab therefore recovers from three separate deploys instead of one, and a
 * genuinely missing chunk still gets at most three reloads spread over at
 * least twenty minutes before the error is left to throw, where the browser
 * console reports it.
 */

const reloadedMark = "substrate.chunk-reload"

/** How long a reload suppresses the next one. Comfortably longer than a loop
 * (which is seconds) and comfortably shorter than the gap between deploys. */
const RELOAD_WINDOW_MS = 10 * 60 * 1000

/** The tab's lifetime ceiling. Three deploys is more than any real session
 * sits through; past it, something is broken in a way reloading will not fix. */
const MAX_RELOADS = 3

/** The window surface this needs, so a test can hand it a fake rather than
 * navigate jsdom. */
export interface ChunkReloadWindow {
  addEventListener(type: string, listener: (event: Event) => void): void
  removeEventListener(type: string, listener: (event: Event) => void): void
  location: { reload: () => void }
  sessionStorage: Pick<Storage, "getItem" | "setItem">
}

/** Installs the reload guard. Returns the uninstaller (tests use it; the app
 * keeps the listener for the life of the document).
 *
 * `now` is injected so a test can move time rather than wait ten minutes. */
export function installChunkReload(
  win: ChunkReloadWindow = window,
  now: () => number = () => Date.now()
): () => void {
  const onPreloadError = (event: Event) => {
    if (!mayReload(win, now())) return
    // Vite rethrows the error to the window unless the event is cancelled, and
    // an uncaught error racing a reload is noise about a state we are already
    // leaving.
    event.preventDefault()
    win.location.reload()
  }
  win.addEventListener("vite:preloadError", onPreloadError)
  return () => win.removeEventListener("vite:preloadError", onPreloadError)
}

interface ReloadMark {
  /** How many reloads this tab has spent. */
  n: number
  /** When the last one was, as epoch ms. */
  at: number
}

/** Whether to reload now, recording it when the answer is yes. */
function mayReload(win: ChunkReloadWindow, at: number): boolean {
  try {
    const mark = readMark(win)
    if (mark) {
      if (mark.n >= MAX_RELOADS) return false
      if (at - mark.at < RELOAD_WINDOW_MS) return false
    }
    win.sessionStorage.setItem(
      reloadedMark,
      JSON.stringify({ n: (mark?.n ?? 0) + 1, at } satisfies ReloadMark)
    )
    return true
  } catch {
    // No storage (private mode, a sandboxed frame) means no memory to guard
    // with, and an unguarded reload can loop. Take the error instead.
    return false
  }
}

function readMark(win: ChunkReloadWindow): ReloadMark | undefined {
  const raw = win.sessionStorage.getItem(reloadedMark)
  if (raw === null) return undefined
  try {
    const parsed: unknown = JSON.parse(raw)
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      typeof (parsed as ReloadMark).n === "number" &&
      typeof (parsed as ReloadMark).at === "number"
    ) {
      return parsed as ReloadMark
    }
  } catch {
    // Not JSON: a mark written by the previous one-shot guard, which stored
    // "1". It means one reload has happened, at an unknown time — so treat it
    // as spent and long ago, which lets the window admit the next deploy
    // rather than stranding a tab that upgraded mid-session.
  }
  return { n: 1, at: 0 }
}
