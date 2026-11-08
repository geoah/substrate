import type { AuthorizationDetail } from "./manifest"

/** A signed-in app on one server: the token the owner's approval minted and
 * what it was minted for. */
export interface Session {
  server: string
  repository: string
  token: string
  tokenID?: string
  /** The scopes the approval gave. */
  scopes: string[]
  /** The scopes the request asked for: what the approval left out of them
   * is the owner's choice, never a reason to ask again. */
  requestedScopes?: string[]
  /** Epoch ms; absent when the owner approved it until they revoke it. */
  expiresAt?: number
  clientID: string
  revocationEndpoint?: string
  details: AuthorizationDetail[]
  signedInAt: number
}

/** Whether the approved access has ended by `now`. */
export function hasEnded(session: Session, now = Date.now()): boolean {
  return session.expiresAt !== undefined && session.expiresAt <= now
}

/** The session kept in this browser's storage, one per app: a web app's
 * token lives where its pages can read it, which is what a public client
 * is. The kept session is the one every tab of the app runs on: a sign-out
 * or a sign-in in one tab reaches the others through `watch`. Its reads
 * write nothing; `save` and `clear` are `SessionKeeper`'s, under the lock
 * every tab's change of it takes. */
export class SessionStore {
  readonly key: string

  constructor(appID: string) {
    this.key = `substrate.session.${appID}`
  }

  /** The session kept, ended or not; null when none or unreadable. */
  read(): Session | null {
    try {
      const raw = localStorage.getItem(this.key)
      return raw ? (JSON.parse(raw) as Session) : null
    } catch {
      return null
    }
  }

  /** The session kept, while its access has not ended. */
  load(): Session | null {
    const s = this.read()
    return s && !hasEnded(s) ? s : null
  }

  /** Throws where the storage will not take it (full, or private). */
  save(session: Session): void {
    localStorage.setItem(this.key, JSON.stringify(session))
  }

  clear(): void {
    localStorage.removeItem(this.key)
  }

  /** Whether `token` is the session every tab runs on now. */
  current(token: string): boolean {
    return this.load()?.token === token
  }

  /** Calls `fn` with the kept session whenever another tab changes it. A
   * tab's own writes raise no storage event, so it learns only of others'. */
  watch(fn: (session: Session | null) => void): () => void {
    const onStorage = (e: StorageEvent) => {
      if (e.key === this.key || e.key === null) fn(this.load())
    }
    window.addEventListener("storage", onStorage)
    return () => window.removeEventListener("storage", onStorage)
  }
}

/** Revokes the session's token (RFC 7009). Only a 2xx confirms it: the RFC's
 * server answers 200 for a token it revoked and for one already invalid, and
 * internal/api's handler answers 200 to every well-formed request and 503
 * when it could not revoke. A 400 or a 401 refuses the request itself
 * (invalid_request, unsupported_token_type, invalid_client) and revokes
 * nothing, so the token is still live and is kept to try again and to name
 * to the owner. */
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
  if (!res.ok) {
    throw new Error(
      `The server didn't confirm revoking this sign-in (${res.status}).`
    )
  }
}
