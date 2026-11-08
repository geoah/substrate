import {
  beginSignIn,
  completeSignIn,
  isCallback,
  revoke,
  SessionStore,
  type AppManifest,
  type Session,
} from "@substrate/auth"
import {
  SubstrateClient,
  describe,
  type LocatedServer,
} from "@substrate/client"
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react"
import { ConnectScreen } from "./ConnectScreen"
import { LiveCollection, type LiveSnapshot } from "./live"
import { ToastHost } from "./toast"

interface SubstrateContextValue {
  manifest: AppManifest
  session: Session
  client: SubstrateClient
  collection: LiveCollection
  signOut: () => Promise<void>
}

const SubstrateContext = createContext<SubstrateContextValue | null>(null)

export function useSubstrate(): SubstrateContextValue {
  const v = useContext(SubstrateContext)
  if (!v) throw new Error("useSubstrate outside <SubstrateApp>")
  return v
}

/** The live records of the app's kinds, current with the change feed. */
export function useLive(): LiveSnapshot {
  const { collection } = useSubstrate()
  return useSyncExternalStore(collection.subscribe, collection.getSnapshot)
}

export interface SubstrateAppProps {
  manifest: AppManifest
  /** The kinds the app reads and follows. */
  kinds: string[]
  /** What the connect screen says the app is. */
  tagline?: string
  icon?: ReactNode
  children: ReactNode
}

/** An app on the owner's substrate: the connect screen until a session is
 * kept, the sign-in's return handled, and the app with its live records once
 * signed in (the kit's AppShell over the link). */
export function SubstrateApp({
  manifest,
  kinds,
  tagline,
  icon,
  children,
}: SubstrateAppProps) {
  const store = useMemo(() => new SessionStore(manifest.id), [manifest.id])
  const [session, setSession] = useState<Session | null>(() =>
    isCallback(manifest) ? null : store.load()
  )
  const [completing, setCompleting] = useState(() => isCallback(manifest))
  const [problem, setProblem] = useState<string | null>(null)
  const started = useRef(false)

  useEffect(() => {
    if (!completing || started.current) return
    started.current = true
    completeSignIn(manifest)
      .then((s) => {
        store.save(s)
        setSession(s)
      })
      .catch((e) => setProblem(describe(e)))
      .finally(() => setCompleting(false))
  }, [completing, manifest, store])

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

  const client = useMemo(
    () => (session ? new SubstrateClient(session.server, session.token) : null),
    [session]
  )
  const collection = useMemo(
    () => (client ? new LiveCollection(client, kinds) : null),
    [client, kinds]
  )

  const signOut = useCallback(async () => {
    const s = session
    store.clear()
    setSession(null)
    if (s) await revoke(s).catch(() => {})
  }, [session, store])

  useEffect(() => {
    if (!collection) return
    collection.start()
    const unsubscribe = collection.subscribe(() => {
      if (collection.getSnapshot().status === "signedOut") {
        store.clear()
        setSession(null)
        setProblem("Your access to this server ended. Sign in again.")
      }
    })
    return () => {
      unsubscribe()
      collection.stop()
    }
  }, [collection, store])

  if (completing) {
    return (
      <div className="flex h-full items-center justify-center text-secondary">
        <Spinner /> <span className="ml-3">Signing in…</span>
      </div>
    )
  }

  if (!session || !client || !collection) {
    return (
      <ToastHost>
        <ConnectScreen
          manifest={manifest}
          tagline={tagline}
          icon={icon}
          problem={problem}
          onSignIn={signIn}
        />
      </ToastHost>
    )
  }

  return (
    <SubstrateContext.Provider
      value={{ manifest, session, client, collection, signOut }}
    >
      <ToastHost>{children}</ToastHost>
    </SubstrateContext.Provider>
  )
}

export function Spinner({ className = "" }: { className?: string }) {
  return (
    <span
      className={`inline-block size-5 animate-spin rounded-full border-2 border-current border-t-transparent ${className}`}
      aria-hidden
    />
  )
}
