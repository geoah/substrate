/** The native Apple apps (`apps/<app>/apple`) load this console from their own
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
  /** The app came from TestFlight or the App Store, which updates it. Such
   * an app opens no account creation: one that did would owe a way to
   * delete the account too (App Review guideline 5.1.1(v)). */
  appStore?: boolean
  /** The publisher's privacy policy, as the release stamped it; absent in a
   * local build. App Review wants it reachable inside the app. */
  privacyPolicy?: string
  /** The sign-in the app holds for this server, given to the page before
   * any script runs (decision 0204). The page reads its session here and
   * keeps none of its own; signing out tells the app. Absent where the app
   * leaves signing in to the console's own page, on a server that cannot
   * grant the app full access. */
  session?: ShellSession
}

/** The app's sign-in, as the console holds a password sign-in's. */
export interface ShellSession {
  secret: string
  repository: string
  tokenId: string
  /** The name the person signs in with (decision 0274); an app from before
   * identities gives none, and its person's is the repository's. */
  identity?: string
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

/** The sign-in the app holds for the page, if it holds one. */
export function shellSession(
  // globalThis, not window: the API modules that read the session also run
  // where there is no window, and are a browser's own there.
  win: unknown = globalThis
): ShellSession | undefined {
  const session = nativeShell(win)?.session
  return session &&
    typeof session.secret === "string" &&
    typeof session.repository === "string" &&
    typeof session.tokenId === "string" &&
    (session.identity === undefined || typeof session.identity === "string")
    ? session
    : undefined
}

/** Tell the app the repository the sign-in holding `tokenId` opens is now
 * named `repository` (decision 0287): the app keeps the new name with the
 * sign-in and gives it to the page at its next load. */
export function renamed(
  tokenId: string,
  repository: string,
  win: unknown = globalThis
): void {
  ;(win as ShellWindow).webkit?.messageHandlers?.substrateShell?.postMessage({
    type: "renamed",
    tokenId,
    repository,
  })
}

/** Tell the app the sign-in holding `tokenId` ended: the page signed out,
 * or the server refused the token. The app revokes it and asks for a sign-in
 * again; naming the token keeps a late message from ending a newer one. */
export function signedOut(tokenId: string, win: unknown = globalThis): void {
  ;(win as ShellWindow).webkit?.messageHandlers?.substrateShell?.postMessage({
    type: "signedOut",
    tokenId,
  })
}
