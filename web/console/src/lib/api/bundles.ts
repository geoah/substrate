/** Bundle lifecycle reads and writes. Everything rides the host's computed
 * status endpoints and the lifecycle verbs — the bundle ROWS themselves are
 * ordinary `core.substrate.reamde.dev/bundles` records the generic browse already
 * renders. Account configs are a TRAIT query (kinds implementing
 * `accountconfig`), and the OAuth connect button hits `oauth/start` for one
 * account record. */

import { queryOptions, type QueryClient } from "@tanstack/react-query"

import { catalogQueryOptions, type CatalogItem } from "./catalog"
import { API_BASE, CORE_AUTHORITY, corePath, request, seg } from "./http"
import type { BundleStatus, SubstrateRecord, Page } from "./types"

/** Re-exported so the pages keep their `@/lib/api/bundles` import for the
 * computed status shape (the wire type lives in types.ts). */
export type { BundleStatus } from "./types"

/** The account-config trait, host-recognized — its full identity, so a
 * bundle-local look-alike can never answer for it (api/bundles.go note).
 * NOTE(v1): the trait-reference spelling and the `traits/{trait}/records`
 * read below are unverified against the v1 wire — the standalone connectors
 * plane is gone; anyone building the integrations Accounts surface should
 * confirm both against the server before relying on them. */
export const ACCOUNT_CONFIG_TRAIT = "accountconfig.core.substrate.reamde.dev"

const BUNDLES = corePath("bundles")

/** Every installed bundle's computed status. */
export async function fetchBundleStatuses(
  signal?: AbortSignal
): Promise<BundleStatus[]> {
  const res = await request<{ bundles?: BundleStatus[] }>(
    "GET",
    `${BUNDLES}/status`,
    undefined,
    { signal }
  )
  return res.bundles ?? []
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
      request<BundleStatus>(
        "GET",
        `${BUNDLES}/${seg(id)}/status`,
        undefined,
        { signal }
      ),
    staleTime: 30_000,
  })
}

/** The lifecycle verbs. disable/enable/uninstall answer with the fresh status;
 * purge answers with the tombstoned-row count. */
export type BundleVerb = "disable" | "enable" | "uninstall"

export function runBundleVerb(id: string, verb: BundleVerb): Promise<BundleStatus> {
  return request<BundleStatus>("POST", `${BUNDLES}/${seg(id)}/${verb}`)
}

export function purgeBundle(id: string): Promise<{ purged: number }> {
  return request<{ purged: number }>("POST", `${BUNDLES}/${seg(id)}/purge`)
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
  queryClient.setQueryData<CatalogItem[]>(catalogQueryOptions.queryKey, (prev) =>
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
        `${corePath("traits", trait)}/records?${q}`,
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
  return request<{ url: string }>(
    "POST",
    `${API_BASE}/${CORE_AUTHORITY}/oauth/start`,
    { record }
  )
}

// ── OAuth return-to-origin (postMessage from the substrate callback) ─────────

/** The `source` discriminator every OAuth-callback message carries. MUST match
 * the backend callback's `postMessage` payload exactly. */
export const SUBSTRATE_OAUTH_SOURCE = "substrate-oauth"

/** The parsed OAuth-return message: a success names the account record that got
 * connected (so the right row invalidates), a failure names a correlation id
 * for the owner to quote when the host logs the reason. */
export type SubstrateOAuthMessage =
  | { ok: true; record: string }
  | { ok: false; correlation: string }

/** Validate an untrusted `MessageEvent.data` as one of our OAuth-return
 * messages, returning null for anything that is not ours. */
export function parseSubstrateOAuthMessage(data: unknown): SubstrateOAuthMessage | null {
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

/** A bundle's human-readable lifecycle stance, for a status chip. */
export type BundleState =
  | "needs-configuration"
  | "uninstalled"
  | "disabled"
  | "enabled"

export function bundleState(b: BundleStatus): BundleState {
  if (!b.installed) return "uninstalled"
  if (!b.enabled) return "disabled"
  if (!b.configured) return "needs-configuration"
  return "enabled"
}
