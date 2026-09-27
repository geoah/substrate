/** Matching for the ⌘K palette. Pages and collections match when every word
 * typed starts a word of their name, so "lisb" never lands on "Drive files"
 * by picking its letters out one at a time; records go to the ranked read,
 * whose grammar only matches a word being typed when it ends in `*`. */

/** A text's words: lowercased, split on anything that is not a letter or a
 * digit. */
export function words(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
}

/** Whether every word typed starts some word of the text: "dri fi" matches
 * "Drive files", "fi" matches it too, "lisb" does not. Nothing typed matches
 * everything. */
export function matchesWordPrefixes(query: string, text: string): boolean {
  const typed = words(query)
  if (!typed.length) return true
  const have = words(text)
  return typed.every((t) => have.some((w) => w.startsWith(t)))
}

/** A typed query as the ranked read should run it while the reader is still
 * typing: the last word matches as a prefix ("prepare lisb" asks for
 * "prepare lisb*"). What the reader wrote in the grammar stays as written: a
 * trailing space ends the word, and a quoted phrase, an exclusion, an `OR`
 * or a word already ending in `*` is left alone. */
export function typeAheadQuery(query: string): string {
  const trimmed = query.trim()
  if (!trimmed || /\s$/.test(query)) return trimmed
  if ((trimmed.match(/"/g) ?? []).length % 2 === 1) return trimmed
  const last = trimmed.split(/\s+/).at(-1) ?? ""
  if (last === "OR" || !/^[\p{L}\p{N}]+$/u.test(last)) return trimmed
  return `${trimmed}*`
}
