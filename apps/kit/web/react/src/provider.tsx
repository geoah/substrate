import {
  AppStatusModel,
  SessionKeeper,
  beginSignIn,
  completeSignIn,
  discardStrayCallback,
  isCallback,
  requirementOf,
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
import { ReadCache } from "./reads"
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
  private readonly keeper: SessionKeeper

  constructor(session: Session, keeper: SessionKeeper) {
    super(session.server, session.token)
    this.keeper = keeper
  }

  override headers(accept?: string): Headers {
    const h = super.headers(accept)
    if (this.token && !this.keeper.current(this.token))
      h.delete("Authorization")
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
  const keeper = useMemo(() => new SessionKeeper(manifest.id), [manifest.id])
  const kindKey = kinds
    .map((k) => (typeof k === "string" ? k : k.kind))
    .join(" ")
  const kindRefs = useMemo(() => kindKey.split(" ").filter(Boolean), [kindKey])
  const [session, setSession] = useState<Session | null>(() =>
    isCallback(manifest) ? null : keeper.load()
  )
  const [completing, setCompleting] = useState(() => isCallback(manifest))
  const [problem, setProblem] = useState<Connection["problem"]>(null)
  const [unrevoked, setUnrevoked] = useState<Session[]>(() => keeper.pending())
  const started = useRef(false)

  useEffect(() => {
    discardStrayCallback(manifest)
  }, [manifest])

  const shown = useRef(session)
  useEffect(() => {
    shown.current = session
  })

  /** A session whose access ended here: forgotten unless another tab has
   * signed in since, whose session this tab takes up instead. One this tab
   * has moved on from meanwhile is left be. `cause` is the keeper's: a 401
   * the server answered, or a time up that may rest on this browser's
   * clock, whose token is still queued to revoke. */
  const endSession = useCallback(
    (ended: Session, words: string, cause: "refused" | "timeUp") => {
      void keeper.end(ended, cause).then((now) => {
        if (shown.current?.token !== ended.token) return
        shown.current = now
        setSession(now)
        setProblem(
          now
            ? null
            : { title: `${manifest.name}'s access ended`, message: words }
        )
      })
    },
    [keeper, manifest.name]
  )

  // Every tab runs on the one kept session: another tab's sign-out closes
  // this one's repository and drops its records, and its sign-in opens
  // here too. A page back from the cache, or to the front, looks again,
  // since it heard nothing while it was away.
  useEffect(() => {
    const look = () => {
      setUnrevoked(keeper.pending())
      const now = keeper.load()
      if (shown.current?.token === now?.token) return
      shown.current = now
      setProblem(null)
      setSession(now)
    }
    const stopSession = keeper.watchSession(look)
    const stopRevocations = keeper.watchQueue(() =>
      setUnrevoked(keeper.pending())
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
  }, [keeper])

  // The server's redirect back, kept by `keeper.signIn`: the session it
  // replaces is the one kept when it is saved, which another tab may have
  // signed in meanwhile, and its revocation is sent after, never waited
  // on. A sign-in that is not kept leaves the kept session in place.
  useEffect(() => {
    if (!completing || started.current) return
    started.current = true
    completeSignIn(manifest, window.location, (s) => keeper.discard(s))
      .then(async (s) => {
        await keeper.signIn(s)
        shown.current = s
        setSession(s)
        setProblem(null)
      })
      .catch((e: unknown) => {
        const kept = keeper.load()
        shown.current = kept
        setSession(kept)
        if (kept) toast(describe(e), "error")
        else setProblem({ title: "Couldn't sign in", message: describe(e) })
      })
      .finally(() => setCompleting(false))
  }, [completing, manifest, keeper, toast])

  // The revocations still owed, sent at every open and whenever the
  // network comes back, and again after a wait while the server does not
  // confirm them.
  useEffect(() => {
    const stop = keeper.onUnrevoked(setUnrevoked)
    const retry = () => void keeper.send()
    retry()
    window.addEventListener("online", retry)
    return () => {
      stop()
      window.removeEventListener("online", retry)
    }
  }, [keeper])

  const client = useMemo(
    () => (session ? new SessionClient(session, keeper) : null),
    [session, keeper]
  )

  const [link, setLink] = useState<RepositoryLink | null>(null)
  const [reads, setReads] = useState<ReadCache | null>(null)
  useEffect(() => {
    if (!session || !client) return
    let cancelled = false
    let opened: RepositoryLink | undefined
    let cache: ReadCache | undefined
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
      cache = new ReadCache({ client, outbox: l.outbox })
      cache.start()
      setLink(l)
      setReads(cache)
    })
    // The records and the reads go with the link: a session ended in
    // another tab leaves nothing of it shown here, and every read of it is
    // aborted.
    return () => {
      cancelled = true
      cache?.close()
      void opened?.close()
      setLink(null)
      setReads(null)
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
            : "."),
        "refused"
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
          "The time you approved it for is up. Sign in again.",
          "timeUp"
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
        // The check may rest on this browser's clock, so the token is
        // queued to revoke as for a time up.
        endSession(
          session,
          `${new URL(session.server).host} no longer accepts ${manifest.name}: the time you approved it for is up, or it was revoked in Settings, under Apps. Sign in again.`,
          "timeUp"
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
    (force = false) =>
      statusModel ? statusModel.refresh(force) : Promise.resolve(),
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

  // `keeper.signOut` queues the token before it clears the session, and
  // clears it only while it is this tab's: clearing it signs out every
  // tab, whose requests stop carrying it; then what this browser kept for
  // the repository is forgotten, while the revocation goes in the
  // background. A session another tab signed in since stands, and this tab
  // takes it up, the repository's data kept where it is that session's
  // too. One sign-out runs at a time.
  const signingOut = useMemo(() => oneAtATime(), [])
  const signOut = useCallback(() => {
    const s = session
    const l = link
    if (!s) return Promise.resolve()
    return signingOut(async () => {
      const out = await keeper.signOut(s)
      const now = out.type === "replaced" ? out.session : null
      setProblem(null)
      setSession(now)
      const shared =
        now?.repository.toLowerCase() === s.repository.toLowerCase()
      if (!shared)
        await l?.forget().catch((e: unknown) =>
          setProblem({
            title: "Signed out, but not everything was forgotten",
            message: describe(e),
          })
        )
    })
  }, [session, link, keeper, signingOut])

  const value = useMemo<SubstrateContextValue | null>(
    () =>
      // A link opened for the session before is not this one's.
      session &&
      client &&
      link &&
      reads &&
      link.linked.client === client &&
      reads.client === client
        ? {
            manifest,
            session,
            client,
            linked: link.linked,
            outbox: link.outbox,
            cache: link.cache,
            storage: link.storage,
            reads,
            kept,
            signOut,
            signInAgain,
          }
        : null,
    [session, client, link, reads, manifest, kept, signOut, signInAgain]
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
