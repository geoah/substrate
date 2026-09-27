/** The collections as a person meets them: "Your data" (the repository's own
 * authority and every other authority that is neither a provider nor the
 * substrate's own), one "From <Provider>" group per provider package, and the
 * substrate's own machinery last. The sidebar, the command menu, All data and
 * Home all read this one grouping, so a kind sits in the same place on every
 * surface. */

import {
  PROVIDERS_AUTHORITY,
  providerInfo,
  providerOfKind,
  type ProviderInfo,
} from "@/lib/actor-identity"
import { CORE_AUTHORITY, splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"
import { displayPlural, lowerFirst, packageDisplayName } from "@/lib/kind-names"

export type CollectionGroupKind = "yours" | "provider" | "system"

export interface CollectionPackage {
  authority: string
  package: string
  /** `<authority>/<package>`. */
  identity: string
  /** Name-sorted by the reference's last segment. */
  kinds: KindInfo[]
}

export interface CollectionAuthority {
  authority: string
  packages: CollectionPackage[]
}

export interface CollectionGroup {
  /** Stable across renders and sessions: the key a collapsed group is stored
   * under. */
  id: string
  type: CollectionGroupKind
  /** "Your data", "From Google", "Substrate". */
  label: string
  provider?: ProviderInfo
  /** The technical tree under the group. */
  authorities: CollectionAuthority[]
  /** Every kind in the group, sorted by display plural. */
  kinds: KindInfo[]
  /** The kinds everyday navigation lists: the primary ones. */
  primary: KindInfo[]
  /** Supporting and internal kinds, which only technical mode lists. */
  hidden: KindInfo[]
}

const byPlural = (a: KindInfo, b: KindInfo) =>
  displayPlural(a).localeCompare(displayPlural(b))

const byReferenceName = (a: KindInfo, b: KindInfo) =>
  splitKind(a.identity).name.localeCompare(splitKind(b.identity).name)

function treeOf(kinds: KindInfo[]): CollectionAuthority[] {
  const byAuthority = new Map<string, Map<string, KindInfo[]>>()
  for (const k of kinds) {
    const packages = byAuthority.get(k.authority) ?? new Map()
    byAuthority.set(k.authority, packages)
    const list = packages.get(k.package) ?? []
    packages.set(k.package, list)
    list.push(k)
  }
  return [...byAuthority.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([authority, packages]) => ({
      authority,
      packages: [...packages.entries()]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([pkg, list]) => ({
          authority,
          package: pkg,
          identity: authority ? `${authority}/${pkg}` : pkg,
          kinds: [...list].sort(byReferenceName),
        })),
    }))
}

function group(
  id: string,
  type: CollectionGroupKind,
  label: string,
  kinds: KindInfo[],
  provider?: ProviderInfo
): CollectionGroup {
  const sorted = [...kinds].sort(byPlural)
  return {
    id,
    type,
    label,
    provider,
    authorities: treeOf(kinds),
    kinds: sorted,
    primary: sorted.filter((k) => kindPurpose(k) === "primary"),
    hidden: sorted.filter((k) => kindPurpose(k) !== "primary"),
  }
}

/** The groups in reading order: yours, then each provider by name, then the
 * substrate's own. A group with no kinds is left out. `home` is the
 * repository's own authority; it leads the tree inside "Your data". */
export function collectionGroups(
  kinds: KindInfo[],
  home = ""
): CollectionGroup[] {
  const yours: KindInfo[] = []
  const system: KindInfo[] = []
  const providers = new Map<string, { info: ProviderInfo; kinds: KindInfo[] }>()
  for (const k of kinds) {
    const provider = providerOfKind(k.identity)
    if (provider) {
      const entry = providers.get(provider.key) ?? { info: provider, kinds: [] }
      providers.set(provider.key, entry)
      entry.kinds.push(k)
    } else if (k.authority === CORE_AUTHORITY) system.push(k)
    else yours.push(k)
  }
  const out: CollectionGroup[] = []
  if (yours.length) {
    const g = group("group:yours", "yours", "Your data", yours)
    // The repository's own authority reads first; the rest alphabetically.
    g.authorities.sort(
      (a, b) =>
        Number(b.authority === home) - Number(a.authority === home) ||
        a.authority.localeCompare(b.authority)
    )
    out.push(g)
  }
  for (const { info, kinds: list } of [...providers.values()].sort((a, b) =>
    a.info.name.localeCompare(b.info.name)
  )) {
    out.push(
      group(
        `group:provider:${info.key}`,
        "provider",
        `From ${info.name}`,
        list,
        info
      )
    )
  }
  if (system.length) {
    out.push(group("group:system", "system", "Substrate", system))
  }
  return out
}

/** Where a kind sits, in words: "Your data", "From Google", "Substrate". */
export function collectionSource(kind: string): string {
  const provider = providerOfKind(kind)
  if (provider) return `From ${provider.name}`
  return splitKind(kind).authority === CORE_AUTHORITY
    ? "Substrate"
    : "Your data"
}

/** An authority as its page and crumb name it, in `collectionSource`'s
 * words: "Your data", "From providers", "Substrate". */
export function authorityTitle(authority: string): string {
  if (authority === PROVIDERS_AUTHORITY) return "From providers"
  return authority === CORE_AUTHORITY ? "Substrate" : "Your data"
}

/** A package as its page and crumb name it: a provider by its own name
 * ("Google"), any other package by its word ("Tasks package"). */
export function packageTitle(authority: string, pkg: string): string {
  if (authority === PROVIDERS_AUTHORITY) return providerInfo(pkg).name
  return `${packageDisplayName(pkg)} package`
}

/** Provider groups start folded; every other group starts open. The stored
 * `collapsed` list records a group only while it differs from its default,
 * so a provider group the reader opened is stored under its `:open` key. */
export function groupToggleKey(
  g: Pick<CollectionGroup, "id" | "type">
): string {
  return g.type === "provider" ? `${g.id}:open` : g.id
}

export function isGroupOpen(
  g: Pick<CollectionGroup, "id" | "type">,
  collapsed: readonly string[]
): boolean {
  const stored = collapsed.includes(groupToggleKey(g))
  return g.type === "provider" ? stored : !stored
}

/** A few hidden kinds by name, for "like email addresses and labels":
 * supporting kinds first, since they are the ones a person meets. */
export function hiddenExamples(g: CollectionGroup, count = 2): string {
  const names = [
    ...g.hidden.filter((k) => kindPurpose(k) === "supporting"),
    ...g.hidden.filter((k) => kindPurpose(k) !== "supporting"),
  ]
    .slice(0, count)
    .map((k) => lowerFirst(displayPlural(k)))
  if (names.length <= 1) return names[0] ?? ""
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}
