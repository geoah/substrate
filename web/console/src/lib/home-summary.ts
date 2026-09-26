/** The lines Home's overview cards read, from the reads the other pages
 * already make. Pure, so the words are checked without a page. */

import {
  providerInfo,
  PROVIDERS_AUTHORITY,
  type ProviderInfo,
} from "@/lib/actor-identity"
import type { BundleStatus, KindInfo } from "@/lib/api/types"
import type { CollectionGroup } from "@/lib/collections"

export interface CardSummary {
  big: string
  sub: string
}

function andList(names: string[]): string {
  if (names.length <= 1) return names[0] ?? ""
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

export function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`
}

/** Where each provider stands, in a phrase: connected, waiting for an
 * account, not set up yet, or turned off. */
export function providerState(b: BundleStatus): string {
  const name = providerInfo(b.package).name
  if (!b.enabled) return `${name} turned off`
  if ((b.setup ?? []).length > 0) return `${name} not set up yet`
  if (b.accounts > 0) return `${name} connected`
  return `${name} needs an account`
}

export function providersSummary(statuses: BundleStatus[]): CardSummary {
  const providers = statuses.filter((b) => b.authority === PROVIDERS_AUTHORITY)
  if (!providers.length) {
    return {
      big: "None added",
      sub: "Bring in your mail, calendar or contacts",
    }
  }
  return {
    big: `${providers.length.toLocaleString()} added`,
    sub: providers.map(providerState).join(" · "),
  }
}

export function dataSummary(
  collections: number,
  providers: number
): CardSummary {
  return {
    big: plural(collections, "collection", "collections"),
    sub: providers
      ? `yours and from ${plural(providers, "provider", "providers")}`
      : "all yours",
  }
}

/** "substrate, notekeeper and 6 more". */
export function namesSummary(names: string[], shown = 2): string {
  if (names.length <= shown + 1) return andList(names)
  return `${names.slice(0, shown).join(", ")} and ${names.length - shown} more`
}

/** One heading of Home's collections: "Your data", or "From Google" with
 * the provider it names. */
export interface HomeSection {
  id: string
  label: string
  provider?: ProviderInfo
  kinds: KindInfo[]
}

/** Home's collections, grouped the way the sidebar groups them. Up to `cap`
 * cards: yours take at least `yoursAtLeast` places, the ones that hold
 * something first, and the providers' kinds fill what is left in the
 * sidebar's order. `held` says whether a kind holds any records. */
export function homeSections(
  groups: readonly CollectionGroup[],
  held: (kind: KindInfo) => boolean,
  cap = 9,
  yoursAtLeast = 6
): HomeSection[] {
  const yours = groups
    .filter((g) => g.type === "yours")
    .flatMap((g) => g.primary)
    .map((k, i) => ({ k, i, held: held(k) }))
    .sort((a, b) => Number(b.held) - Number(a.held) || a.i - b.i)
    .map(({ k }) => k)
  const providers = groups.filter((g) => g.type === "provider")
  const provided = providers.reduce((n, g) => n + g.primary.length, 0)
  const shownYours = yours.slice(
    0,
    Math.min(cap, Math.max(yoursAtLeast, cap - provided))
  )
  const out: HomeSection[] = []
  if (shownYours.length) {
    out.push({ id: "group:yours", label: "Your data", kinds: shownYours })
  }
  let left = cap - shownYours.length
  for (const g of providers) {
    if (left <= 0) break
    const kinds = g.primary.slice(0, left)
    left -= kinds.length
    if (kinds.length) {
      out.push({ id: g.id, label: g.label, provider: g.provider, kinds })
    }
  }
  return out
}
