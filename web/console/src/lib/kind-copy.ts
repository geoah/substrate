/** A kind's description as an everyday reader gets it. A shipped declaration
 * opens with one plain sentence saying what a record is to a person, and the
 * API and design notes developers and agents rely on follow it; everyday mode
 * shows that first sentence, and technical mode the whole description. */

import { PROVIDERS_AUTHORITY, providerInfo } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"
import { KEPT_CAPITALS, displayPlural, lowerFirst } from "@/lib/kind-names"

/** The longest everyday line, before it is cut at a word. */
export const EVERYDAY_MAX = 120

/** A description's first sentence, whitespace flattened: up to the first
 * sentence end followed by a space or the end of the text. */
export function firstSentence(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim()
  const end = flat.search(/[.!?](\s|$)/)
  return end === -1 ? flat : flat.slice(0, end + 1)
}

/** Whether a sentence reads as everyday words: short enough for one line,
 * and free of the marks of an API note — code spans, parentheses, dotted or
 * slashed names (`events.list`, `GET /v1/users`), `key=value`, a version
 * word (`v3`) or a SHOUTED word that is not a name spelled in capitals. */
export function plainSentence(sentence: string): boolean {
  if (!sentence || sentence.length > EVERYDAY_MAX) return false
  if (/[`()=/]/.test(sentence)) return false
  if (/\w\.\w/.test(sentence)) return false
  if (/\bv\d+\b/i.test(sentence)) return false
  const shouted = sentence.match(/\b[A-Z]{4,}\b/g) ?? []
  return shouted.every((w) => KEPT_CAPITALS.has(w))
}

/** The first clause of a developer's description, as a sentence: cut at the
 * first ":", " — ", ";" or sentence end, code marks dropped, SHOUTED words
 * (emphasis in a declaration) lowered, and never longer than EVERYDAY_MAX. */
export function firstClause(text: string): string {
  const flat = text.replace(/`/g, "").replace(/\s+/g, " ").trim()
  const end = flat.search(/:\s|\s[—–-]\s|;|[.!?](\s|$)/)
  let clause = (end === -1 ? flat : flat.slice(0, end)).trim()
  clause = clause.replace(/\b[A-Z]{4,}\b/g, (w) =>
    KEPT_CAPITALS.has(w) ? w : w.toLowerCase()
  )
  if (!clause) return ""
  if (clause.length > EVERYDAY_MAX) {
    const cut = clause.slice(0, EVERYDAY_MAX)
    const at = cut.lastIndexOf(" ")
    return `${(at > 0 ? cut.slice(0, at) : cut).replace(/[,\s]+$/, "")}…`
  }
  return /[.!?…]$/.test(clause) ? clause : `${clause}.`
}

/** The declaration's first sentence when it reads as everyday words. When it
 * does not (a declaration that still opens with API notes) a provider's kind
 * reads "Your Google calendar events, copied in and kept up to date." and any
 * other kind the first clause of its description. Undefined when there is
 * nothing to say. `description` stands in for a kind the registry does not
 * hold yet (a catalog closure's). */
export function everydayDescription(
  kind: KindInfo | string,
  description?: string
): string | undefined {
  const text = description ?? (typeof kind === "string" ? "" : kind.description)
  const first = text ? firstSentence(text) : ""
  if (plainSentence(first)) return first
  const reference = typeof kind === "string" ? kind : kind.identity
  const { authority, pkg } = splitKind(reference)
  if (authority === PROVIDERS_AUTHORITY && pkg) {
    const plural = lowerFirst(displayPlural(kind))
    return `Your ${providerInfo(pkg).name} ${plural}, copied in and kept up to date.`
  }
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
