import {
  AppStatusModel,
  RevocationQueue,
  SessionStore,
  beginSignIn,
  completeSignIn,
  discardStrayCallback,
  isCallback,
  requirementOf,
  signOut as signOutOf,
  type AppManifest,
  type AppStatusState,
  type Session,
} from "@substrate/auth"
import {
  SubstrateClient,
  describe,
  probe,
  type Kind,
  type LocatedServer,
} from "@substrate/client"
import { openRepository, type RepositoryLink } from "@substrate/local"
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react"
import {
  ConnectionContext,
  SubstrateContext,
  type Connection,
  type SubstrateContextValue,
} from "./context"
import { useToast } from "./toast"

const noSubscription = () => () => {}
const signedOutStatus: AppStatusState = { status: null, checking: false }
const noStatus = () => signedOutStatus

/** setTimeout fires at once for a wait past 2^31-1 ms (24.8 days), so a
 * longer one is waited in steps. */
const longestWait = 2_147_483_647

/** Runs one at a time: a call while one runs answers that one. */
function oneAtATime(): (run: () => Promise<void>) => Promise<void> {
  let running: Promise<void> | null = null
  return (run) =>
    (running ??= run().finally(() => {
      running = null
    }))
}

export interface SubstrateProviderProps {
  manifest: AppManifest
  /** The kinds the app reads and follows: declared kinds, or references. */
  kinds: readonly (string | Kind<unknown, never>)[]
  /** What the app calls its records in a sentence, plural: "tasks".
   * Absent, "records". */
  kept?: string
  /** How often, at most, the app's standing with its server is checked
   * again when the page comes back to the front, in ms. */
  statusInterval?: number
  children: ReactNode
}

/** A session's client, which sends its token only while it is still the
 * session every tab of the app runs on. A tab another tab signed out, or
 * one waking from the back-forward cache after the sign-out, sends nothing
 * the old token could do: the server answers 401, and the tab ends. */
class SessionClient extends SubstrateClient {
  private readonly store: SessionStore

  constructor(session: Session, store: SessionStore) {
    super(session.server, session.token)
    this.store = store
  }

  override headers(accept?: string): Headers {
    const h = super.headers(accept)
    if (this.token && !this.store.current(this.token)) h.delete("Authorization")
    return h
  }
}

/** The app's connection to the owner's substrate with no screens of its
 * own: the session, the sign-in's return, the app's standing, and the
 * repository opened once signed in. Its children read `useConnection()`
 * to draw their own connect screen, and the records once its `phase` is
 * `open`. `SubstrateApp` is this with the kit's screens; an app with its
 * own wraps this in a `ToastHost`, where writes that were not kept are
 * said. */
export function SubstrateProvider({
  manifest,
  kinds,
  kept = "records",
  statusInterval = 300_000,
  children,
}: SubstrateProviderProps) {
  const toast = useToast()
  const store = useMemo(() => new SessionStore(manifest.id), [manifest.id])
  const revocations = useMemo(
    () => new RevocationQueue(manifest.id),
    [manifest.id]
  )
  const kindKey = kinds
    .map((k) => (typeof k === "string" ? k : k.kind))
    .join(" ")
  const kindRefs = useMemo(() => kindKey.split(" ").filter(Boolean), [kindKey])
  const [session, setSession] = useState<Session | null>(() =>
    isCallback(manifest) ? null : store.load()
  )
  const [completing, setCompleting] = useState(() => isCallback(manifest))
  const [problem, setProblem] = useState<Connection["problem"]>(null)
  const [unrevoked, setUnrevoked] = useState<Session[]>(() =>
    revocations.pending()
  )
  const started = useRef(false)

  useEffect(() => {
    discardStrayCallback(manifest)
  }, [manifest])

  /** A session whose access ended here: forgotten unless another tab has
   * signed in since, whose session this tab takes up instead. */
  const endSession = useCallback(
    (ended: Session, words: string) => {
      const now = store.clearIf(ended.token)
      setSession(now)
      setProblem(
        now
          ? null
          : { title: `${manifest.name}'s access ended`, message: words }
      )
    },
    [store, manifest.name]
  )

  // Every tab runs on the one kept session: another tab's sign-out closes
  // this one's repository and drops its records, and its sign-in opens
  // here too. A page back from the cache, or to the front, looks again,
  // since it heard nothing while it was away.
  const shown = useRef(session)
  useEffect(() => {
    shown.current = session
  })
  useEffect(() => {
    const look = () => {
      setUnrevoked(revocations.pending())
      const now = store.load()
      if (shown.current?.token === now?.token) return
      shown.current = now
      setProblem(null)
      setSession(now)
    }
    const stopSession = store.watch(look)
    const stopRevocations = revocations.watch(() =>
      setUnrevoked(revocations.pending())
    )
    const visible = () => {
      if (document.visibilityState === "visible") look()
    }
    window.addEventListener("pageshow", look)
    document.addEventListener("visibilitychange", visible)
    return () => {
      stopSession()
      stopRevocations()
      window.removeEventListener("pageshow", look)
      document.removeEventListener("visibilitychange", visible)
    }
  }, [store, revocations])

  // The server's redirect back. The session it replaces is the one kept
  // when it answers, which another tab may have signed in meanwhile, and
  // its token is kept to revoke before the new session takes its place, or
  // revoked first where the queue will not keep it. A sign-in again that
  // fails leaves the kept session in place.
  useEffect(() => {
    if (!completing || started.current) return
    started.current = true
    completeSignIn(manifest)
      .then(async (s) => {
        const kept = store.load()
        const replaced = kept && kept.token !== s.token ? kept : null
        const held = !replaced || revocations.keep(replaced)
        const gone = !held && !!replaced && (await revocations.send(replaced))
        try {
          store.save(s)
        } catch (e) {
          // Not kept, the new token is held by nothing: it is revoked, and
          // the session before stays.
          if (replaced && held) revocations.drop(replaced)
          void revocations.send(s)
          throw e
        }
        setSession(s)
        setProblem(null)
        if (!replaced) return
        if (held) await revocations.send(replaced)
        else if (!gone) {
          const host = new URL(replaced.server).host
          toast(
            `Signed in, but ${manifest.name}'s earlier access still works on ${host}. Revoke it yourself in ${host}'s Settings, under Apps.`,
            "error"
          )
        }
        setUnrevoked(revocations.pending())
      })
      .catch((e: unknown) => {
        const kept = store.load()
        setSession(kept)
        if (kept) toast(describe(e), "error")
        else setProblem({ title: "Couldn't sign in", message: describe(e) })
      })
      .finally(() => setCompleting(false))
  }, [completing, manifest, store, revocations, toast])

  // Tokens a sign-out could not revoke, tried again at every open and
  // whenever the network comes back.
  useEffect(() => {
    const retry = () => void revocations.retry().then(setUnrevoked)
    retry()
    window.addEventListener("online", retry)
    return () => window.removeEventListener("online", retry)
  }, [revocations])

  const client = useMemo(
    () => (session ? new SessionClient(session, store) : null),
    [session, store]
  )

  const [link, setLink] = useState<RepositoryLink | null>(null)
  useEffect(() => {
    if (!session || !client) return
    let cancelled = false
    let opened: RepositoryLink | undefined
    void openRepository({
      app: manifest.id,
      repository: session.repository,
      client,
      kinds: kindRefs,
      kept,
    }).then((l) => {
      if (cancelled) return l.close()
      opened = l
      l.start()
      setLink(l)
    })
    // The records go with the link: a session ended in another tab leaves
    // nothing of it shown here.
    return () => {
      cancelled = true
      void opened?.close()
      setLink(null)
    }
  }, [session, client, kindRefs, kept, manifest.id])

  // A 401 from the feed, a read or a write ends the session; what waits in
  // the outbox stays kept for the next sign-in to this repository.
  useEffect(() => {
    if (!link || !session) return
    const host = new URL(session.server).host
    const check = () => {
      const signedOut =
        link.linked.statusSnapshot().status === "signedOut" ||
        link.outbox.getSnapshot().state.type === "signedOut"
      if (!signedOut) return
      const waiting = link.outbox.getSnapshot().waiting
      endSession(
        session,
        `Your access to ${host} ended. Sign in again` +
          (waiting
            ? ` to send the ${waiting === 1 ? "change" : `${waiting} changes`} waiting in this browser.`
            : ".")
      )
    }
    const a = link.linked.subscribe(check)
    const b = link.outbox.subscribe(check)
    return () => {
      a()
      b()
    }
  }, [link, session, endSession])

  // Access approved for a time ends at that time, page open or not.
  useEffect(() => {
    const until = session?.expiresAt
    if (!session || until === undefined) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const arm = () => {
      const wait = until - Date.now()
      if (wait <= 0)
        return endSession(
          session,
          "The time you approved it for is up. Sign in again."
        )
      timer = setTimeout(arm, Math.min(wait, longestWait))
    }
    arm()
    return () => clearTimeout(timer)
  }, [session, endSession])

  // The app's standing with its server: at sign-in, and again when the page
  // comes back to the front, at most once per interval.
  const statusModel = useMemo(
    () =>
      session && client
        ? new AppStatusModel({
            manifest,
            session,
            client,
            interval: statusInterval,
          })
        : null,
    [session, client, manifest, statusInterval]
  )
  const statusState = useSyncExternalStore(
    statusModel?.subscribe ?? noSubscription,
    statusModel?.getSnapshot ?? noStatus
  )
  useEffect(() => {
    if (!statusModel || !session) return
    const unsubscribe = statusModel.subscribe(() => {
      const { status } = statusModel.getSnapshot()
      if (status?.issues.some((i) => i.type === "accessEnded"))
        endSession(
          session,
          `${new URL(session.server).host} no longer accepts ${manifest.name}: the time you approved it for is up, or it was revoked in Settings, under Apps. Sign in again.`
        )
    })
    void statusModel.refresh(true)
    const visible = () => {
      if (document.visibilityState === "visible") void statusModel.refresh()
    }
    document.addEventListener("visibilitychange", visible)
    return () => {
      document.removeEventListener("visibilitychange", visible)
      unsubscribe()
      statusModel.close()
    }
  }, [statusModel, session, manifest.name, endSession])
  const checkStatus = useCallback(
    (force = false) => statusModel?.refresh(force) ?? Promise.resolve(),
    [statusModel]
  )

  const signIn = useCallback(
    async (located: LocatedServer) => {
      setProblem(null)
      await beginSignIn({
        manifest,
        server: located.server,
        metadata: located.metadata,
        repository: located.repository,
      })
    },
    [manifest]
  )

  const signInAgain = useCallback(async () => {
    if (!session) return
    try {
      const located = await probe(
        session.server,
        undefined,
        requirementOf(manifest)
      )
      await beginSignIn({
        manifest,
        server: located.server,
        metadata: located.metadata,
        repository: session.repository,
      })
    } catch (e) {
      toast(describe(e), "error")
    }
  }, [session, manifest, toast])

  // `signOutOf` keeps the token or revokes it before the session that holds
  // it is cleared, and clears it only while it is this tab's: clearing it
  // signs out every tab, whose requests stop carrying it; then what this
  // browser kept for the repository is forgotten while the token is
  // revoked, and the screen names it only if the server did not confirm.
  // A session another tab signed in since stands, and this tab takes it
  // up, the repository's data kept where it is that session's too. One
  // sign-out runs at a time.
  const signingOut = useMemo(() => oneAtATime(), [])
  const signOut = useCallback(() => {
    const s = session
    const l = link
    if (!s) return Promise.resolve()
    return signingOut(async () => {
      const out = await signOutOf(s, store, revocations)
      setProblem(null)
      setSession(out.type === "replaced" ? out.session : null)
      const shared =
        out.type === "replaced" &&
        out.session.repository.toLowerCase() === s.repository.toLowerCase()
      await Promise.all([
        !shared &&
          l?.forget().catch((e: unknown) =>
            setProblem({
              title: "Signed out, but not everything was forgotten",
              message: describe(e),
            })
          ),
        out.revoked,
      ])
      setUnrevoked(revocations.pending())
    })
  }, [session, link, store, revocations, signingOut])

  const value = useMemo<SubstrateContextValue | null>(
    () =>
      // A link opened for the session before is not this one's.
      session && client && link && link.linked.client === client
        ? {
            manifest,
            session,
            client,
            linked: link.linked,
            outbox: link.outbox,
            cache: link.cache,
            storage: link.storage,
            kept,
            signOut,
            signInAgain,
          }
        : null,
    [session, client, link, manifest, kept, signOut, signInAgain]
  )

  const connection = useMemo<Connection>(
    () => ({
      manifest,
      phase: completing
        ? "signingIn"
        : !session
          ? "signedOut"
          : value
            ? "open"
            : "opening",
      problem,
      unrevoked,
      signIn,
      appStatus: statusState.status,
      checking: statusState.checking,
      checkStatus,
    }),
    [
      manifest,
      completing,
      session,
      value,
      problem,
      unrevoked,
      signIn,
      statusState,
      checkStatus,
    ]
  )

  return (
    <ConnectionContext.Provider value={connection}>
      <SubstrateContext.Provider value={value}>
        {children}
      </SubstrateContext.Provider>
    </ConnectionContext.Provider>
  )
}
