import type { AuthorizationDetail } from "./manifest"

/** A signed-in app on one server: the token the owner's approval minted and
 * what it was minted for. */
export interface Session {
  server: string
  repository: string
  token: string
  tokenID?: string
  scopes: string[]
  /** Epoch ms; absent when the owner approved it until they revoke it. */
  expiresAt?: number
  clientID: string
  revocationEndpoint?: string
  details: AuthorizationDetail[]
  signedInAt: number
}

/** Sessions kept in this browser's storage, one per app: a web app's token
 * lives where its pages can read it, which is what a public client is. */
export class SessionStore {
  private readonly key: string

  constructor(appID: string) {
    this.key = `substrate.session.${appID}`
  }

  load(): Session | null {
    try {
      const raw = localStorage.getItem(this.key)
      if (!raw) return null
      const s = JSON.parse(raw) as Session
      if (s.expiresAt && s.expiresAt <= Date.now()) {
        localStorage.removeItem(this.key)
        return null
      }
      return s
    } catch {
      return null
    }
  }

  save(session: Session): void {
    localStorage.setItem(this.key, JSON.stringify(session))
  }

  clear(): void {
    localStorage.removeItem(this.key)
  }
}

/** Revokes the session's token (RFC 7009). A token already gone is done. */
export async function revoke(session: Session): Promise<void> {
  const endpoint =
    session.revocationEndpoint ?? `${session.server}/oauth/revoke`
  const body = new URLSearchParams({
    token: session.token,
    client_id: session.clientID,
  })
  const res = await fetch(endpoint, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
    },
    body,
    redirect: "error",
  })
  if (!res.ok && res.status !== 400 && res.status !== 401) {
    throw new Error(
      `The server didn't confirm revoking this sign-in (${res.status}).`
    )
  }
}
