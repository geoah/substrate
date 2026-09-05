/** The bundle CATALOG: the importable bundle closures shipped in the binary —
 * what the Registry page lists and imports. The catalog read flags each entry
 * with whether THIS repository already has it, so the page can fold imported +
 * not-imported into one list; the import runs the same admission path an
 * explicit apply uses and answers with the freshly imported bundle's status. */

import { queryOptions } from "@tanstack/react-query"

import { rootPath, request, seg } from "./http"
import type {
  BundleClosure,
  BundleStatus,
  CatalogItem,
  CatalogTier,
  OperationalList,
} from "./types"

/** One shipped bundle closure plus whether this repository already has it. */
export type { CatalogItem } from "./types"
/** What installing lands, by kind — the detail preview before installing. */
export type CatalogClosure = BundleClosure

const CATALOG = rootPath("catalog")

export const catalogQueryOptions = queryOptions({
  queryKey: ["catalog"],
  queryFn: async ({ signal }) => {
    const res = await request<OperationalList<CatalogItem>>(
      "GET",
      CATALOG,
      undefined,
      { signal }
    )
    return res.items ?? []
  },
  staleTime: 60_000,
})

/** The id a catalog entry has in a repository whose own authority is `home`:
 * a provider keeps the id it publishes, a SAMPLE takes this repository's
 * authority with the sample's package (decision records 0047 and 0048). This
 * is the id the bundle STATUS carries once it lands, so it is how a stored
 * bundle finds its shipped closure again. */
export function landedId(
  item: Pick<CatalogItem, "id" | "tier" | "package">,
  home: string
): string {
  if (item.tier !== "sample" || !home) return item.id
  return `${home}/${item.package}`
}

/** One catalog entry as it lands HERE: a sample's authority, closure
 * identities, declared input kinds and `requires:` all carry this repository's
 * authority, because that is what the import writes and what the registry will
 * hold. A preview naming the placeholder would link nowhere and gate on a
 * package the server never looks for. `id` is NOT rewritten: it addresses the
 * shipped closure, which is what the import door is called with. */
export function landedCatalog(item: CatalogItem, home: string): CatalogItem {
  if (item.tier !== "sample" || !home) return item
  const from = `${item.authority}/`
  const rehome = (s: string) =>
    s.startsWith(from) ? `${home}/${s.slice(from.length)}` : s
  const closure = item.closure
  return {
    ...item,
    authority: home,
    requires: item.requires?.map(rehome),
    inputs: item.inputs
      ? Object.fromEntries(
          Object.entries(item.inputs).map(([name, input]) => [
            name,
            { ...input, kind: rehome(input.kind) },
          ])
        )
      : undefined,
    closure: {
      ...closure,
      kinds: closure.kinds?.map(rehome),
      functions: closure.functions?.map(rehome),
      agents: closure.agents?.map(rehome),
      mappings: closure.mappings?.map(rehome),
      kindDescriptions: closure.kindDescriptions
        ? Object.fromEntries(
            Object.entries(closure.kindDescriptions).map(([k, v]) => [
              rehome(k),
              v,
            ])
          )
        : undefined,
      records: closure.records?.map((r) => ({ ...r, kind: rehome(r.kind) })),
    },
  }
}

/** One catalog entry by the id it has HERE, sharing the list's cache (the same
 * `["catalog"]` query, selected down) and rehomed the way it landed. Returns
 * undefined when this repository's bundle is not a shipped closure: a bundle
 * applied by hand has no catalog entry, and the caller falls back to what the
 * registry alone knows. */
export function catalogItemQueryOptions(id: string, home = "") {
  return queryOptions({
    queryKey: catalogQueryOptions.queryKey,
    queryFn: catalogQueryOptions.queryFn,
    staleTime: 60_000,
    select: (items: CatalogItem[]) => {
      const found = items.find((i) => landedId(i, home) === id)
      return found && landedCatalog(found, home)
    },
  })
}

/** Install a PROVIDER's closure into this repository, under the authority that
 * publishes it. Owner-gated and idempotent (re-installing is the bundle's own
 * upgrade semantics); the response is the installed bundle's computed status.
 * The catalog id is a package reference and carries a `/`, so it is
 * `%2F`-encoded as one path segment. */
export function installBundle(id: string): Promise<BundleStatus> {
  return request<BundleStatus>("POST", `${CATALOG}/${seg(id)}/install`)
}

/** Import a SAMPLE under this repository's own authority (decision record
 * 0048). The server rehomes the closure before admitting it, so the status it
 * answers with carries the LANDED id (`<your authority>/<package>`), not the
 * shipped one this call names. */
export function importBundle(id: string): Promise<BundleStatus> {
  return request<BundleStatus>("POST", `${CATALOG}/${seg(id)}/import`)
}

/** The door a catalog entry takes, by tier. */
export function takeBundle(item: {
  id: string
  tier: CatalogTier
}): Promise<BundleStatus> {
  return item.tier === "sample" ? importBundle(item.id) : installBundle(item.id)
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
 * client — the `GET /api/v1/oauth/callback` the host serves, on the
 * substrate's own origin. KNOWN CONSTANT, not derived. */
export const OAUTH_CALLBACK_URL = `${SUBSTRATE_ORIGIN}${rootPath("oauth", "callback")}`

export function oauthCallbackURL(): string {
  return OAUTH_CALLBACK_URL
}
