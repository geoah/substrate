import { sessionLocks, type SessionLocks } from "./locks"
import { RevocationQueue } from "./revocations"
import { SessionStore, hasEnded, revoke, type Session } from "./session"

/** Where a token no session keeps went: queued to revoke, revoked, or still
 * live, which only the owner can revoke now, in the server's Settings. */
export type Discarded = "queued" | "revoked" | "live"

/** A token revoked now, without the queue: what one the queue would not
 * take is left with. */
export function revokeNow(session: Session): Promise<Discarded> {
  return revoke(session).then(
    () => "revoked" as const,
    () => "live" as const
  )
}

const freeStorage =
  "Free some of this browser's storage, or let this site keep data"

/** A sign-out that did not happen: this browser could not keep the token to
 * revoke, so the session stays where it was. The message is words for a
 * screen. */
export class NotSignedOut extends Error {
  constructor() {
    super(
      `You're still signed in: this browser couldn't keep the access to revoke it, so nothing changed. ${freeStorage}, then try again.`
    )
    this.name = "NotSignedOut"
  }
}

/** A sign-in that was not kept: this browser could not keep its session, or
 * the token of the one it would replace, so the session before stays.
 * `access` says where the new token went. The message is words for a
 * screen. */
export class NotSignedIn extends Error {
  readonly access: Discarded

  constructor(session: Session, access: Discarded) {
    const host = new URL(session.server).host
    super(
      access === "live"
        ? `This browser couldn't keep the sign-in, and ${host} didn't confirm revoking the access it gave: revoke it yourself in ${host}'s Settings, under Apps. ${freeStorage}, then sign in again.`
        : `This browser couldn't keep the sign-in, so nothing changed and the access ${host} gave ${access === "revoked" ? "was revoked" : "is being revoked"}. ${freeStorage}, then sign in again.`
    )
    this.name = "NotSignedIn"
    this.access = access
  }
}

/** How a sign-out ended. */
export type SignOut =
  /** No session is kept: every tab is signed out. */
  | { type: "signedOut" }
  /** Another tab signed in first, with `session`, which stands: only the
   * token signed out of is revoked. */
  | { type: "replaced"; session: Session }

export interface SessionKeeperOptions {
  /** The lock every change takes: the browser's Web Locks, else this tab's
   * own. */
  locks?: SessionLocks
  /** The waits before revocations the server did not confirm are sent
   * again, in ms; the last repeats. */
  retryDelays?: readonly number[]
  /** How long one revocation may take before it counts as not confirmed,
   * in ms. */
  revokeTimeout?: number
}

const defaultRetryDelays = [5_000, 30_000, 120_000, 600_000]

/** The session every tab of an app runs on, and the tokens it stopped
 * using, changed only here. Each change (a sign-in kept, a sign-out, an
 * ended access taken up, a revocation confirmed) runs under the one Web
 * Lock all the app's tabs share, `substrate.session.<app id>`: it reads the
 * kept session afresh, decides and writes, and awaits nothing while it
 * holds the lock, so no other tab's change lands between its read and its
 * write. Where the browser has no Web Locks, the lock holds within the tab
 * alone (`tabLocks`).
 *
 * One rule keeps every token: a token leaves the kept session only into
 * the revocation queue, written before the session changes, and leaves the
 * queue only once its server confirms revoking it. A change whose token the
 * queue will not take (a full or private storage) does not happen, and says
 * so. Revocations are sent from the queue after the lock is let go, in the
 * background, and sent again while they fail: no change waits on one. */
export class SessionKeeper {
  private readonly store: SessionStore
  private readonly queue: RevocationQueue
  private readonly locks?: SessionLocks
  private readonly retryDelays: readonly number[]
  private readonly revokeTimeout?: number
  /** Each token's revocation on the wire, by token. */
  private readonly inflight = new Map<string, Promise<void>>()
  /** The tokens whose last revocation the server did not confirm. */
  private readonly failed = new Set<string>()
  private failures = 0
  private timer?: ReturnType<typeof setTimeout>
  private readonly listeners = new Set<(unrevoked: Session[]) => void>()

  constructor(appID: string, opts: SessionKeeperOptions = {}) {
    this.store = new SessionStore(appID)
    this.queue = new RevocationQueue(appID)
    this.locks = opts.locks
    this.retryDelays = opts.retryDelays ?? defaultRetryDelays
    this.revokeTimeout = opts.revokeTimeout
  }

  /** Calls `fn` when another tab changes the kept session. */
  watchSession(fn: () => void): () => void {
    return this.store.watch(fn)
  }

  /** Calls `fn` when another tab changes the tokens queued to revoke. */
  watchQueue(fn: () => void): () => void {
    return this.queue.watch(fn)
  }

  /** The kept session, while its access has not ended. */
  load(): Session | null {
    return this.store.load()
  }

  /** Whether `token` is the session every tab runs on now. */
  current(token: string): boolean {
    return this.store.current(token)
  }

  /** The tokens queued to revoke. */
  pending(): Session[] {
    return this.queue.pending()
  }

  /** Runs `change` under the lock. It is synchronous, so it holds the lock
   * only while it reads and writes the storage. */
  private locked<T>(change: () => T): Promise<T> {
    return (this.locks ?? sessionLocks()).request(this.store.key, () =>
      Promise.resolve().then(change)
    )
  }

  /** Keeps `next`, the session a sign-in's return minted, as the one every
   * tab runs on. The session it replaces is the one kept now, which another
   * tab may have signed in since, and its token is queued before `next`
   * takes its place. Rejects with `NotSignedIn`, the kept session as it
   * was, when the queue will not take that token or the storage will not
   * take `next`; `next`'s token is then queued, or revoked at once where
   * the queue will not take it either. */
  async signIn(next: Session): Promise<void> {
    const kept = await this.locked(() => {
      // Raw, not `load`: a session whose time looks up by this browser's
      // clock may still be live on the server, so it is queued like any.
      const before = this.store.read()
      const replaced = before && before.token !== next.token ? before : null
      if (replaced && !this.queue.keep(replaced))
        return this.queue.keep(next) ? "queued" : "refused"
      try {
        this.store.save(next)
      } catch {
        // The session before stands, so its token leaves the queue.
        if (replaced) this.queue.drop(replaced)
        return this.queue.keep(next) ? "queued" : "refused"
      }
      return "saved"
    })
    void this.send()
    if (kept === "saved") return
    throw new NotSignedIn(
      next,
      kept === "queued" ? "queued" : await revokeNow(next)
    )
  }

  /** Signs out of `session`, this tab's: its token is queued, then the kept
   * session is cleared while it is still that one. A tab that slept through
   * another tab's sign-in leaves the session that stands, which answers for
   * the tab to take up. Rejects with `NotSignedOut`, nothing changed, when
   * the queue will not take the token. */
  async signOut(session: Session): Promise<SignOut> {
    const out = await this.locked((): SignOut | null => {
      // Queued even when its time looks up: only the server's answer
      // confirms a token is gone, and this browser's clock may run ahead.
      if (!this.queue.keep(session)) return null
      const kept = this.store.read()
      if (kept?.token !== session.token)
        return kept && !hasEnded(kept)
          ? { type: "replaced", session: kept }
          : { type: "signedOut" }
      try {
        this.store.clear()
      } catch {
        // Still kept, so still in use: not to be revoked.
        this.queue.drop(session)
        return null
      }
      return { type: "signedOut" }
    })
    void this.send()
    if (!out) throw new NotSignedOut()
    return out
  }

  /** Takes up the end of `session`'s access here, and answers the session
   * kept after: one another tab signed in since, or none. `cause` says who
   * ended it:
   * - `"refused"`: the server answered 401 to a request that carried its
   *   token (a tab's requests carry only the kept session's). The server's
   *   word is the confirmation, so nothing is queued.
   * - `"timeUp"`: this browser's clock, or a check that may rest on it,
   *   says its time is up. A clock can run ahead of the server's, so the
   *   token is queued like any other before the session is cleared; where
   *   the queue will not take it, the session stays kept (no tab runs on
   *   it, since `load` passes it over) and the next sign-in queues it. */
  async end(
    session: Session,
    cause: "refused" | "timeUp" = "refused"
  ): Promise<Session | null> {
    const after = await this.locked(() => {
      const kept = this.store.read()
      if (kept?.token !== session.token)
        return kept && !hasEnded(kept) ? kept : null
      if (cause === "timeUp" && !this.queue.keep(session)) return null
      try {
        this.store.clear()
      } catch {
        // Kept, and still the token's one copy if the queue holds none;
        // the next sign-in replaces it and queues it.
        if (cause === "timeUp") this.queue.drop(session)
      }
      return null
    })
    if (cause === "timeUp") void this.send()
    return after
  }

  /** Lets go of a token a sign-in minted and will not keep (one for another
   * repository than the one typed): queued to revoke, or revoked at once
   * where the queue will not take it. Answers where it went. */
  async discard(session: Session): Promise<Discarded> {
    const held = await this.locked(() => this.queue.keep(session))
    if (!held) return revokeNow(session)
    void this.send()
    return "queued"
  }

  /** Calls `fn` with the tokens still unrevoked after each pass over the
   * queue. While anyone listens, a pass that leaves some is followed by
   * another, after a wait that grows with each that fails. */
  onUnrevoked(fn: (unrevoked: Session[]) => void): () => void {
    this.listeners.add(fn)
    return () => {
      this.listeners.delete(fn)
      if (this.listeners.size) return
      clearTimeout(this.timer)
      this.timer = undefined
    }
  }

  /** Sends every queued token's revocation, each on its own, so one the
   * server does not answer holds back no other; one already on the wire is
   * not sent twice. Forgets each the server confirms, and answers the
   * tokens left. */
  async send(): Promise<Session[]> {
    clearTimeout(this.timer)
    this.timer = undefined
    let due: Session[]
    try {
      due = await this.locked(() => {
        // One the kept session still holds (a sign-in whose save failed
        // and whose replaced token could not leave the queue) is in use:
        // it is forgotten here. Every other is sent, a token whose time
        // looks up included: only the server's answer confirms it is gone.
        const kept = this.store.read()?.token
        const out: Session[] = []
        for (const s of this.queue.pending()) {
          if (s.token === kept) this.queue.drop(s)
          else out.push(s)
        }
        return out
      })
    } catch {
      due = this.queue.pending()
    }
    await Promise.all(due.map((s) => this.sendOne(s)))
    return this.report()
  }

  /** Tells the listeners the tokens still unrevoked, and schedules the next
   * pass while any are. Called as each token's revocation settles, so one
   * still on the wire holds back no other's retry. */
  private report(): Session[] {
    const left = this.queue.pending().filter((s) => this.failed.has(s.token))
    for (const fn of this.listeners) fn(left)
    if (!left.length) {
      this.failures = 0
    } else if (this.listeners.size && !this.timer) {
      this.failures++
      const at = Math.min(this.failures, this.retryDelays.length) - 1
      this.timer = setTimeout(() => void this.send(), this.retryDelays[at])
    }
    return left
  }

  /** One token's revocation, sent with the lock let go and forgotten under
   * it once the server confirms. */
  private sendOne(session: Session): Promise<void> {
    let sending = this.inflight.get(session.token)
    if (sending) return sending
    sending = revoke(session, this.revokeTimeout)
      .then(
        async () => {
          this.failed.delete(session.token)
          await this.locked(() => this.queue.drop(session))
        },
        () => {
          this.failed.add(session.token)
        }
      )
      .catch(() => {})
      .finally(() => {
        this.inflight.delete(session.token)
        this.report()
      })
    this.inflight.set(session.token, sending)
    return sending
  }
}
