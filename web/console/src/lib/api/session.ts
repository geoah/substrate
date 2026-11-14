/** The browser's half of the bearer contract: the token secret is minted once
 * — by `POST /login`, `POST /register` or `POST /tokens` — and never leaves
 * localStorage. The fetch layer reads it on every call; a 401 anywhere drops
 * it and fires the expiry handler the app installs at startup (which routes to
 * the login page).
 *
 * A session IS a token record: there is no session concept beside
 * it, which is why the tokens page is also the sessions page and why logging
 * out revokes the record it is holding. Beside the secret we keep the
 * repository that was signed in to, the name of the person who signed in (the
 * password-factor endpoints need it in their body) and the id of the token
 * record this browser holds (what logging out revokes). */

import {
  shellSession as givenSession,
  renamed as shellRenamed,
  signedOut,
} from "@/lib/shell"

// The `substrate.*` keys are the component's name; the tests pin them.
const TOKEN_KEY = "substrate.token"
const REPOSITORY_KEY = "substrate.repository"
const IDENTITY_KEY = "substrate.identity"
// The id of the token record this browser holds: what logging out revokes.
const TOKEN_ID_KEY = "substrate.tokenid"

let unauthorizedHandler: (() => void) | null = null
// The app's sign-in is given to the page once, frozen, and outlives the
// session it held: once the page ends it, it is never read again, or the
// login route would read it as still signed in and send the page back.
let endedSecret: string | null = null

// The name the page renamed the app's sign-in's repository to, for the
// sign-in it renamed: the session the app gave is frozen for the page's
// life, and the app gives the new name at the next load.
let renamedTo: { secret: string; repository: string } | null = null

/** The app's sign-in, until the page ends it, under the name the page last
 * renamed its repository to. */
function shellSession() {
  const given = givenSession()
  if (!given || given.secret === endedSecret) return undefined
  return renamedTo?.secret === given.secret
    ? { ...given, repository: renamedTo.repository }
    : given
}

/** Whether the app holds the page's sign-in, now or until the page ended it:
 * then the app, not the page, revokes the token. */
export function appHoldsSession(): boolean {
  return givenSession() !== undefined
}
// Every cached answer belongs to ONE repository, and no query key names which:
// signing in as somebody else in the same tab would otherwise keep answering
// from the previous repository's cache — a sidebar listing kinds this
// repository never imported, whose collection then 404s as `unknown
// collection`. The handler the app installs drops the cache; session.ts fires
// it on every identity change and imports no cache itself.
let sessionChangedHandler: (() => void) | null = null

export function getToken(): string | null {
  // In the Apple app the app holds the sign-in, in the Keychain, and the page
  // keeps none of its own.
  const shell = shellSession()
  if (shell) return shell.secret
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

/** The repository this browser is signed in to. It is also what the
 * password-factor endpoints need in their body, so the account page never has
 * to ask for it. */
export function getRepository(): string | null {
  const shell = shellSession()
  if (shell) return shell.repository
  try {
    return localStorage.getItem(REPOSITORY_KEY)
  } catch {
    return null
  }
}

/** The name the person signed in to this browser signs in with: what the
 * password-factor endpoints name in their body, so the account page never has
 * to ask for it. A session from before identities (decision 0274) kept none,
 * and its person signs in with the repository's name. */
export function getIdentityName(): string | null {
  const shell = shellSession()
  if (shell) return shell.identity ?? shell.repository
  try {
    return (
      localStorage.getItem(IDENTITY_KEY) ?? localStorage.getItem(REPOSITORY_KEY)
    )
  } catch {
    return null
  }
}

/** The token record this browser is holding, so the tokens page can mark its
 * own row and logging out can delete it. */
export function getTokenId(): string | null {
  const shell = shellSession()
  if (shell) return shell.tokenId
  try {
    return localStorage.getItem(TOKEN_ID_KEY)
  } catch {
    return null
  }
}

// The writes below store the token secret in localStorage AS CLEAR TEXT, and
// CodeQL names each one (js/clear-text-storage-of-sensitive-data). That is
// the design, not an oversight:
//
//   - The secret IS the session. The console holds a bearer token like every
//     other client; there is no cookie and no server-side session to keep it
//     in instead, and it has to survive a reload somewhere.
//   - Encrypting it here would be theater. The page's own script would need
//     the key, on the same origin, so anything that can read localStorage
//     (same-origin script, an XSS foothold, a person at this keyboard) could
//     read the key beside the ciphertext. That is obfuscation, not
//     protection, and it would still be flagged as the secret it is.
//
// What actually bounds the exposure: the stored value is one REVOCABLE token
// record (the tokens page lists it, logout deletes it, expiry is enforced
// server-side at authenticate), scoped to one repository, and never the
// password, which is not stored anywhere. A 401 drops it (sessionExpired).

/** Store just the secret — the login probe path, before the repository and
 * the token id are known, and the tests. `saveSession` is the full write. */
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
  sessionChanged()
}

/** The full sign-in write: the secret, the repository, the held token's id
 * and the person's name, as a mint (login/register/token) hands them back.
 * Clear text on purpose; the block above setToken says why. */
export function saveSession(
  secret: string,
  repository: string,
  tokenId: string,
  identity?: string
): void {
  localStorage.setItem(TOKEN_KEY, secret)
  localStorage.setItem(REPOSITORY_KEY, repository)
  localStorage.setItem(TOKEN_ID_KEY, tokenId)
  if (identity) localStorage.setItem(IDENTITY_KEY, identity)
  else localStorage.removeItem(IDENTITY_KEY)
  sessionChanged()
}

/** The repository's name after a rename: the token and its id stay, since
 * a rename moves neither. In the Apple app the app holds the sign-in, so it
 * is told, keeps the name with it, and gives it to the page at its next
 * load; the page reads the new name at once. */
export function setRepository(repository: string): void {
  const shell = shellSession()
  if (shell) {
    renamedTo = { secret: shell.secret, repository }
    shellRenamed(shell.tokenId, repository)
  } else {
    localStorage.setItem(REPOSITORY_KEY, repository)
  }
  sessionChanged()
}

/** Drops everything `hasSession` reads. In the Apple app that is the app's
 * sign-in, so the app is told to forget it. */
export function clearSession(): void {
  dropStored()
  const shell = shellSession()
  if (shell) {
    endedSecret = shell.secret
    signedOut(shell.tokenId)
  }
  sessionChanged()
}

function dropStored(): void {
  try {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(REPOSITORY_KEY)
    localStorage.removeItem(IDENTITY_KEY)
    localStorage.removeItem(TOKEN_ID_KEY)
  } catch {
    /* storage disabled — nothing to drop */
  }
}

/** In the Apple app, a token this page kept from a password sign-in before
 * the app held the sign-in is still live: it is revoked with itself, as
 * signing out does, and dropped. One the server would not revoke now is kept
 * for the next start, rather than forgotten while live. */
export async function retireStoredSession(
  fetcher: typeof fetch = fetch
): Promise<void> {
  const shell = shellSession()
  if (!shell) return
  let token: string | null, id: string | null
  try {
    token = localStorage.getItem(TOKEN_KEY)
    id = localStorage.getItem(TOKEN_ID_KEY)
  } catch {
    return
  }
  if (!token) return
  if (token !== shell.secret) {
    // Without its record's id a kept token cannot be revoked here, and
    // dropping it would forget a live token: it is left as it is.
    if (!id) return
    try {
      const res = await fetcher(`/tokens/${encodeURIComponent(id)}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
      })
      // A 401 or 404 is a token already gone.
      if (!res.ok && res.status !== 401 && res.status !== 404) return
    } catch {
      return
    }
  }
  dropStored()
}

export function hasSession(): boolean {
  return getToken() !== null
}

/** Installed once at startup by whoever owns the query cache. It fires on
 * every write below, so a new call site cannot forget to drop the previous
 * repository's answers. */
export function setSessionChangedHandler(handler: (() => void) | null): void {
  sessionChangedHandler = handler
}

function sessionChanged(): void {
  sessionChangedHandler?.()
}

/** Installed once at startup with a router-aware redirect; the fetch layer
 * calls `sessionExpired` and never imports the router itself. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler
}

/** A 401 came back on an authenticated call: the token is dead whatever the
 * page was doing, so the session drops before anyone re-renders. */
export function sessionExpired(): void {
  clearSession()
  unauthorizedHandler?.()
}
