/** Bundle lifecycle reads and writes. Everything rides the host's computed
 * status endpoints and the lifecycle transitions — the bundle ROWS themselves
 * are ordinary `substrate.reamde.dev/core/bundle` records the generic browse
 * already renders. Account configs are a TRAIT query (kinds implementing
 * `accountconfig`), and the OAuth connect button hits `oauth/start` for one
 * account record. */

import { queryOptions, type QueryClient } from "@tanstack/react-query"

import { catalogQueryOptions, type CatalogItem } from "./catalog"
import { CORE_PACKAGE, corePath, request, rootPath, seg } from "./http"
import type {
  BundleStatus,
  OperationalList,
  SubstrateRecord,
  Page,
} from "./types"

/** Re-exported so the pages keep their `@/lib/api/bundles` import for the
 * computed status shape (the wire types live in types.ts). */
export type { BundleStatus, InputStatus, SetupItem } from "./types"

/** The account-config trait, host-recognized, by its full identity, so a
 * package-local look-alike can never answer for it (api/bundles.go note). It
 * is a kind reference like any other: the core package, then the trait's own
 * name. */
export const ACCOUNT_CONFIG_TRAIT = `${CORE_PACKAGE}/accountconfig`

const BUNDLES = corePath("bundle")

/** Every installed bundle's computed status. */
export async function fetchBundleStatuses(
  signal?: AbortSignal
): Promise<BundleStatus[]> {
  const res = await request<OperationalList<BundleStatus>>(
    "GET",
    `${BUNDLES}/status`,
    undefined,
    { signal }
  )
  return res.items ?? []
}

export const bundleStatusesQueryOptions = queryOptions({
  queryKey: ["bundles", "status"],
  queryFn: ({ signal }) => fetchBundleStatuses(signal),
  staleTime: 30_000,
})

export function bundleStatusQueryOptions(id: string) {
  return queryOptions({
    queryKey: ["bundle", "status", id],
    queryFn: ({ signal }) =>
      request<BundleStatus>("GET", `${BUNDLES}/${seg(id)}/status`, undefined, {
        signal,
      }),
    staleTime: 30_000,
  })
}

/** The lifecycle transitions. A bundle's runtime lifecycle IS record state the
 * substrate owns (decision 0033), so each is a PATCH of the bundle record's
 * `disabled`/`uninstalled`/`purging` state, not a verb. disable/enable answer
 * with the fresh status; uninstall tears the bundle row down, so there is no
 * status to answer and the server acks instead; purge answers with the
 * tombstoned-row count. */
export type BundleVerb = "disable" | "enable" | "uninstall"

/** PATCH one bundle-state property. */
function patchBundleState<T>(
  id: string,
  prop: string,
  value: unknown
): Promise<T> {
  return request<T>("PATCH", `${BUNDLES}/${seg(id)}`, {
    properties: { [prop]: value },
  })
}

export function runBundleVerb(
  id: string,
  verb: "disable" | "enable"
): Promise<BundleStatus> {
  return patchBundleState<BundleStatus>(id, "disabled", verb === "disable")
}

export function uninstallBundle(id: string): Promise<{ uninstalled: boolean }> {
  return patchBundleState<{ uninstalled: boolean }>(id, "uninstalled", true)
}

export function purgeBundle(id: string): Promise<{ purged: number }> {
  return patchBundleState<{ purged: number }>(id, "purging", true)
}

/** Bind one input to a record (a reference on the bundle's record row, named
 * for the input); an empty record unbinds. Answers with the refreshed
 * status. */
export function bindBundleInput(
  id: string,
  input: string,
  record: string
): Promise<BundleStatus> {
  return request<BundleStatus>("POST", `${BUNDLES}/${seg(id)}/bind`, {
    input,
    record,
  })
}

/** Fold one fresh BundleStatus — the answer a lifecycle verb or a catalog
 * install returns — into every cache that renders bundle state: the detail
 * status, the statuses list, and the catalog entry's installed flag. */
export function seedBundleStatus(
  queryClient: QueryClient,
  status: BundleStatus
): void {
  queryClient.setQueryData(bundleStatusQueryOptions(status.id).queryKey, status)
  queryClient.setQueryData<BundleStatus[]>(
    bundleStatusesQueryOptions.queryKey,
    (prev) => {
      if (!prev) return prev
      return prev.some((b) => b.id === status.id)
        ? prev.map((b) => (b.id === status.id ? status : b))
        : [...prev, status]
    }
  )
  queryClient.setQueryData<CatalogItem[]>(
    catalogQueryOptions.queryKey,
    (prev) =>
      prev?.map((item) =>
        item.id === status.id ? { ...item, installed: status.installed } : item
      )
  )
}

/** Re-read the bundle-state surfaces shortly after a lifecycle verb lands. */
export function refetchBundleStateSoon(queryClient: QueryClient): void {
  for (const delay of [1_500, 5_000]) {
    setTimeout(() => {
      void queryClient.invalidateQueries({ queryKey: ["bundle"] })
      void queryClient.invalidateQueries({
        queryKey: bundleStatusesQueryOptions.queryKey,
      })
      void queryClient.invalidateQueries({
        queryKey: catalogQueryOptions.queryKey,
      })
      void queryClient.invalidateQueries({ queryKey: ["connections"] })
    }, delay)
  }
}

/** The bounded cap for the embedded account list. The wire has no server-side
 * authority filter, so the console reads ONE bounded page of accountconfig
 * records across all bundles and filters to this bundle client-side. */
export const TRAIT_ACCOUNTS_CAP = 200

/** One bounded read of a trait's records, with whether the wire held more. */
export interface TraitRecords {
  records: SubstrateRecord[]
  /** More rows exist beyond the bounded read — the embedded list cannot be
   * trusted complete and the surface should say so. */
  capped: boolean
}

/** The account-config records: a bounded page of live records of a kind
 * implementing the `accountconfig` trait, across all bundles — the page scopes
 * to one bundle by the record's authority. */
export function traitRecordsQueryOptions(trait: string) {
  return queryOptions({
    queryKey: ["trait", "records", trait],
    queryFn: async ({ signal }): Promise<TraitRecords> => {
      const q = new URLSearchParams({ first: String(TRAIT_ACCOUNTS_CAP) })
      const page = await request<Page>(
        "GET",
        `${corePath("trait", trait)}/records?${q}`,
        undefined,
        { signal }
      )
      const records = page.records ?? []
      return {
        records,
        capped: Boolean(page.cursor && records.length >= TRAIT_ACCOUNTS_CAP),
      }
    },
    staleTime: 30_000,
  })
}

/** Begin the host connect flow for one account record: the response carries
 * the provider consent URL the browser should visit. */
export function startOAuth(record: string): Promise<{ url: string }> {
  return request<{ url: string }>("POST", rootPath("oauth", "start"), {
    record,
  })
}

// ── OAuth return-to-origin (postMessage from the substrate callback) ─────────

/** The `source` discriminator every OAuth-callback message carries. MUST match
 * the backend callback's `postMessage` payload exactly. */
export const SUBSTRATE_OAUTH_SOURCE = "substrate-oauth"

/** The parsed OAuth-return message: a success names the account record that got
 * connected (so the right row invalidates), a failure names a correlation id
 * for the owner to quote when the host logs the reason. */
export type SubstrateOAuthMessage =
  { ok: true; record: string } | { ok: false; correlation: string }

/** Validate an untrusted `MessageEvent.data` as one of our OAuth-return
 * messages, returning null for anything that is not ours. */
export function parseSubstrateOAuthMessage(
  data: unknown
): SubstrateOAuthMessage | null {
  if (typeof data !== "object" || data === null) return null
  const msg = data as Record<string, unknown>
  if (msg.source !== SUBSTRATE_OAUTH_SOURCE) return null
  if (msg.ok === true) {
    return typeof msg.record === "string" && msg.record
      ? { ok: true, record: msg.record }
      : null
  }
  if (msg.ok === false) {
    return {
      ok: false,
      correlation: typeof msg.correlation === "string" ? msg.correlation : "",
    }
  }
  return null
}

/** A bundle's lifecycle stance, for a status chip. LIFECYCLE ONLY: setup is a
 * separate signal (the setup list on the status), never folded in here.
 * Quarantined comes first: the only installed:false statuses the server
 * emits are quarantined ones (uninstall stops listing the bundle at all),
 * and the fix is a re-install, not an install. */
export type BundleState = "quarantined" | "uninstalled" | "disabled" | "enabled"

export function bundleState(b: BundleStatus): BundleState {
  if (b.quarantined) return "quarantined"
  if (!b.installed) return "uninstalled"
  if (!b.enabled) return "disabled"
  return "enabled"
}

/** How many setup steps stand between the bundle and readiness. Zero means
 * ready, and every surface showing the count shows nothing at zero. */
export function setupCount(b: Pick<BundleStatus, "setup">): number {
  return b.setup?.length ?? 0
}
