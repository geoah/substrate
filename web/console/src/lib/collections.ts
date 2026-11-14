/** The collections as a person meets them: one group per package that holds
 * the owner's records ("Training", "Health", "Tasks": the repository's own
 * packages, every other authority's that is neither a provider's nor the
 * substrate's machinery, and the substrate's observations and well-known
 * catalogues), one group per owner-data kind inside the machinery packages
 * ("Contacts", named by its plural, since its package is core) or one
 * named group for the ones a package lists together ("Assistant": the
 * owner's memories and instructions), one "From
 * <Provider>" group per provider package, and the substrate's own machinery
 * (core, llm, console) last. The package is the
 * group because it is the unit one publisher versions and ships together,
 * while one authority publishes unrelated packages (the substrate publishes
 * both health and labs, a repository holds every sample it imported). The
 * sidebar, the command menu, All data, Home and the crumbs all read this one
 * grouping, so a kind sits in the same place on every surface. */

import {
  PROVIDERS_AUTHORITY,
  providerInfo,
  providerOfKind,
  type ProviderInfo,
} from "@/lib/actor-identity"
import { CORE_AUTHORITY, LOCAL_AUTHORITY, splitKind } from "@/lib/api/http"
import {
  isMachineryKind,
  isSeededOwnerData,
  sharedOwnerDataGroup,
} from "@/lib/core-kinds"
import type { KindInfo } from "@/lib/api/types"
import { kindPurpose } from "@/lib/definition"
import { displayPlural, lowerFirst, packageDisplayName } from "@/lib/kind-names"

export type CollectionGroupKind = "package" | "provider" | "system"

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
  /** "Training", "From Google", "Substrate". A package whose word another
   * package group shares carries its authority too: "Training
   * (geoah.me)". */
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
    const packages =
      byAuthority.get(k.authority) ?? new Map<string, KindInfo[]>()
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

/** The groups in reading order: the packages by word, then each provider by
 * name, then the substrate's own. A group with no kinds is left out. The
 * repository's own package, under `~`, leads others that share its word. */
export function collectionGroups(kinds: KindInfo[]): CollectionGroup[] {
  const packages = new Map<string, KindInfo[]>()
  const system: KindInfo[] = []
  const providers = new Map<string, { info: ProviderInfo; kinds: KindInfo[] }>()
  for (const k of kinds) {
    const provider = providerOfKind(k.identity)
    if (provider) {
      const entry = providers.get(provider.key) ?? { info: provider, kinds: [] }
      providers.set(provider.key, entry)
      entry.kinds.push(k)
    } else if (isMachineryKind(k.identity)) system.push(k)
    else {
      // An owner-data kind of a machinery package is a group of its own: its
      // package is the substrate's, and "Core" names nothing a person keeps.
      // The ones a package lists together (the assistant's memories and
      // instructions) share their package's group, under its own name.
      const id =
        isSeededOwnerData(k.identity) && !sharedOwnerDataGroup(k.identity)
          ? k.identity
          : `${k.authority}/${k.package}`
      packages.set(id, [...(packages.get(id) ?? []), k])
    }
  }
  const named = [...packages.entries()]
    .map(([id, list]) => ({
      id,
      list,
      authority: list[0].authority,
      word: isSeededOwnerData(list[0].identity)
        ? (sharedOwnerDataGroup(list[0].identity) ?? displayPlural(list[0]))
        : packageDisplayName(list[0].package),
    }))
    .sort(
      (a, b) =>
        a.word.localeCompare(b.word) ||
        Number(b.authority === LOCAL_AUTHORITY) -
          Number(a.authority === LOCAL_AUTHORITY) ||
        a.authority.localeCompare(b.authority)
    )
  const words = named.map((p) => p.word)
  const shared = new Set(words.filter((w, i) => words.indexOf(w) !== i))
  const out: CollectionGroup[] = named.map(({ id, list, authority, word }) =>
    group(
      `group:package:${id}`,
      "package",
      shared.has(word) ? `${word} (${ownerWord(authority)})` : word,
      list
    )
  )
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

/** Where a kind sits, in words: its package ("Training"), "From Google",
 * "Substrate", or an owner-data kind's own group ("Contacts"). */
export function collectionSource(kind: string): string {
  const provider = providerOfKind(kind)
  if (provider) return `From ${provider.name}`
  if (isSeededOwnerData(kind))
    return sharedOwnerDataGroup(kind) ?? displayPlural(kind)
  const { pkg } = splitKind(kind)
  return isMachineryKind(kind) ? "Substrate" : packageDisplayName(pkg)
}

/** An authority as its page and crumb name it: "From providers",
 * "Substrate", the repository's own `~` as "Yours", and any other by its own
 * name, since it is the publisher of the packages under it and not a place
 * of its own. */
export function authorityTitle(authority: string): string {
  if (authority === PROVIDERS_AUTHORITY) return "From providers"
  if (authority === LOCAL_AUTHORITY) return "Yours"
  return authority === CORE_AUTHORITY ? "Substrate" : authority
}

/** Who a package belongs to beside its word, where two share one: "yours"
 * for the repository's own, else the authority that publishes it. */
function ownerWord(authority: string): string {
  return authority === LOCAL_AUTHORITY ? "yours" : authority
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
