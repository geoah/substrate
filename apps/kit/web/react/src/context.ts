import type { AppManifest, AppStatus, Session } from "@substrate/auth"
import type { SubstrateClient } from "@substrate/client"
import type {
  LinkedRecords,
  Outbox,
  RecordCache,
  RepositoryStorage,
} from "@substrate/local"
import { createContext, useContext } from "react"
import type { LiveCollection } from "./live"

export interface SubstrateContextValue {
  manifest: AppManifest
  session: Session
  client: SubstrateClient
  /** The records of the app's kinds as the owner sees them. */
  linked: LinkedRecords
  /** @deprecated The same object as `linked`; read records through
   * `useRecords` or `useLive`, write through `useWrites`. */
  collection: LiveCollection
  outbox: Outbox
  cache: RecordCache
  storage: RepositoryStorage
  /** What the app calls its records in a sentence, plural: "tasks". */
  kept: string
  /** The last check of the app's standing with its server; null until one
   * answered. */
  appStatus: AppStatus | null
  checking: boolean
  checkStatus: (force?: boolean) => Promise<void>
  /** Tokens this browser stopped using that their servers have not
   * confirmed revoking. */
  unrevoked: Session[]
  /** Revokes the app's access, forgets what this browser kept for the
   * repository, and shows the connect screen. */
  signOut: () => Promise<void>
  /** The server's sign-in and approval page again, for this repository: to
   * review more access, renew it, or sign in after it ended. */
  signInAgain: () => Promise<void>
}

export const SubstrateContext = createContext<SubstrateContextValue | null>(
  null
)

/** The signed-in app: its manifest, session, client, records and outbox. */
export function useSubstrate(): SubstrateContextValue {
  const v = useContext(SubstrateContext)
  if (!v) throw new Error("useSubstrate outside <SubstrateApp>")
  return v
}
