/** The substrate's door: registration behind an invite code, login, the
 * password-factor credential changes, and the token records that ARE sessions.
 * A sign-in names a person, who picks a repository (decision 0274).
 *
 * These endpoints carry NO `/api` prefix — they sit at the root
 * (`/register`, `/login`, `/password`, `/totp`, `/tokens`). The password-factor
 * ones (`/password`, `/totp`, and their enroll steps) are deliberately
 * ANONYMOUS: the current password and TOTP code travel in the body, and a
 * bearer on them is refused outright (RB-6). Login and registration end LOGGED
 * IN — the response carries a freshly minted token secret, shown once. */

import { queryOptions } from "@tanstack/react-query"

import { doorNamesAPerson } from "./discovery"
import { request } from "./http"
import { appHoldsSession, clearSession, getToken, getTokenId } from "./session"
import type {
  IdentityInfo,
  MintedToken,
  OperationalList,
  RepositoryNames,
  SessionCredential,
  TOTPEnrollment,
  TokenInfo,
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

// ── the person ───────────────────────────────────────────────────────────

/** The fields a door request names its person by: `identity`, or on a server
 * from before identities, the sign-in name as `repository`
 * (doorNamesAPerson). */
function person(
  identity: string
): { identity: string } | { repository: string } {
  return doorNamesAPerson() ? { identity } : { repository: identity }
}

/** The person a token acts for: their name, whether their sign-in asks for a
 * code, and the repositories they may open. */
export function getIdentity(): Promise<IdentityInfo> {
  return request<IdentityInfo>("GET", "/identity")
}

export const identityQueryOptions = queryOptions({
  queryKey: ["identity"],
  queryFn: getIdentity,
})

// ── registration ────────────────────────────────────────────────────────────

/** Step one of registration: the invite code and the name the person will
 * sign in with buy a TOTP seed. It writes NOTHING — the caller holds the seed
 * and hands it back with one code, so an abandoned registration leaves no
 * row. */
export function registerEnroll(
  inviteCode: string,
  identity: string
): Promise<TOTPEnrollment> {
  return request<TOTPEnrollment>(
    "POST",
    "/register/enroll",
    { inviteCode, ...person(identity) },
    { anonymous: true }
  )
}

export interface RegisterInput {
  inviteCode: string
  /** The name the person will sign in with: `ada.example.com`, or a bare
   * label the substrate completes under its public host
   * (`registration.nameHost`). */
  identity: string
  /** The repository to create, and its name: absent takes the person's.
   * It can be renamed later (`renameRepository`, decision 0287). */
  repository?: string
  password: string
  totpSecret: string
  totpCode: string
  /** Registers without a second factor, which the owner may turn on later in
   * Settings; `totpSecret` and `totpCode` are then empty. */
  skipTotp?: boolean
  label?: string
}

/** What registration hands back: the mint of the first token, with the
 * repository it created. */
export type RegisterResult = MintedToken

/** The recovery key a server below `auth` revision 2 minted for a
 * registration that named no recipient, which is every registration this
 * console sends. Such a server still wraps the repository's key to it and
 * returns it on this one answer, so the answer is the only copy (decision
 * 0264). A current server's answer carries none: no wire type names it. */
export function olderServersRecoveryKey(
  result: RegisterResult
): string | undefined {
  const key: unknown = (result as { recoveryKey?: unknown }).recoveryKey
  return typeof key === "string" && key !== "" ? key : undefined
}

/** Step two: the same code proves the seed, and one creation act makes the
 * person, their repository and the first token. Registration ends LOGGED IN
 * — the response carries the token secret. */
export function register(input: RegisterInput): Promise<RegisterResult> {
  const { identity, repository, ...rest } = input
  const body = doorNamesAPerson()
    ? input
    : { ...rest, repository: repository ?? identity }
  return request<RegisterResult>("POST", "/register", body, {
    anonymous: true,
  })
}

// ── login ─────────────────────────────────────────────────────────────────

/** The login request as the door decodes it (`substrate.LoginRequest`): the
 * person, the repository they open where they may open several, the two
 * factors and the label of the token the login mints. */
export interface LoginRequest {
  identity: string
  repository?: string
  password: string
  totpCode: string
  label?: string
}

/** Login mints a token RECORD and returns its secret once: there is no session
 * concept beside it, the console holds a token like every other client. A
 * person who may open several repositories and names none is refused with
 * `repository_required` and the list, and their code is not spent: the same
 * call naming one signs in. */
export function login(
  identity: string,
  password: string,
  totpCode: string,
  label = "console",
  repository?: string
): Promise<MintedToken> {
  const body: LoginRequest | Record<string, string> = !doorNamesAPerson()
    ? { repository: identity, password, totpCode, label }
    : repository
      ? { identity, repository, password, totpCode, label }
      : { identity, password, totpCode, label }
  return request<MintedToken>("POST", "/login", body, { anonymous: true })
}

// ── password-factor credential changes ──────────────────────────────────────

/** The password-factor rule (RB-6): the CURRENT password and code travel in
 * the body. A bearer token alone is refused with 403 — so these calls are
 * deliberately anonymous. */
export function changePassword(
  identity: string,
  password: string,
  totpCode: string,
  newPassword: string,
  signOutEverywhere = false
): Promise<SessionCredential> {
  return request<SessionCredential>(
    "POST",
    "/password",
    signOutEverywhere
      ? {
          ...person(identity),
          password,
          totpCode,
          newPassword,
          signOutEverywhere,
        }
      : { ...person(identity), password, totpCode, newPassword },
    { anonymous: true }
  )
}

/** Ends every token the person holds: every browser and script signed in,
 * every token minted from one, and every app's and assistant's access, this
 * browser's included (decision 0272), in every repository they open. Both
 * current factors travel in the body, like the credential changes: it is how
 * an account is taken back from a leaked token, so a token is no evidence
 * for it. */
export function signOutEverywhere(
  identity: string,
  password: string,
  totpCode: string
): Promise<SessionCredential> {
  return request<SessionCredential>(
    "POST",
    "/signout/everywhere",
    { ...person(identity), password, totpCode },
    { anonymous: true }
  )
}

/** Re-enrollment step one: prove both current factors, receive a fresh seed. */
export function totpEnroll(
  identity: string,
  password: string,
  totpCode: string
): Promise<TOTPEnrollment> {
  return request<TOTPEnrollment>(
    "POST",
    "/totp/enroll",
    { ...person(identity), password, totpCode },
    { anonymous: true }
  )
}

/** Re-enrollment step two: the new seed proved by one of its own codes. */
export async function totpChange(
  identity: string,
  password: string,
  totpCode: string,
  newTotpSecret: string,
  newTotpCode: string
): Promise<void> {
  await request<SessionCredential>(
    "POST",
    "/totp",
    { ...person(identity), password, totpCode, newTotpSecret, newTotpCode },
    { anonymous: true }
  )
}

/** Delete a repository for good: every record, token and file, and the name
 * is free to register again. The person's factors travel in the body, like
 * the credential changes, so a leaked token cannot do it; a person whose last
 * repository it was goes with it. */
export async function deleteRepository(
  identity: string,
  repository: string,
  password: string,
  totpCode: string
): Promise<void> {
  await request<void>(
    "POST",
    "/repository/delete",
    doorNamesAPerson()
      ? { identity, repository, password, totpCode }
      : { repository, password, totpCode },
    { anonymous: true }
  )
}

/** Give the repository a new name (decision 0287). The person and their
 * current factors travel in the body, like the delete, and only an admin of
 * the repository renames it. The old name keeps resolving for 90 days, and
 * the answer says until when. */
export function renameRepository(
  identity: string,
  repository: string,
  password: string,
  totpCode: string,
  name: string
): Promise<RepositoryNames> {
  return request<RepositoryNames>(
    "POST",
    "/repository/rename",
    { identity, repository, password, totpCode, name },
    { anonymous: true }
  )
}

// ── tokens (which ARE sessions) ─────────────────────────────────────────────

/** The tokens page's list: metadata only, never the hash. */
export async function listTokens(): Promise<TokenInfo[]> {
  const res = await request<OperationalList<TokenInfo>>("GET", "/tokens")
  return res?.items ?? []
}

/** Every token record, one cache entry for every list in Settings, so a
 * revoke or a mint on one page refreshes the others. */
export const tokensQueryOptions = queryOptions({
  queryKey: ["tokens"],
  queryFn: listTokens,
})

/** Mint a token for a script or a device. The secret comes back ONCE. */
export function mintToken(
  label: string,
  expiresAt?: string
): Promise<MintedToken> {
  const body: { label: string; expiresAt?: string } = { label }
  if (expiresAt) body.expiresAt = expiresAt
  return request<MintedToken>("POST", "/tokens", body)
}

/** Mint an AI assistant's token (decision 0230), for a client that takes a
 * pasted one: it works at the MCP address alone, and `label` is the name it
 * goes by. The secret comes back ONCE. */
export function mintAssistantToken(label: string): Promise<MintedToken> {
  return request<MintedToken>("POST", "/tokens", { label, scopes: ["mcp"] })
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
  // In the Apple app the app holds the token, and revokes it when the page
  // says it ended, trying again while the server does not answer.
  if (id && getToken() && !appHoldsSession()) {
    try {
      await revokeToken(id)
    } catch {
      /* already gone / offline — clear locally regardless */
    }
  }
  clearSession()
}
