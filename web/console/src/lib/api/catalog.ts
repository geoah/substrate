/** The bundle CATALOG: the bundle closures shipped in the binary and the
 * packages authority documents publish — what the console lists and adds. The
 * catalog read flags each entry with whether THIS repository already has it,
 * so a page can fold added + not-added into one list; adding runs the same
 * admission path an explicit apply uses and answers with the status of the
 * bundle as it landed. */

import { queryOptions } from "@tanstack/react-query"

import { LOCAL_AUTHORITY, rootPath, request, seg } from "./http"
import type {
  BundleStatus,
  AddonInstallResult,
  BundleInstallRequest,
  CatalogItem,
  OperationalList,
  ShippedUpgrade,
} from "./types"

/** One shipped bundle closure plus whether this repository already has it. */
export type { CatalogItem } from "./types"

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

/** The boot upgrade's preview: one entry per package the binary ships and
 * seeds (core), which no catalog entry carries. The server computes it at read
 * against the running binary, so `blockers` here is what the boot logged when
 * it refused to move core, and the Registry can state it. */
export const shippedUpgradesQueryOptions = queryOptions({
  queryKey: ["vocabulary", "upgrade"],
  queryFn: async ({ signal }) => {
    const res = await request<OperationalList<ShippedUpgrade>>(
      "GET",
      rootPath("vocabulary", "upgrade"),
      undefined,
      { signal }
    )
    return res.items ?? []
  },
  staleTime: 60_000,
})

/** The id a catalog entry has once it lands: a provider keeps the id it
 * publishes, a SAMPLE is copied into the repository's local namespace as
 * `~/<package>`, the same in every repository (decision records 0048 and
 * 0259). This is the id the bundle STATUS carries once it lands. Two entries
 * can share it, so it finds a stored bundle's shipped closure only for a
 * copy with no origin stamp (heldCopyOf). */
export function landedId(
  item: Pick<CatalogItem, "id" | "keptBy" | "package">
): string {
  if (item.keptBy !== "repository") return item.id
  return `${LOCAL_AUTHORITY}/${item.package}`
}

/** The bundle this repository holds that IS catalog entry `item`, or
 * undefined: the server's rule (api `installedSet.copyOf`), so a row and the
 * entry's `installed` flag agree. A copy stamped with an origin belongs to the
 * entry it names and to no other, because two publishers' samples of one
 * package word both land at `~/<package>` and the id alone would hand one
 * copy to both. Only an unstamped copy (a provider, a sample installed
 * verbatim, or one copied before the stamp existed) is matched by id: the
 * landed id first, then the shipped one. */
export function heldCopyOf(
  item: Pick<CatalogItem, "id" | "keptBy" | "package">,
  statuses: readonly BundleStatus[]
): BundleStatus | undefined {
  const stamped = statuses.find((s) => s.origin === item.id)
  if (stamped) return stamped
  const at = (id: string) => statuses.find((s) => s.id === id)
  const copy = at(landedId(item)) ?? at(item.id)
  return copy?.origin ? undefined : copy
}

/** What can continue a DNS-style name: the boundary the server's rehoming
 * walk uses (vocabulary.RehomeAuthority), so the two agree on what counts as a
 * mention. */
const NAME_CHARACTER = /[A-Za-z0-9._-]/

/** Every whole-name mention of `from` in one string, rewritten to `to`. NOT a
 * prefix rewrite: a trigger's callable is a core kind reference with the
 * sample's own identity INSIDE it
 * (`substrate.reamde.dev/core/function/<sample authority>/pebble/ingest`), and
 * a preview that left the shipped authority in there would name a record this
 * repository will not have. A mention inside a longer name
 * (`notsamples.example.com`) is left alone, which is the server's rule too. */
export function rehomeAuthority(s: string, from: string, to: string): string {
  if (!to) return s
  return rewriteMentions(s, from, () => to)
}

/** A sample's mentions of its own authority `from` as the copy lands them
 * (`vocabulary.LocalizeText`): a mention followed by "/" names a kind, a
 * package or a record path and moves into `~`, except after "//", where it
 * is a URL's host. Any other mention is an actor text or prose, which the
 * copy spells with the repository's name; no identity a preview rewrites
 * holds one, so it is left as shipped. */
export function localizeAuthority(s: string, from: string): string {
  return rewriteMentions(s, from, (at, end) =>
    s[end] === "/" && !s.slice(0, at).endsWith("//") ? LOCAL_AUTHORITY : from
  )
}

/** Every whole-name mention of `from` in `s`, replaced by what `to` answers
 * for the mention at `at` up to `end`. */
function rewriteMentions(
  s: string,
  from: string,
  to: (at: number, end: number) => string
): string {
  if (!from || !s.includes(from)) return s
  let out = ""
  let i = 0
  for (;;) {
    const at = s.indexOf(from, i)
    if (at < 0) {
      out += s.slice(i)
      return out
    }
    const end = at + from.length
    const before = at > 0 ? s[at - 1] : ""
    const after = end < s.length ? s[end] : ""
    const whole =
      !NAME_CHARACTER.test(before || " ") && !NAME_CHARACTER.test(after || " ")
    out += s.slice(i, at) + (whole ? to(at, end) : from)
    i = end
  }
}

/** One catalog entry as it lands HERE: a sample's authority, closure
 * identities, declared input kinds and `requires:` all move into the local
 * namespace `~`, because that is what adding it writes and what the registry
 * will hold. A preview naming the placeholder would link nowhere and gate on
 * a package the server never looks for. `id` is NOT rewritten: it addresses
 * the shipped closure, which is what the add route is called with. */
export function landedCatalog(item: CatalogItem): CatalogItem {
  if (item.keptBy !== "repository") return item
  const rehome = (s: string) => localizeAuthority(s, item.authority)
  const closure = item.closure
  return {
    ...item,
    authority: LOCAL_AUTHORITY,
    requires: item.requires?.map(rehome),
    requiresAtLeast: item.requiresAtLeast
      ? Object.fromEntries(
          Object.entries(item.requiresAtLeast).map(([pkg, floor]) => [
            rehome(pkg),
            floor,
          ])
        )
      : undefined,
    // A suggested mapping's id and TARGET are this package's, so both land
    // in the local namespace; `from` is the provider's own kind and is left
    // exactly as published (decision record 0049).
    suggestedMappings: item.suggestedMappings?.map((m) => ({
      ...m,
      id: rehome(m.id),
      to: rehome(m.to),
    })),
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
      kinds: closure.kinds?.map(rehome) ?? null,
      traits: closure.traits?.map(rehome) ?? null,
      functions: closure.functions?.map(rehome) ?? null,
      agents: closure.agents?.map(rehome) ?? null,
      mappings: closure.mappings?.map(rehome) ?? null,
      // A trigger's id is a plain record id, so only the callable it names
      // carries the authority that moves.
      triggers: closure.triggers ?? null,
      triggerCallables: rekey(closure.triggerCallables, (k) => k, rehome),
      kindDescriptions: rekey(closure.kindDescriptions, rehome),
      kindPurposes: rekey(closure.kindPurposes, rehome),
      traitDescriptions: rekey(closure.traitDescriptions, rehome),
      functionDescriptions: rekey(closure.functionDescriptions, rehome),
      agentDescriptions: rekey(closure.agentDescriptions, rehome),
      records:
        closure.records?.map((r) => ({ ...r, kind: rehome(r.kind) })) ?? null,
    },
  }
}

/** One of the closure's keyed maps, rehomed: the keys are identities the
 * copy rewrites, and a value is prose unless it is an identity too. Absent
 * stays absent, so an older server's omission is not turned into an empty
 * map. */
function rekey(
  map: Record<string, string> | undefined,
  key: (s: string) => string,
  value: (s: string) => string = (s) => s
): Record<string, string> | undefined {
  if (!map) return undefined
  return Object.fromEntries(
    Object.entries(map).map(([k, v]) => [key(k), value(v)])
  )
}

/** ONE catalog entry, read fresh from the server rather than from the list's
 * cache. A confirmation names one plan, hashed over the records it rewrites
 * and the declarations it converts, so a chain of adds re-reads the preview of
 * each bundle right before it takes it: the step before it may have written
 * either, and the token the list was read with is then refused (engine
 * convert.go). The id is a package reference, so its slash is
 * one encoded path segment. */
export function fetchCatalogItem(id: string): Promise<CatalogItem> {
  return request<CatalogItem>("GET", `${CATALOG}/${seg(id)}`)
}

/** What adding a package answers: the status of the bundle as it landed,
 * with the add-ons it took along. */
export type BundleInstallResult = BundleStatus & {
  addons?: AddonInstallResult[]
}

/** Add one catalog package to this repository: `POST /catalog/{id}/add`.
 * Who keeps it decides where it lands (decision record 0258): a package its
 * publisher keeps lands under the authority that publishes it, and a SAMPLE
 * is copied into this repository's local namespace first, so the status
 * carries the LANDED id (`~/<package>`), not the one this call names.
 * Owner-gated and idempotent: adding again is the package's own upgrade, and
 * one over a copy this repository edited, or whose plan is lossy, needs the
 * confirmation its preview handed out (decision records 0067 and 0070). The
 * id is a package reference and carries a `/`, so it is `%2F`-encoded as one
 * path segment. */
export function addBundle(
  id: string,
  body?: BundleInstallRequest
): Promise<BundleInstallResult> {
  return request<BundleInstallResult>(
    "POST",
    `${CATALOG}/${seg(id)}/add`,
    takeBody(body)
  )
}

/** No body is the bare POST the route always took; a body with nothing in
 * it would be an empty object the strict decoder still has to read. */
function takeBody(
  body: BundleInstallRequest | undefined
): BundleInstallRequest | undefined {
  return body && (body.confirm || body.expect) ? body : undefined
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
