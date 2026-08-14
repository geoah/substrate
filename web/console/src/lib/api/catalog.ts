/** The bundle CATALOG: the importable bundle closures shipped in the binary —
 * what the Registry page lists and imports. The catalog read flags each entry
 * with whether THIS repository already has it, so the page can fold imported +
 * not-imported into one list; the import runs the same admission path an
 * explicit apply uses and answers with the freshly imported bundle's status. */

import { queryOptions } from "@tanstack/react-query"

import { API_BASE, CORE_AUTHORITY, corePath, request, seg } from "./http"
import type { BundleClosure, BundleStatus, CatalogBundle } from "./types"

/** One shipped bundle closure plus whether this repository already installed it
 * (substrate catalog.Bundle + the installed flag). The console's page vocabulary
 * kept the name `CatalogItem`; the wire type is `CatalogBundle`. */
export type CatalogItem = CatalogBundle
/** What installing lands, by kind — the detail preview before installing. */
export type CatalogClosure = BundleClosure

const CATALOG = corePath("catalog")

export const catalogQueryOptions = queryOptions({
  queryKey: ["catalog"],
  queryFn: async ({ signal }) => {
    const res = await request<{ catalog?: CatalogItem[] }>(
      "GET",
      CATALOG,
      undefined,
      { signal }
    )
    return res.catalog ?? []
  },
  staleTime: 60_000,
})

/** One catalog entry by bundle id, sharing the list's cache (the same
 * `["catalog"]` query, selected down). Returns undefined when this repository's
 * bundle is not a shipped closure — an installed-via-apply bundle has no
 * catalog entry, and the caller falls back to what the registry alone knows. */
export function catalogItemQueryOptions(id: string) {
  return queryOptions({
    queryKey: catalogQueryOptions.queryKey,
    queryFn: catalogQueryOptions.queryFn,
    staleTime: 60_000,
    select: (items: CatalogItem[]) => items.find((i) => i.id === id),
  })
}

/** Import a shipped bundle's closure into this repository. Owner-gated and
 * idempotent (re-importing is the bundle's own upgrade semantics); the response
 * is the imported bundle's computed status. The catalog id is a reference and
 * may carry a `/`, so it is `%2F`-encoded as one path segment.
 *
 * NAME NOTE: the console says IMPORT (owner ruling) but the WIRE verb is
 * unchanged — this POSTs `…/catalog/{id}/install`. The function is named for
 * what the reader asked for; the path is named for what the server serves. */
export function importBundle(id: string): Promise<BundleStatus> {
  return request<BundleStatus>("POST", `${CATALOG}/${seg(id)}/install`)
}

/** The substrate origin that actually receives the provider OAuth redirect —
 * NOT the console origin. The server is configured with
 * `SUBSTRATE_OAUTH_CALLBACK_URL`, and the console is often served elsewhere.
 * The provider matches the redirect URI EXACTLY, so deriving it from
 * `window.location.origin` would hand the owner a URI the authorization
 * request never sends — a guaranteed `redirect_uri_mismatch`. A deployment
 * setting must never be guessed in the browser, so it is baked at build time
 * and MUST be set to the same origin the server's callback URL carries. */
const SUBSTRATE_ORIGIN =
  import.meta.env.VITE_SUBSTRATE_ORIGIN ?? "https://substrate.example.com"

/** The exact OAuth callback URL the owner must register in their provider
 * client — the `GET /core.substrate.reamde.dev/oauth/callback` the host serves, on the
 * substrate's own origin. KNOWN CONSTANT, not derived. */
export const OAUTH_CALLBACK_URL = `${SUBSTRATE_ORIGIN}${API_BASE}/${CORE_AUTHORITY}/oauth/callback`

export function oauthCallbackURL(): string {
  return OAUTH_CALLBACK_URL
}
