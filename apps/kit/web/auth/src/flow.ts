import type { AuthorizationServerMetadata } from "@substrate/client"
import type { AppManifest, AuthorizationDetail } from "./manifest"
import { makePKCE, randomToken } from "./pkce"
import { revoke, type Session } from "./session"

/** What the page keeps across the navigation to the server's approval page
 * and back: in sessionStorage, so it never outlives the tab. */
interface Pending {
  state: string
  verifier: string
  server: string
  repository?: string
  metadata: AuthorizationServerMetadata
  /** The scopes asked for, which the session keeps beside those given. */
  scopes?: string[]
  startedAt: number
}

const pendingKey = (app: string) => `substrate.signin.${app}`

export class SignInError extends Error {
  readonly code: string
  constructor(code: string, message: string) {
    super(message)
    this.name = "SignInError"
    this.code = code
  }
}

/** Opens the server's own sign-in and approval page in this tab: OAuth 2.1's
 * code flow with PKCE for a public client whose `client_id` is a URL, plus
 * the details the approval sets up (decision 0194). The app never sees a
 * password. */
export async function beginSignIn(opts: {
  manifest: AppManifest
  server: string
  metadata: AuthorizationServerMetadata
  repository?: string
  /** Where the page goes: the approval page, in this tab. */
  navigate?: (url: string) => void
}): Promise<void> {
  const { manifest, metadata } = opts
  const types = metadata.authorization_details_types_supported ?? []
  if (manifest.details.some((d) => !types.includes(d.type))) {
    throw new SignInError(
      "server_too_old",
      `${new URL(opts.server).host} can't set up what ${manifest.name} asks for: it needs an update first.`
    )
  }
  const pkce = await makePKCE()
  const state = randomToken(24)
  const pending: Pending = {
    state,
    verifier: pkce.verifier,
    server: opts.server,
    repository: opts.repository,
    metadata,
    scopes: manifest.scopes,
    startedAt: Date.now(),
  }
  sessionStorage.setItem(pendingKey(manifest.id), JSON.stringify(pending))
  const u = new URL(metadata.authorization_endpoint)
  const params: [string, string][] = [
    ["response_type", "code"],
    ["client_id", manifest.clientID],
    ["redirect_uri", manifest.redirectURI],
    ["scope", manifest.scopes.join(" ")],
    ["code_challenge", pkce.challenge],
    ["code_challenge_method", "S256"],
    ["state", state],
  ]
  if (opts.repository) params.push(["login_hint", opts.repository])
  params.push(["software_version", manifest.softwareVersion])
  if (manifest.details.length)
    params.push(["authorization_details", JSON.stringify(manifest.details)])
  for (const [k, v] of params) u.searchParams.set(k, v)
  const navigate =
    opts.navigate ?? ((url: string) => window.location.assign(url))
  navigate(u.toString())
}

/** Whether this page load is the server's redirect back. */
export function isCallback(
  manifest: AppManifest,
  location: Location = window.location
): boolean {
  const q = new URLSearchParams(location.search)
  return (
    (q.has("code") || q.has("error")) &&
    q.has("state") &&
    !!sessionStorage.getItem(pendingKey(manifest.id))
  )
}

interface TokenResponse {
  access_token: string
  token_type?: string
  scope?: string
  expires_in?: number
  repository: string
  token_id?: string
  authorization_details?: AuthorizationDetail[]
}

/** The redirect's query checked (the state this flow sent, the issuer it
 * began on, RFC 9207) and the code exchanged for a session. The query is
 * cleared from the address bar either way. */
export async function completeSignIn(
  manifest: AppManifest,
  location: Location = window.location
): Promise<Session> {
  const q = new URLSearchParams(location.search)
  const raw = sessionStorage.getItem(pendingKey(manifest.id))
  sessionStorage.removeItem(pendingKey(manifest.id))
  window.history.replaceState(null, "", location.pathname + location.hash)
  if (!raw)
    throw new SignInError(
      "invalid_state",
      "The sign-in came back for another request. Try again."
    )
  const pending = JSON.parse(raw) as Pending
  if (q.get("state") !== pending.state) {
    throw new SignInError(
      "invalid_state",
      "The sign-in came back for another request. Try again."
    )
  }
  const iss = q.get("iss")
  if (iss !== null) {
    if (iss !== pending.metadata.issuer) {
      throw new SignInError(
        "invalid_issuer",
        `The sign-in came back from ${iss}, not ${pending.metadata.issuer}.`
      )
    }
  } else if (pending.metadata.authorization_response_iss_parameter_supported) {
    throw new SignInError(
      "invalid_issuer",
      "The sign-in came back without naming its server. Try again."
    )
  }
  const error = q.get("error")
  if (error) {
    if (error === "access_denied")
      throw new SignInError(error, "Access was not granted.")
    throw new SignInError(error, q.get("error_description") ?? error)
  }
  const code = q.get("code")
  if (!code)
    throw new SignInError(
      "invalid_request",
      "The sign-in came back without a code."
    )

  const res = await fetch(pending.metadata.token_endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
    },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      client_id: manifest.clientID,
      redirect_uri: manifest.redirectURI,
      code_verifier: pending.verifier,
    }),
    redirect: "error",
  })
  const body = (await res
    .json()
    .catch(() => ({}))) as Partial<TokenResponse> & {
    error?: string | { message?: string }
    error_description?: string
  }
  if (!res.ok || !body.access_token || !body.repository) {
    const words =
      body.error_description ??
      (typeof body.error === "object" ? body.error?.message : body.error) ??
      `The server answered ${res.status}.`
    throw new SignInError(
      typeof body.error === "string" ? body.error : "token_refused",
      words
    )
  }
  const session: Session = {
    server: pending.server,
    repository: body.repository,
    token: body.access_token,
    tokenID: body.token_id || undefined,
    scopes: (body.scope ?? "").split(" ").filter(Boolean),
    requestedScopes: pending.scopes,
    expiresAt: body.expires_in
      ? Date.now() + body.expires_in * 1000
      : undefined,
    clientID: manifest.clientID,
    revocationEndpoint: pending.metadata.revocation_endpoint,
    details: body.authorization_details ?? [],
    signedInAt: Date.now(),
  }
  // A token for another repository than the one typed: the person signed in
  // to a different repository on that server. Never kept.
  if (
    pending.repository &&
    normal(pending.repository) !== normal(session.repository)
  ) {
    await revoke(session).catch(() => {})
    throw new SignInError(
      "repository_mismatch",
      `You signed in to ${session.repository}, not ${pending.repository}. Sign in to ${pending.repository}, or type ${session.repository} to connect it instead.`
    )
  }
  return session
}

function normal(name: string): string {
  return name.toLowerCase().replace(/\.$/, "")
}
