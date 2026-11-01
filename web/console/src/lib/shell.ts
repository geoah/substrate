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
  /** The app signs in through the substrate's own sign-in page, in Safari
   * or the default browser, where the substrate can grant it full access
   * (decision 0204), so the sign-in page asks the app rather than taking
   * the password itself. */
  signIn?: boolean
}

/** What the app's sign-in answers: the session the console keeps, or why
 * there is none. */
export type ShellSignIn =
  | { secret: string; repository: string; tokenId: string }
  | { cancelled: true }
  | { error: string }

interface ShellWindow {
  substrateShell?: ShellInfo
  webkit?: {
    messageHandlers?: {
      substrateShell?: { postMessage: (message: unknown) => void }
      substrateShellSignIn?: {
        postMessage: (message: unknown) => Promise<ShellSignIn>
      }
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

/** Whether the sign-in page should hand signing in to the app: the app
 * offers it and the substrate's authorization server lists `full`. A server
 * older than full access, or one whose metadata does not answer, keeps the
 * password form. */
export async function shellSignsIn(
  win: unknown = window,
  fetcher: typeof fetch = fetch
): Promise<boolean> {
  const w = win as ShellWindow
  if (
    !nativeShell(w)?.signIn ||
    !w.webkit?.messageHandlers?.substrateShellSignIn
  )
    return false
  try {
    const res = await fetcher("/.well-known/oauth-authorization-server")
    if (!res.ok) return false
    const meta = (await res.json()) as { scopes_supported?: unknown }
    return (
      Array.isArray(meta.scopes_supported) &&
      meta.scopes_supported.includes("full")
    )
  } catch {
    return false
  }
}

/** Ask the app to sign in: it opens the substrate's own sign-in and
 * approval page and answers once the owner approved, denied or closed it. */
export async function shellSignIn(win: unknown = window): Promise<ShellSignIn> {
  const handler = (win as ShellWindow).webkit?.messageHandlers
    ?.substrateShellSignIn
  if (!handler) return { error: "This app can't sign in for the console." }
  return handler.postMessage({ type: "signIn" })
}

/** Stop a sign-in waiting on the browser, which the owner may have left. */
export function cancelShellSignIn(win: unknown = window): void {
  ;(win as ShellWindow).webkit?.messageHandlers?.substrateShell?.postMessage({
    type: "cancelSignIn",
  })
}
