/** The browser's half of the bearer contract: the token secret is minted once
 * — by `POST /login`, `POST /register` or `POST /tokens` — and never leaves
 * localStorage. The fetch layer reads it on every call; a 401 anywhere drops
 * it and fires the expiry handler the app installs at startup (which routes to
 * the login page).
 *
 * A session IS a token record: there is no session concept beside
 * it, which is why the tokens page is also the sessions page and why logging
 * out revokes the record it is holding. Beside the secret we keep the signed-in
 * username (the password-factor endpoints need it in their body) and the id of
 * the token record this browser holds (what logging out revokes). */

// The `substrate.*` keys are the component's name; the tests pin them.
const TOKEN_KEY = "substrate.token"
const USER_KEY = "substrate.username"
// The id of the token record this browser holds: what logging out revokes.
const TOKEN_ID_KEY = "substrate.tokenid"

let unauthorizedHandler: (() => void) | null = null

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

/** The signed-in user's name. It is also what the password-factor endpoints
 * need in their body, so the account page never has to ask for it. */
export function getUsername(): string | null {
  try {
    return localStorage.getItem(USER_KEY)
  } catch {
    return null
  }
}

/** The token record this browser is holding, so the tokens page can mark its
 * own row and logging out can delete it. */
export function getTokenId(): string | null {
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

/** Store just the secret — the login probe path, before the username/id are
 * known, and the tests. `saveSession` is the full write. */
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

/** The full sign-in write: the secret, the username, and the held token's id,
 * as a mint (login/register/token) hands them back. Clear text on purpose;
 * the block above setToken says why. */
export function saveSession(
  secret: string,
  username: string,
  tokenId: string
): void {
  localStorage.setItem(TOKEN_KEY, secret)
  localStorage.setItem(USER_KEY, username)
  localStorage.setItem(TOKEN_ID_KEY, tokenId)
}

/** Drops everything `hasSession` reads. */
export function clearSession(): void {
  try {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
    localStorage.removeItem(TOKEN_ID_KEY)
  } catch {
    /* storage disabled — nothing to drop */
  }
}

export function hasSession(): boolean {
  return getToken() !== null
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

/** `substrate…9f2c` — the footer's identity line without ever showing the secret. */
export function maskedToken(token: string): string {
  if (token.length <= 8) return "token"
  return `${token.slice(0, 4)}…${token.slice(-4)}`
}
