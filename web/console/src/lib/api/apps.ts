/** The one core kind the apps runtime reads: `app`, a record whose `source`
 * the console mounts behind the guest. An ordinary record collection under
 * the core package, so its rows arrive as `SubstrateRecord` and nothing here
 * touches the wire golden.
 *
 * Functions and constants only: an exported interface in this directory must
 * be pinned in `wire.golden.test.ts`, and an app's decoded shape is the apps
 * runtime's own (`lib/apps/spec.ts`), not the wire's. The three readers at
 * the end are what an attach point needs BEFORE the runtime chunk loads: they
 * read the raw row and decode nothing else. */

import { CORE_PACKAGE, CORE_AUTHORITY, CORE_PACKAGE_NAME } from "./http"
import { recordQueryOptions, recordsQueryOptions } from "./records"
import { readReference, type SubstrateRecord } from "./types"

/** The collection name, which is the kind's own word. */
export const APP_NAME = "app"

/** The kind identity, `<authority>/<package>/<name>`. */
export const APP_KIND = `${CORE_PACKAGE}/${APP_NAME}`

/** A declaration record's id is the kind identity, so a stored `kind:`
 * reference reads `substrate.reamde.dev/core/kind/<identity>`; this is the
 * prefix a reader strips to get the identity back. The trait, function and
 * agent prefixes are the same shape for the grant's other arms. */
export const KIND_RECORD_PREFIX = `${CORE_PACKAGE}/kind/`
export const TRAIT_RECORD_PREFIX = `${CORE_PACKAGE}/trait/`
export const FUNCTION_RECORD_PREFIX = `${CORE_PACKAGE}/function/`
export const AGENT_RECORD_PREFIX = `${CORE_PACKAGE}/agent/`

/** One page comfortably above any hand-written set of apps. */
const APPS_PAGE = 200

export function appsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: APP_NAME,
    first: APPS_PAGE,
  })
}

export function appQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME, id)
}

/** The `attach` list off a raw app row; absent means the launcher alone. */
export function appAttaches(
  app: SubstrateRecord,
  at: "launcher" | "home" | "record" | "browse"
): boolean {
  const attach = app.properties.attach
  if (!Array.isArray(attach)) return at === "launcher"
  return attach.includes(at)
}

/** The kind identities a raw app row's grant reads, prefixes stripped. */
export function appReadKinds(app: SubstrateRecord): string[] {
  const permissions = app.properties.permissions
  const reads =
    permissions && typeof permissions === "object"
      ? (permissions as { reads?: { kinds?: unknown } }).reads?.kinds
      : undefined
  if (!Array.isArray(reads)) return []
  return reads.flatMap((value) => {
    const held = readReference(value)
    if (!held) return []
    return [
      held.path.startsWith(KIND_RECORD_PREFIX)
        ? held.path.slice(KIND_RECORD_PREFIX.length)
        : held.path,
    ]
  })
}
