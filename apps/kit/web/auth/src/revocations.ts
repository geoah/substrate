import { revoke, type Session } from "./session"

/** The tokens this browser stopped using that their servers have not
 * confirmed revoking yet, kept beside the session so a sign-out that met no
 * server is tried again at the next open (the kit's pending revocations).
 * A token is kept before it is revoked and forgotten once it is, so a
 * failed revocation never leaves a live token with nothing holding it. */
export class RevocationQueue {
  private readonly key: string

  constructor(appID: string) {
    this.key = `substrate.unrevoked.${appID}`
  }

  pending(): Session[] {
    try {
      const raw = localStorage.getItem(this.key)
      const list = raw ? (JSON.parse(raw) as Session[]) : []
      return Array.isArray(list) ? list : []
    } catch {
      return []
    }
  }

  private write(list: Session[]): boolean {
    try {
      if (list.length) localStorage.setItem(this.key, JSON.stringify(list))
      else localStorage.removeItem(this.key)
      return true
    } catch {
      // a private window keeps nothing: the revocation is tried once
      return false
    }
  }

  /** Keeps the token to revoke, before anything else forgets it: a
   * sign-out calls this before it clears the session, so the token is never
   * held by nothing. Answers whether it was kept. */
  keep(session: Session): boolean {
    const list = this.pending().filter((s) => s.token !== session.token)
    return this.write([...list, session])
  }

  private forget(session: Session): void {
    this.write(this.pending().filter((s) => s.token !== session.token))
  }

  /** Revokes a token already kept, and forgets it once the server confirms.
   * Answers whether it did; one it did not stays kept for `retry`. */
  async send(session: Session): Promise<boolean> {
    try {
      await revoke(session)
      this.forget(session)
      return true
    } catch {
      return false
    }
  }

  /** Keeps the token, then revokes it. */
  async revoke(session: Session): Promise<boolean> {
    this.keep(session)
    return this.send(session)
  }

  /** Calls `fn` whenever another tab changes the tokens kept. */
  watch(fn: () => void): () => void {
    const onStorage = (e: StorageEvent) => {
      if (e.key === this.key || e.key === null) fn()
    }
    window.addEventListener("storage", onStorage)
    return () => window.removeEventListener("storage", onStorage)
  }

  /** Tries every kept token again and answers the ones still live. A
   * token that has ended needs no revoking and is forgotten. */
  async retry(now = Date.now()): Promise<Session[]> {
    const left: Session[] = []
    for (const session of this.pending()) {
      if (session.expiresAt !== undefined && session.expiresAt <= now) {
        this.forget(session)
        continue
      }
      try {
        await revoke(session)
        this.forget(session)
      } catch {
        left.push(session)
      }
    }
    return left
  }
}

/** One sentence per server and repository the kept tokens reach, in the
 * order they were met: where the access still works, that the app tries
 * again, and where the owner revokes it by hand. */
export function unrevokedSentences(
  appName: string,
  unrevoked: readonly Session[]
): string[] {
  const groups = new Map<string, Session>()
  for (const s of unrevoked) {
    const identity = `${s.server}|${s.repository.toLowerCase()}`
    if (!groups.has(identity)) groups.set(identity, s)
  }
  return [...groups.values()].map((s) => {
    const host = new URL(s.server).host
    const repository =
      s.repository.toLowerCase() === host.toLowerCase()
        ? ""
        : ` to ${s.repository}`
    return `${appName}'s earlier access${repository} still works on ${host}. It tries again each time it opens; you can also revoke it yourself in ${host}'s Settings, under Apps.`
  })
}

/** The title every screen says the unrevoked tokens under. */
export const unrevokedTitle = "Server access has not been revoked yet"
