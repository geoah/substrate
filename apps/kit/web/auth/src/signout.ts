import type { RevocationQueue } from "./revocations"
import type { Session, SessionStore } from "./session"

/** A sign-out that did not happen: the token could be neither kept to
 * revoke later nor revoked now, so the session stays where it was, to try
 * again. The message is words for a screen. */
export class NotSignedOut extends Error {
  constructor() {
    super(
      "You're still signed in: this browser could not keep the access to revoke it later, and the server didn't confirm revoking it now. Try again once the server can be reached."
    )
    this.name = "NotSignedOut"
  }
}

/** How a sign-out ended. `revoked` settles once the server answers: whether
 * it confirmed revoking the token, which otherwise waits in the queue. */
export type SignOut =
  /** No session is kept: every tab is signed out. */
  | { type: "signedOut"; revoked: Promise<boolean> }
  /** Another tab signed in first, with `session`, which stands: only the
   * token signed out of is revoked. */
  | { type: "replaced"; session: Session; revoked: Promise<boolean> }

/** Signs out of `session` without losing its token: it is kept in the queue
 * before the session is cleared, or, where the queue would not keep it,
 * revoked first, so a live token is always held by the session or by the
 * queue. The session is cleared only while it is still the kept one: a tab
 * that slept through another tab's sign-in revokes its own token and leaves
 * the other session standing. Rejects with `NotSignedOut`, nothing changed,
 * when the token could be neither kept nor revoked. */
export async function signOut(
  session: Session,
  store: SessionStore,
  queue: RevocationQueue
): Promise<SignOut> {
  let revoked: Promise<boolean> | undefined
  // Not kept, the session is the token's only copy until it is revoked.
  if (!queue.keep(session) && store.current(session.token)) {
    if (!(await queue.send(session))) throw new NotSignedOut()
    revoked = Promise.resolve(true)
  }
  const kept = store.clearIf(session.token)
  revoked ??= queue.send(session)
  return kept
    ? { type: "replaced", session: kept, revoked }
    : { type: "signedOut", revoked }
}
