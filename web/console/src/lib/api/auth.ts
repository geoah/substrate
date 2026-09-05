/** The substrate's door: registration behind an invite code, login, the
 * password-factor credential changes, and the token records that ARE sessions.
 *
 * These endpoints carry NO `/api` prefix — they sit at the root
 * (`/register`, `/login`, `/password`, `/totp`, `/tokens`). The password-factor
 * ones (`/password`, `/totp`, and their enroll steps) are deliberately
 * ANONYMOUS: the current password and TOTP code travel in the body, and a
 * bearer on them is refused outright (RB-6). Login and registration end LOGGED
 * IN — the response carries a freshly minted token secret, shown once. */

import { request } from "./http"
import { clearSession, getToken, getTokenId } from "./session"
import type {
  MintedToken,
  OperationalList,
  TokenInfo,
  TOTPEnrollment,
} from "./types"

/** How many digits a TOTP code has; the substrate accepts nothing else. */
export const CODE_DIGITS = 6

/** Strips the spacing authenticators display codes with ("123 456",
 * "123-456") and returns the bare digits, or null when the result is not
 * exactly six digits. */
export function normalizeCode(input: string): string | null {
  const code = input.replace(/[\s-]/g, "")
  return new RegExp(`^\\d{${CODE_DIGITS}}$`).test(code) ? code : null
}

// ── registration ────────────────────────────────────────────────────────────

/** Step one of registration: the invite code and a username buy a TOTP seed.
 * It writes NOTHING — the caller holds the seed and hands it back with one
 * code, so an abandoned registration leaves no row. */
export function registerEnroll(
  inviteCode: string,
  username: string
): Promise<TOTPEnrollment> {
  return request<TOTPEnrollment>(
    "POST",
    "/register/enroll",
    { inviteCode, username },
    { anonymous: true }
  )
}

export interface RegisterInput {
  inviteCode: string
  username: string
  password: string
  totpSecret: string
  totpCode: string
  label?: string
  /** The DNS-style authority the repository will own, the home of every kind
   * its user declares. Absent, the substrate names it `<username>.<its host>`. */
  authority?: string
  /** A client-generated age recipient; absent asks the substrate to mint the
   * pair and return the identity once. */
  recoveryPublicKey?: string
}

/** What registration hands back beyond the token: the recovery identity
 * (present only when the server minted the pair; shown once, never stored)
 * and the enrolled recipient. */
export interface RegisterResult extends MintedToken {
  /** The authority the repository was created with, echoed so a client that
   * sent none learns the default it got. */
  authority?: string
  recoveryKey?: string
  recoveryPublicKey?: string
}

/** Step two: the same code proves the seed, and one transaction creates the
 * repository, the sealed material, the credential, the recovery key and the
 * first token. Registration ends LOGGED IN — the response carries the token
 * secret, and the recovery identity rides beside it, once. */
export function register(input: RegisterInput): Promise<RegisterResult> {
  return request<RegisterResult>("POST", "/register", input, {
    anonymous: true,
  })
}

// ── login ─────────────────────────────────────────────────────────────────

/** Login mints a token RECORD and returns its secret once: there is no session
 * concept beside it, the console holds a token like every other client. */
export function login(
  username: string,
  password: string,
  totpCode: string,
  label = "console"
): Promise<MintedToken> {
  return request<MintedToken>(
    "POST",
    "/login",
    { username, password, totpCode, label },
    { anonymous: true }
  )
}

// ── password-factor credential changes ──────────────────────────────────────

/** The password-factor rule (RB-6): the CURRENT password and code travel in
 * the body. A bearer token alone is refused with 403 — so these calls are
 * deliberately anonymous. */
export async function changePassword(
  username: string,
  password: string,
  totpCode: string,
  newPassword: string
): Promise<void> {
  await request<{ username: string }>(
    "POST",
    "/password",
    { username, password, totpCode, newPassword },
    { anonymous: true }
  )
}

/** Re-enrollment step one: prove both current factors, receive a fresh seed. */
export function totpEnroll(
  username: string,
  password: string,
  totpCode: string
): Promise<TOTPEnrollment> {
  return request<TOTPEnrollment>(
    "POST",
    "/totp/enroll",
    { username, password, totpCode },
    { anonymous: true }
  )
}

/** Re-enrollment step two: the new seed proved by one of its own codes. */
export async function totpChange(
  username: string,
  password: string,
  totpCode: string,
  newTotpSecret: string,
  newTotpCode: string
): Promise<void> {
  await request<{ username: string }>(
    "POST",
    "/totp",
    { username, password, totpCode, newTotpSecret, newTotpCode },
    { anonymous: true }
  )
}

// ── tokens (which ARE sessions) ─────────────────────────────────────────────

/** The tokens page's list: metadata only, never the hash. */
export async function listTokens(): Promise<TokenInfo[]> {
  const res = await request<OperationalList<TokenInfo>>("GET", "/tokens")
  return res?.items ?? []
}

/** Mint a token for a script or a device. The secret comes back ONCE. */
export function mintToken(
  label: string,
  expiresAt?: string
): Promise<MintedToken> {
  const body: { label: string; expiresAt?: string } = { label }
  if (expiresAt) body.expiresAt = expiresAt
  return request<MintedToken>("POST", "/tokens", body)
}

/** Revoking IS deleting the token record — the endpoint, the generic record
 * delete and `substratectl` are all the same write. */
export async function revokeToken(id: string): Promise<void> {
  await request<void>("DELETE", `/tokens/${encodeURIComponent(id)}`)
}

/** Sign out: revoke the token record this browser holds, then drop the local
 * session. A revoke that fails (the token was already dead, or the network is
 * gone) still clears locally — the browser must never be left holding a secret
 * it believes is live. */
export async function logout(): Promise<void> {
  const id = getTokenId()
  if (id && getToken()) {
    try {
      await revokeToken(id)
    } catch {
      /* already gone / offline — clear locally regardless */
    }
  }
  clearSession()
}
