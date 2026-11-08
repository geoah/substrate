import {
  AppStatusModel,
  RevocationQueue,
  SessionStore,
  beginSignIn,
  completeSignIn,
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
import { ConnectScreen } from "./ConnectScreen"
import { SubstrateContext, type SubstrateContextValue } from "./context"
import { LiveCollection } from "./live"
import { ToastHost, useToast } from "./toast"

export { useSubstrate } from "./context"
export { useLive } from "./hooks"

const noSubscription = () => () => {}
const signedOutStatus: AppStatusState = { status: null, checking: false }
const noStatus = () => signedOutStatus

export interface SubstrateAppProps {
  manifest: AppManifest
  /** The kinds the app reads and follows: references, or declared kinds. */
  kinds: readonly (string | Kind<unknown, never>)[]
  /** What the app calls its records in a sentence, plural: "tasks".
   * Absent, "records". */
  kept?: string
  /** What the connect screen says the app is. */
  tagline?: string
  icon?: ReactNode
  /** How often, at most, the app's standing with its server is checked
   * again when the page comes back to the front, in ms. */
  statusInterval?: number
  children: ReactNode
}

/** An app on the owner's substrate: the connect screen until a session is
 * kept, the sign-in's return handled, and the app with its records, outbox
 * and status once signed in (the kit's AppShell over the link). */
export function SubstrateApp(props: SubstrateAppProps) {
  return (
    <ToastHost>
      <Shell {...props} />
    </ToastHost>
  )
}

function Shell({
  manifest,
  kinds,
  kept = "records",
  tagline,
  icon,
  statusInterval = 300_000,
  children,
}: SubstrateAppProps) {
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
  const [problem, setProblem] = useState<string | null>(null)
  const [unrevoked, setUnrevoked] = useState<Session[]>(() =>
    revocations.pending()
  )
  const started = useRef(false)

  const endSession = useCallback(
    (words: string) => {
      store.clear()
      setSession(null)
      setProblem(words)
    },
    [store]
  )

  // The server's redirect back. A sign-in again that fails leaves the
  // session it would have replaced in place.
  useEffect(() => {
    if (!completing || started.current) return
    started.current = true
    const previous = store.load()
    completeSignIn(manifest)
      .then((s) => {
        store.save(s)
        setSession(s)
        setProblem(null)
        if (previous && previous.token !== s.token)
          void revocations
            .revoke(previous)
            .then(() => setUnrevoked(revocations.pending()))
      })
      .catch((e) => {
        if (previous) {
          setSession(previous)
          toast(describe(e), "error")
        } else {
          setProblem(describe(e))
        }
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
    () => (session ? new SubstrateClient(session.server, session.token) : null),
    [session]
  )

  const [link, setLink] = useState<RepositoryLink<LiveCollection> | null>(null)
  useEffect(() => {
    if (!session || !client) return
    let cancelled = false
    let opened: RepositoryLink<LiveCollection> | undefined
    void openRepository({
      app: manifest.id,
      repository: session.repository,
      client,
      kinds: kindRefs,
      kept,
      makeLinked: (opts) => new LiveCollection(opts),
    }).then((l) => {
      if (cancelled) return l.close()
      opened = l
      l.start()
      setLink(l)
    })
    return () => {
      cancelled = true
      opened?.close()
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
        link.linked.getSnapshot().status === "signedOut" ||
        link.outbox.getSnapshot().state.type === "signedOut"
      if (!signedOut) return
      const waiting = link.outbox.getSnapshot().waiting
      endSession(
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
    if (!session?.expiresAt) return
    const wait = session.expiresAt - Date.now()
    if (wait > 2_147_483_647) return
    const t = setTimeout(
      () =>
        endSession(
          `${manifest.name}'s access ended: the time you approved it for is up. Sign in again.`
        ),
      Math.max(0, wait)
    )
    return () => clearTimeout(t)
  }, [session, manifest.name, endSession])

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

  const signOut = useCallback(async () => {
    const s = session
    const l = link
    store.clear()
    setSession(null)
    setProblem(null)
    if (l) await l.forget().catch(() => {})
    if (s) {
      await revocations.revoke(s)
      setUnrevoked(revocations.pending())
    }
  }, [session, link, store, revocations])

  const value = useMemo<SubstrateContextValue | null>(
    () =>
      // A link opened for the session before is not this one's.
      session && client && link && link.linked.client === client
        ? {
            manifest,
            session,
            client,
            linked: link.linked,
            collection: link.linked,
            outbox: link.outbox,
            cache: link.cache,
            storage: link.storage,
            kept,
            appStatus: statusState.status,
            checking: statusState.checking,
            checkStatus,
            unrevoked,
            signOut,
            signInAgain,
          }
        : null,
    [
      session,
      client,
      link,
      manifest,
      kept,
      statusState,
      checkStatus,
      unrevoked,
      signOut,
      signInAgain,
    ]
  )

  if (completing) {
    return <Waiting words="Signing in…" />
  }

  if (!session) {
    return (
      <ConnectScreen
        manifest={manifest}
        tagline={tagline}
        icon={icon}
        problem={problem}
        unrevoked={unrevoked}
        onSignIn={signIn}
      />
    )
  }

  if (!value) return <Waiting words="Opening…" />

  return (
    <SubstrateContext.Provider value={value}>
      {children}
    </SubstrateContext.Provider>
  )
}

function Waiting({ words }: { words: string }) {
  return (
    <div
      className="flex h-full items-center justify-center text-secondary"
      role="status"
    >
      <Spinner /> <span className="ml-3">{words}</span>
    </div>
  )
}

export function Spinner({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-block size-5 rounded-full border-2 border-current border-t-transparent motion-safe:animate-spin ${className}`}
      aria-hidden
    />
  )
}
