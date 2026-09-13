/** The core kinds the apps runtime reads: `view` (one way of looking at
 * records), `app` (an ordered list of screens over views) and `package` (one
 * row per installed package, whose `version` is what a view's
 * `requiresAtLeast` floor is held against). All are ordinary record
 * collections under the core package, readable by the repository's token
 * like `kind` is, so their records arrive as `SubstrateRecord` and nothing
 * here touches the wire golden.
 *
 * Functions and constants only: an exported interface in this directory must
 * be pinned in `wire.golden.test.ts`, and a view's decoded shape is the apps
 * runtime's own (`lib/apps/spec.ts`), not the wire's. */

import { CORE_PACKAGE, CORE_AUTHORITY, CORE_PACKAGE_NAME } from "./http"
import { recordQueryOptions, recordsQueryOptions } from "./records"
import type { SubstrateRecord } from "./types"

/** The collection names, which are the kinds' own words. */
export const VIEW_NAME = "view"
export const APP_NAME = "app"
export const PACKAGE_NAME = "package"

/** The kind identities, `<authority>/<package>/<name>`. */
export const VIEW_KIND = `${CORE_PACKAGE}/${VIEW_NAME}`
export const APP_KIND = `${CORE_PACKAGE}/${APP_NAME}`
export const PACKAGE_KIND = `${CORE_PACKAGE}/${PACKAGE_NAME}`

/** A declaration record's id is the kind identity, so a stored `kind:`
 * reference reads `substrate.reamde.dev/core/kind/<identity>`; this is the
 * prefix a reader strips to get the identity back. */
export const KIND_RECORD_PREFIX = `${CORE_PACKAGE}/kind/`
export const TRAIT_RECORD_PREFIX = `${CORE_PACKAGE}/trait/`
export const VIEW_RECORD_PREFIX = `${VIEW_KIND}/`

/** One page comfortably above any hand-written set of views or apps. */
const APPS_PAGE = 200

export function viewsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: VIEW_NAME,
    first: APPS_PAGE,
  })
}

export function appsQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: APP_NAME,
    first: APPS_PAGE,
  })
}

/** Every installed package's row. A repository holds a few dozen packages at
 * most, so one page is the whole set; the live tail watches the kind, so an
 * install or an upgrade refetches it. */
export function packagesQueryOptions() {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: PACKAGE_NAME,
    first: APPS_PAGE,
  })
}

/** Package identity → installed version, off the package rows. The id IS
 * the identity (`<authority>/<package>`, decision record 0047) and the
 * closure's version is the row's `version` PROPERTY, engine-maintained; the
 * record's own `version` beside it counts writes to the row and is not it.
 * `undefined` in is `undefined` out, so a caller can tell "not loaded" from
 * "no packages". */
export function installedVersions(
  records: SubstrateRecord[] | undefined
): Record<string, number> | undefined {
  if (!records) return undefined
  const out: Record<string, number> = {}
  for (const row of records) {
    const version = row.properties.version
    if (typeof version === "number") out[row.id] = version
  }
  return out
}

export function viewQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, VIEW_NAME, id)
}

export function appQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME, id)
}
