/** The native Apple apps (`apps/apple`) load this console from their own
 * bundle, on an origin of their own, and forward the API paths to the
 * substrate the user picked. The page therefore cannot read the substrate's
 * address off `window.location`: the shell writes it to `window.substrateShell`
 * before any script runs, and everything that names the substrate's origin or
 * host reads it here. In a browser there is no shell, and the console's own
 * origin IS the substrate's. */

export interface ShellInfo {
  platform: "macos" | "ios"
  /** The substrate's origin, `https://substrate.example.com`. */
  server: string
  /** The app's own version, as its release stamped it. */
  version?: string
  /** The app was released and checks for newer releases itself; a local
   * build was not and never looks. */
  updates?: boolean
}

interface ShellWindow {
  substrateShell?: ShellInfo
  webkit?: {
    messageHandlers?: {
      substrateShell?: { postMessage: (message: unknown) => void }
    }
  }
}

/** The native shell the console runs in, if any. */
export function nativeShell(win: unknown = window): ShellInfo | undefined {
  const info = (win as ShellWindow).substrateShell
  return info && typeof info.server === "string" ? info : undefined
}

/** The substrate's origin: where a link pasted elsewhere must point. */
export function substrateOrigin(win: Window = window): string {
  return nativeShell(win)?.server ?? win.location.origin
}

/** The substrate's host name, which completes a bare repository label. */
export function substrateHostname(win: Window = window): string {
  const shell = nativeShell(win)
  if (!shell) return win.location.hostname
  try {
    return new URL(shell.server).hostname
  } catch {
    return ""
  }
}

/** Ask the shell to show its server picker. */
export function changeServer(win: unknown = window): void {
  ;(win as ShellWindow).webkit?.messageHandlers?.substrateShell?.postMessage({
    type: "changeServer",
  })
}

/** Ask the shell to look for a newer release of the app now. The shell
 * answers natively: an offer to install, or that this is the newest. */
export function checkForUpdates(win: unknown = window): void {
  ;(win as ShellWindow).webkit?.messageHandlers?.substrateShell?.postMessage({
    type: "checkForUpdates",
  })
}
