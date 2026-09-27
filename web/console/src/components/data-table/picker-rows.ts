/** What the reference picker lists, as a pure function of what it holds: the
 * chosen ids, the collection's first page and the server's search. */

import type { RecordOption } from "@/lib/identities"

/** Whether a row answers what was typed, on the page already in hand: the
 * title, or the id, holds the text. The server's search is word-based, so an
 * email-shaped title or a fragment of one is found here even where the index
 * would not find it. */
export function localMatch(option: RecordOption, typed: string): boolean {
  const needle = typed.trim().toLowerCase()
  if (!needle) return true
  return (
    option.title.toLowerCase().includes(needle) ||
    option.value.toLowerCase().includes(needle)
  )
}

/** The rows the picker lists: the chosen records first, whatever else is
 * shown (unchecking one must stay one click away while a search shows
 * something else), then the page's own matches, then what the server's search
 * found that the page did not carry. */
export function pickerRows(
  selected: readonly string[],
  chosenTitles: ReadonlyMap<string, string>,
  page: readonly RecordOption[],
  found: readonly RecordOption[],
  typed: string
): RecordOption[] {
  const byId = new Map<string, RecordOption>()
  for (const o of [...found, ...page]) byId.set(o.value, o)
  const chosen = new Set(selected)
  const out: RecordOption[] = selected.map(
    (id) =>
      byId.get(id) ?? {
        value: id,
        title: chosenTitles.get(id) ?? "",
        description: "",
      }
  )
  const seen = new Set(chosen)
  for (const o of page) {
    if (seen.has(o.value) || !localMatch(o, typed)) continue
    seen.add(o.value)
    out.push(o)
  }
  for (const o of found) {
    if (seen.has(o.value)) continue
    seen.add(o.value)
    out.push(o)
  }
  return out
}
