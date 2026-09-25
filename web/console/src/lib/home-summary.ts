/** The lines Home's overview cards read, from the reads the other pages
 * already make. Pure, so the words are checked without a page. */

import { providerInfo, PROVIDERS_AUTHORITY } from "@/lib/actor-identity"
import type { BundleStatus } from "@/lib/api/types"

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
    return { big: "None added", sub: "Bring in your mail, calendar or contacts" }
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
