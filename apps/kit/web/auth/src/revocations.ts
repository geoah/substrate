import type { Session } from "./session"

/** The tokens this browser stopped using that their servers have not
 * confirmed revoking yet, kept beside the session (the kit's pending
 * revocations). A token leaves the session only into this queue, and
 * leaves the queue only once its server confirms revoking it, so a live
 * token is always held by one of the two. Its writes are
 * `SessionKeeper`'s, under the lock every tab's change of the session
 * takes, which also sends what it keeps. */
export class RevocationQueue {
  readonly key: string

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
      // a private or a full storage keeps nothing: the caller hears it
      return false
    }
  }

  /** Keeps the token to revoke. Answers whether it is kept: already, or
   * now; where it is not, whatever held it must go on holding it. */
  keep(session: Session): boolean {
    const list = this.pending()
    if (list.some((s) => s.token === session.token)) return true
    return this.write([...list, session])
  }

  /** Forgets a token: one its server confirmed revoking, one whose time is
   * up, or one the kept session still holds. Answers whether it is gone. */
  drop(session: Session): boolean {
    const list = this.pending()
    const left = list.filter((s) => s.token !== session.token)
    return left.length === list.length || this.write(left)
  }

  /** Calls `fn` whenever another tab changes the tokens kept. */
  watch(fn: () => void): () => void {
    const onStorage = (e: StorageEvent) => {
      if (e.key === this.key || e.key === null) fn()
    }
    window.addEventListener("storage", onStorage)
    return () => window.removeEventListener("storage", onStorage)
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
    return `${appName}'s earlier access${repository} still works on ${host}. It keeps trying to revoke it; you can also revoke it yourself in ${host}'s Settings, under Apps.`
  })
}

/** The title every screen says the unrevoked tokens under. */
export const unrevokedTitle = "Server access has not been revoked yet"
