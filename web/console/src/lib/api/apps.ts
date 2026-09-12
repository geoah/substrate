/** The two core kinds the apps runtime reads: `view` (one way of looking at
 * records) and `app` (an ordered list of screens over views). Both are
 * ordinary record collections under the core package, so their records
 * arrive as `SubstrateRecord` and nothing here touches the wire golden.
 *
 * Functions and constants only: an exported interface in this directory must
 * be pinned in `wire.golden.test.ts`, and a view's decoded shape is the apps
 * runtime's own (`lib/apps/spec.ts`), not the wire's. */

import { CORE_PACKAGE, CORE_AUTHORITY, CORE_PACKAGE_NAME } from "./http"
import { recordQueryOptions, recordsQueryOptions } from "./records"

/** The collection names, which are the kinds' own words. */
export const VIEW_NAME = "view"
export const APP_NAME = "app"

/** The kind identities, `<authority>/<package>/<name>`. */
export const VIEW_KIND = `${CORE_PACKAGE}/${VIEW_NAME}`
export const APP_KIND = `${CORE_PACKAGE}/${APP_NAME}`

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

export function viewQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, VIEW_NAME, id)
}

export function appQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, APP_NAME, id)
}
