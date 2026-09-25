/** A kind's DISPLAY name — "Calendar event series", "People" — built from
 * the compound lowercase word its declaration names it by
 * (`names.singular`: `calendareventseries`, `person`).
 *
 * UI-only, and only ever a label: a display name is never sent to the API and
 * never stands where a kind is identified. The identifier is always the full
 * reference `<authority>/<package>/<name>` (record 0101). */

import { splitKind } from "@/lib/api/http"
import type { KindInfo } from "@/lib/api/types"

/** The words a kind name is split into. A name that does not split fully
 * into these reads as the raw name, capitalised — a wrong split is worse
 * than none. */
const WORDS = new Set([
  "account",
  "actor",
  "address",
  "agent",
  "app",
  "attachment",
  "authority",
  "blob",
  "block",
  "bot",
  "bridge",
  "bundle",
  "calendar",
  "chat",
  "code",
  "comment",
  "conduct",
  "config",
  "console",
  "contact",
  "conversation",
  "credential",
  "cycle",
  "data",
  "database",
  "digest",
  "document",
  "drive",
  "email",
  "event",
  "file",
  "function",
  "gmail",
  "group",
  "instruction",
  "interaction",
  "issue",
  "item",
  "key",
  "kind",
  "label",
  "license",
  "list",
  "log",
  "mapping",
  "merge",
  "message",
  "milestone",
  "note",
  "occurrence",
  "of",
  "organization",
  "package",
  "page",
  "patch",
  "person",
  "policy",
  "preference",
  "project",
  "property",
  "provider",
  "pull",
  "reaction",
  "reading",
  "record",
  "recording",
  "recovery",
  "repository",
  "request",
  "review",
  "run",
  "scratchpad",
  "secret",
  "series",
  "setting",
  "sleep",
  "source",
  "split",
  "state",
  "status",
  "sync",
  "task",
  "team",
  "thread",
  "token",
  "trait",
  "transcript",
  "trigger",
  "type",
  "user",
  "web",
  "workflow",
  "workout",
])

const LONGEST = Math.max(...[...WORDS].map((w) => w.length))

/** The fewest-words split of `name` into known words, the longer first word
 * winning a tie; undefined when no full split exists. */
export function splitWords(name: string): string[] | undefined {
  const memo = new Map<number, string[] | null>()
  const from = (i: number): string[] | null => {
    if (i === name.length) return []
    const cached = memo.get(i)
    if (cached !== undefined) return cached
    let best: string[] | null = null
    for (let len = Math.min(LONGEST, name.length - i); len > 0; len--) {
      const word = name.slice(i, i + len)
      if (!WORDS.has(word)) continue
      const rest = from(i + len)
      if (rest && (!best || rest.length + 1 < best.length)) {
        best = [word, ...rest]
      }
    }
    memo.set(i, best)
    return best
  }
  return from(0) ?? undefined
}

const IRREGULAR: Record<string, string> = {
  person: "people",
  child: "children",
  series: "series",
  data: "data",
}

/** One English word's plural. */
export function pluralWord(word: string): string {
  const lower = word.toLowerCase()
  const irregular = IRREGULAR[lower]
  if (irregular) return matchCase(word, irregular)
  if (/[^aeiou]y$/.test(lower)) return word.slice(0, -1) + "ies"
  if (/(s|x|z|ch|sh)$/.test(lower)) return word + "es"
  return word + "s"
}

function matchCase(original: string, word: string): string {
  return original[0] === original[0].toUpperCase() ? capitalise(word) : word
}

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

/** The kind's own lowercase name, from a registry entry or a reference. */
function nameOf(kind: KindInfo | string): string {
  if (typeof kind === "string") return splitKind(kind).name
  const names = kind.definition?.names as Record<string, unknown> | undefined
  const singular = names?.singular
  if (typeof singular === "string" && singular) return singular
  return kind.name || splitKind(kind.identity).name
}

function words(kind: KindInfo | string): string[] {
  const name = nameOf(kind)
  return splitWords(name) ?? [name]
}

/** "Calendar event series", "Gmail thread", "Person". */
export function displayName(kind: KindInfo | string): string {
  return capitalise(words(kind).join(" "))
}

/** "Calendar event series", "Gmail threads", "People". The last word is the
 * one that takes the plural, except in an "X of Y" name, where X does
 * ("Codes of conduct"). */
export function displayPlural(kind: KindInfo | string): string {
  const parts = words(kind)
  const of = parts.indexOf("of")
  const at = of > 0 ? of - 1 : parts.length - 1
  parts[at] = pluralWord(parts[at])
  return capitalise(parts.join(" "))
}
