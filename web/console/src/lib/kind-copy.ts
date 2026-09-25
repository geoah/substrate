/** A kind's description as an everyday reader gets it. Declarations describe
 * a kind for the developers and agents that write against it (a provider's
 * quote its API documentation); everyday mode shows a short line instead, and
 * technical mode the declaration's own words. */

import { PROVIDERS_AUTHORITY, providerInfo } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"
import { ACRONYMS, displayPlural, lowerFirst } from "@/lib/kind-names"

/** The longest everyday line, before it is cut at a word. */
export const EVERYDAY_MAX = 120

const KNOWN_CAPITALS = new Set(Object.values(ACRONYMS))

/** The first clause of a developer's description, as a sentence: cut at the
 * first ":", " — ", ";" or sentence end, code marks dropped, SHOUTED words
 * (emphasis in a declaration) lowered, and never longer than EVERYDAY_MAX. */
export function firstClause(text: string): string {
  const flat = text.replace(/`/g, "").replace(/\s+/g, " ").trim()
  const end = flat.search(/:\s|\s[—–-]\s|;|[.!?](\s|$)/)
  let clause = (end === -1 ? flat : flat.slice(0, end)).trim()
  clause = clause.replace(/\b[A-Z]{4,}\b/g, (w) =>
    KNOWN_CAPITALS.has(w) ? w : w.toLowerCase()
  )
  if (!clause) return ""
  if (clause.length > EVERYDAY_MAX) {
    const cut = clause.slice(0, EVERYDAY_MAX)
    const at = cut.lastIndexOf(" ")
    return `${(at > 0 ? cut.slice(0, at) : cut).replace(/[,\s]+$/, "")}…`
  }
  return /[.!?…]$/.test(clause) ? clause : `${clause}.`
}

/** "Your Google calendar events, copied in and kept up to date." for a
 * provider's kind; the first clause of the declaration's description for any
 * other; undefined when it has none. `description` stands in for a kind the
 * registry does not hold yet (a catalog closure's). */
export function everydayDescription(
  kind: KindInfo | string,
  description?: string
): string | undefined {
  const reference = typeof kind === "string" ? kind : kind.identity
  const { authority, pkg } = splitKind(reference)
  if (authority === PROVIDERS_AUTHORITY && pkg) {
    const plural = lowerFirst(displayPlural(kind))
    return `Your ${providerInfo(pkg).name} ${plural}, copied in and kept up to date.`
  }
  const text = description ?? (typeof kind === "string" ? "" : kind.description)
  return text ? firstClause(text) || undefined : undefined
}

/** The description a surface shows: the declaration's own with technical
 * details on, the everyday line otherwise. */
export function kindDescription(
  kind: KindInfo | string,
  technical: boolean,
  description?: string
): string | undefined {
  if (!technical) return everydayDescription(kind, description)
  const text =
    description ?? (typeof kind === "string" ? undefined : kind.description)
  return text || undefined
}
