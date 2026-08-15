/** The KIND registry: the `kinds` collection of the core authority, read whole
 * (it is registry-shaped — tens of rows, changing when a bundle is imported,
 * not while a page is open) and folded into the sidebar's nav shape: every
 * authority at one flat level (repository authorities first, then the
 * substrate's own machinery), with no "System" grouping bucket.
 *
 * NOT the Registry PAGE. That surface (`/registry`, pages/registry.tsx) lists
 * bundles; this module is the vocabulary the console routes on, and it is
 * named for the collection it reads — `core.substrate.reamde.dev/kinds`, since the
 * entity→record rename retired `entitykinds`. */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, corePath, request, splitKind } from "./http"
import type { KindInfo, Page } from "./types"

const KINDS = "kinds"
/** One page comfortably above any real registry; the fetch still follows the
 * cursor if a substrate ever outgrows it. */
const REGISTRY_PAGE = 500

/** A declaration record → the flat `KindInfo` the console routes on. The
 * record's ID is the kind reference (`<authority>/<name>`); its properties
 * carry the rest. */
function kindFromRecord(item: Record<string, unknown>): KindInfo | undefined {
  const properties = (item?.properties ?? {}) as Record<string, unknown>
  const identity = String(item?.id ?? "")
  if (!identity) return undefined
  const { authority, name } = splitKind(identity)
  // THE PROPERTIES ARE THE DECLARATION. `definition` was the blob a declaration
  // row used to carry; a row's own properties are the declaration now, and the
  // blob is read only where a repository still holds one an older substrate
  // wrote.
  const legacy = properties.definition as Record<string, unknown> | undefined
  const definition = legacy ?? properties
  const names = (definition.names ?? {}) as Record<string, unknown>
  return {
    identity,
    name: String(names.singular ?? properties.name ?? name),
    authority: String(properties.authority ?? authority),
    // The declaration version is an incremental int64; 0 (absent) covers a
    // declaration written before the server versioned them.
    version: Number(properties.version) || 0,
    plural: String(names.plural ?? properties.plural ?? name),
    source: String(properties.source ?? "builtin"),
    // A STRING or nothing — this renders as prose, and `String()` would turn a
    // malformed declaration's object into "[object Object]" on the page.
    description:
      typeof definition.description === "string" ? definition.description : "",
    definition,
  }
}

/** The registry serves `kind` records; a bare flat `KindInfo` list is
 * also a legal shape of the same resource, so both normalize. */
export function normalizeKinds(payload: unknown): KindInfo[] {
  const raw = Array.isArray(payload)
    ? payload
    : ((payload as { kinds?: unknown[]; records?: unknown[] })?.kinds ??
      (payload as { records?: unknown[] })?.records ??
      [])
  const out: KindInfo[] = []
  for (const item of raw as Array<Record<string, unknown>>) {
    if (
      typeof item?.identity === "string" &&
      typeof item?.plural === "string"
    ) {
      out.push(item as unknown as KindInfo)
      continue
    }
    const kind = kindFromRecord(item)
    if (kind) out.push(kind)
  }
  return out
}

export async function fetchKinds(signal?: AbortSignal): Promise<KindInfo[]> {
  const out: KindInfo[] = []
  let after: string | undefined
  do {
    const q = new URLSearchParams({ first: String(REGISTRY_PAGE) })
    if (after) q.set("after", after)
    const page = await request<Page>(
      "GET",
      `${corePath(KINDS)}?${q}`,
      undefined,
      { signal }
    )
    out.push(...normalizeKinds(page))
    after = page.cursor
  } while (after)
  return out
}

export const kindsQueryOptions = queryOptions({
  queryKey: ["registry", "kinds"],
  queryFn: ({ signal }) => fetchKinds(signal),
  staleTime: 5 * 60_000,
})

/** One sidebar authority: its name and its kinds, name-sorted. */
export interface AuthorityNav {
  authority: string
  kinds: KindInfo[]
}

export interface KindNav {
  /** Every authority at ONE flat level — no "System" grouping bucket (owner
   * ruling: the nav lists all authorities flat, machinery included).
   * Repository-declared authorities sort first, then the substrate's own
   * machinery (core.substrate.reamde.dev and bundle-installed authorities), core leading;
   * that ordering is purely for reading order — there is no nesting between
   * them. A repository-local kind (empty authority) is grouped under "". */
  authorities: AuthorityNav[]
}

/** An authority is machinery-shaped when it is the core authority itself or
 * when every kind in it was installed by a bundle rather than declared as
 * schema. It orders the flat nav list, and the dashboard's Data zone reads it
 * to stay off the machinery's count probes. */
export function isMachineryAuthority(
  authority: string,
  kinds: KindInfo[]
): boolean {
  if (authority === CORE_AUTHORITY) return true
  return kinds.every((k) => k.source === "installed")
}

export function buildKindNav(kinds: KindInfo[]): KindNav {
  const byAuthority = new Map<string, KindInfo[]>()
  for (const k of kinds) {
    const list = byAuthority.get(k.authority)
    if (list) list.push(k)
    else byAuthority.set(k.authority, [k])
  }
  const schema: AuthorityNav[] = []
  const machinery: AuthorityNav[] = []
  for (const [authority, authorityKinds] of byAuthority) {
    authorityKinds.sort((a, b) => a.name.localeCompare(b.name))
    const nav = { authority, kinds: authorityKinds }
    if (isMachineryAuthority(authority, authorityKinds)) machinery.push(nav)
    else schema.push(nav)
  }
  const byName = (a: AuthorityNav, b: AuthorityNav) =>
    a.authority.localeCompare(b.authority)
  schema.sort(byName)
  // core first among machinery authorities — it is the substrate's own.
  machinery.sort(
    (a, b) =>
      Number(b.authority === CORE_AUTHORITY) -
        Number(a.authority === CORE_AUTHORITY) || byName(a, b)
  )
  return { authorities: [...schema, ...machinery] }
}

/** The login probe: the smallest authenticated read there is. Success means
 * the token is live; the caller stores it. Carries the candidate token
 * explicitly so a bad one never touches the stored session. */
export async function probeToken(token: string): Promise<void> {
  const q = new URLSearchParams({ first: "1" })
  await request<Page>("GET", `${corePath(KINDS)}?${q}`, undefined, { token })
}
