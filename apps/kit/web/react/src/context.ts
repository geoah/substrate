import type { AppManifest, AppStatus, Session } from "@substrate/auth"
import type { LocatedServer, SubstrateClient } from "@substrate/client"
import type {
  LinkedRecords,
  Outbox,
  RecordCache,
  RepositoryStorage,
} from "@substrate/local"
import { createContext, useContext } from "react"

/** The open repository: what every screen reads records and writes
 * through. It changes only with the session, so a status check or a
 * connection's state never draws a list again. */
export interface SubstrateContextValue {
  manifest: AppManifest
  session: Session
  client: SubstrateClient
  /** The records of the app's kinds as the owner sees them. */
  linked: LinkedRecords
  outbox: Outbox
  cache: RecordCache
  storage: RepositoryStorage
  /** What the app calls its records in a sentence, plural: "tasks". */
  kept: string
  /** Queues the app's access to revoke, forgets what this browser kept for
   * the repository in every tab, and shows the connect screen; or, where
   * another tab signed in since, queues this tab's access alone and takes
   * up that session. The revocation is sent in the background. Rejects
   * with `NotSignedOut`, still signed in, when the browser could not keep
   * the token to revoke. */
  signOut: () => Promise<void>
  /** The server's sign-in and approval page again, for this repository: to
   * review more access, renew it, or sign in after it ended. `scopes` are
   * asked for this time beside the manifest's and `askScopes`'s. */
  signInAgain: (scopes?: readonly string[]) => Promise<void>
  /** The scopes the app asks for now beyond its manifest's, such as write
   * access to a kind the owner chose: every later `signInAgain` asks for
   * them with the manifest's. */
  askScopes: (scopes: readonly string[]) => void
}

/** Where the app stands with its server, signed in or not: what the connect
 * screen, a banner or Settings shows apart from the records. */
export interface Connection {
  manifest: AppManifest
  /** `signingIn` while the server's redirect back is exchanged, `signedOut`
   * with no session (the connect screen's), `opening` while the
   * repository opens, `open` once the records can be read. */
  phase: "signingIn" | "signedOut" | "opening" | "open"
  /** Why the last session ended, or a sign-in failed. */
  problem: { title: string; message: string } | null
  /** Tokens this browser stopped using that their servers have not
   * confirmed revoking. */
  unrevoked: readonly Session[]
  /** Leaves for the server's sign-in and approval page, in this tab. */
  signIn: (located: LocatedServer) => Promise<void>
  /** The last check of the app's standing with its server; null until one
   * answered. */
  appStatus: AppStatus | null
  checking: boolean
  checkStatus: (force?: boolean) => Promise<void>
}

export const SubstrateContext = createContext<SubstrateContextValue | null>(
  null
)

export const ConnectionContext = createContext<Connection | null>(null)

/** The open repository: its manifest, session, client, records and outbox.
 * Only under `SubstrateApp`'s children, or a `SubstrateProvider` whose
 * phase is `open`. */
export function useSubstrate(): SubstrateContextValue {
  const v = useContext(SubstrateContext)
  if (!v)
    throw new Error(
      "useSubstrate outside an open repository: under <SubstrateApp>, or <SubstrateProvider> once useConnection().phase is open"
    )
  return v
}

/** Where the app stands with its server, anywhere under a provider. */
export function useConnection(): Connection {
  const v = useContext(ConnectionContext)
  if (!v)
    throw new Error(
      "useConnection outside <SubstrateProvider> or <SubstrateApp>"
    )
  return v
}
