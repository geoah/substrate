/** Discovery (`GET /.well-known/substrate/server.json`): what this deployment
 * serves, answered without a token and without opening a repository — which
 * is what lets the DOOR read it. The register page asks it two things: does
 * this substrate read an invite code, and does it verify a second factor at
 * all? Login asks it the first; whether to ask for a code is the login's own
 * answer (`totp_required`). Registration also reads one feature, `operatorDefaults`,
 * to decide where a new repository lands. The Settings page shows the
 * version, the REST endpoint and the features it lists.
 *
 * A local substrate with no `SUBSTRATE_INVITE_CODE` reads none, and one booted
 * with `SUBSTRATE_INSECURE_DISABLE_TOTP` verifies no code, so a console that
 * kept demanding either would be asking for something nothing checks — and
 * refusing the sign-in itself when the digits are not to hand. */

import { useEffect, useState } from "react"
import { queryOptions } from "@tanstack/react-query"

import { request } from "./http"
import type { SiteInfo } from "./types"

export interface AuthPolicy {
  /** False only where no invite code is configured, so the door reads none. */
  inviteRequired: boolean
  /** False only where the second factor is switched off. */
  totpRequired: boolean
  /** The host a short repository name is completed under: the host of the
   * server's public URL, never the address this page was reached at
   * (decision 0288). Empty where the server has none, and then a name is
   * typed whole; absent until discovery answers, and from an older server,
   * which completes a short name under the address it was reached at. */
  nameHost?: string
}

/** One entry of server.json's `features`: a group of routes the server
 * serves, with the label that says how far its shape is promised. The label
 * is read as the string it is, so a value a later server adds still shows. */
interface Feature {
  name: string
  /** `stable`, `beta` or `experimental`. */
  stability: string
  /** Counts up from 1 each time the feature gains something a client may
   * want to check for. Absent from a server older than the count. */
  revision?: number
  /** Where the server announces a coming break or a deprecation. */
  notice?: string
}

/** The fields of server.json the console reads, every one optional so an
 * older server's document still parses. It mirrors internal/api's
 * discoveryDoc, not a struct in internal/substrate, so it stays unexported
 * and out of the wire golden. */
interface DiscoveryDoc {
  registration?: Partial<AuthPolicy>
  server?: { version?: string }
  features?: Feature[]
  surfaces?: Record<string, { endpoint?: string }>
  /** The deployment's own name, logo and landing page, where it sets one.
   * `readSite` (lib/site.ts) checks it before anything uses it. */
  site?: SiteInfo
}

const DISCOVERY_PATH = "/.well-known/substrate/server.json"

// The last document this tab read, whichever reader asked: what the door's
// shape follows (doorNamesAPerson), without a request of its own.
let lastDoc: DiscoveryDoc | null = null

export function fetchDiscoveryDoc(): Promise<DiscoveryDoc> {
  return request<DiscoveryDoc>("GET", DISCOVERY_PATH, undefined, {
    anonymous: true,
  }).then((doc) => {
    lastDoc = doc
    return doc
  })
}

/** The `auth` revision whose door names a person (decision 0274): a sign-in,
 * a registration and every credential change send `identity`, the name the
 * person signs in with, and a sign-in may name the repository it opens. */
export const PERSON_DOOR_REVISION = 3

/** Whether the server's door names a person, by the last document this tab
 * read. A console bundled in an app meets an older server at its own sign-in
 * page, which the app keeps for a server below what it requires: that door
 * takes the sign-in name as `repository` and refuses `identity`, and every
 * person there signs in with their repository's name. Before any document
 * answered, the console speaks the door of the server it was built with. */
export function doorNamesAPerson(): boolean {
  return lastDoc === null || hasFeature(lastDoc, "auth", PERSON_DOOR_REVISION)
}

/** The whole document, for a page that shows what the server says about
 * itself. */
export const discoveryQueryOptions = queryOptions({
  queryKey: ["discovery", "server"],
  queryFn: fetchDiscoveryDoc,
  staleTime: 5 * 60_000,
})

/** The STRICT shape, and what an unanswered or unreadable discovery falls back
 * to: a deployment that wants a code and a console that hid the field would
 * refuse every sign-in, while the reverse merely asks for a digit nobody
 * reads. */
export const STRICT_AUTH_POLICY: AuthPolicy = {
  inviteRequired: true,
  totpRequired: true,
}

/** Whether the document lists a feature by name and, with `atLeast`, at that
 * revision or a later one. The server refuses a request field it does not
 * know, so a caller about to send what revision 3 added asks for at least 3
 * first.
 *
 * An entry without a revision is an older server's, from before revisions
 * were counted, and nothing a revision added is there: it reads as 0, below
 * every revision a caller can ask for. */
export function hasFeature(
  doc: Pick<DiscoveryDoc, "features"> | null | undefined,
  name: string,
  atLeast = 0
): boolean {
  const feature = doc?.features?.find((f) => f.name === name)
  if (!feature) return false
  const revision =
    typeof feature.revision === "number" && feature.revision > 0
      ? feature.revision
      : 0
  return revision >= atLeast
}

/** The IN-FLIGHT request, and only that: doors mounting together share one
 * call, and a door mounting later asks again.
 *
 * The answer is not a constant a tab may keep. A dev substrate is restarted
 * from `mise run dev` to `mise run dev:totp` and back, and a console holding
 * the previous answer would then send no code to a door that wants one — an
 * account nobody can sign into until they think to reload. One unauthenticated,
 * repository-free GET per mount is the cheaper side of that trade. */
let pending: Promise<DiscoveryDoc | null> | null = null

/** The door's read of the document: null where discovery did not answer. */
function fetchDoorDoc(): Promise<DiscoveryDoc | null> {
  pending ??= fetchDiscoveryDoc()
    .catch(() => null)
    .finally(() => {
      pending = null
    })
  return pending
}

export function fetchAuthPolicy(): Promise<AuthPolicy> {
  // Unreachable discovery must not leave the door unrenderable: assume the
  // strict shape, which is the only safe reading of an answer nobody gave.
  return fetchDoorDoc().then((doc) =>
    doc
      ? {
          inviteRequired: doc.registration?.inviteRequired !== false,
          totpRequired: doc.registration?.totpRequired !== false,
          nameHost:
            typeof doc.registration?.nameHost === "string"
              ? doc.registration.nameHost
              : undefined,
        }
      : STRICT_AUTH_POLICY
  )
}

/** Whether the door's discovery lists a feature: false where discovery did
 * not answer, which is what a server without the feature says too. */
export function fetchDoorFeature(name: string): Promise<boolean> {
  return fetchDoorDoc().then((doc) => hasFeature(doc, name))
}

/** Drop the in-flight request, so the next caller starts a fresh one. Tests
 * use it to keep one case's answer out of the next. */
export function resetAuthPolicy(): void {
  pending = null
  lastDoc = null
}

/** The policy as a hook. It renders STRICT until the answer lands, so a slow
 * discovery shows the code field rather than a form the substrate refuses. */
export function useAuthPolicy(): AuthPolicy {
  const [policy, setPolicy] = useState<AuthPolicy>(STRICT_AUTH_POLICY)
  useEffect(() => {
    let live = true
    void fetchAuthPolicy().then((next) => {
      if (live) setPolicy(next)
    })
    return () => {
      live = false
    }
  }, [])
  return policy
}

/** One feature as a hook, for a door: undefined until discovery answers, so
 * a caller that must act on it can still ask `fetchDoorFeature` itself. */
export function useDoorFeature(name: string): boolean | undefined {
  const [listed, setListed] = useState<boolean | undefined>(undefined)
  useEffect(() => {
    let live = true
    void fetchDoorFeature(name).then((next) => {
      if (live) setListed(next)
    })
    return () => {
      live = false
    }
  }, [name])
  return listed
}
