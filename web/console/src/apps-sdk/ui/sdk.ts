/** The one place the kit touches `substrate/app`. The kit is built as its own
 * entry and resolved from the guest's import map beside the SDK, so it must
 * reach the SAME singletons the app imports: a relative import lands on the
 * same URL in dev and the same shared chunk in a build. A test replaces this
 * module, and a rename in the SDK is one edit here. */

export { host, records, useKind } from "../index"
export type { Page } from "../index"
