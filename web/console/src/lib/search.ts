/** The Search page's one preference: which arm of the ranked read to ask
 * for. The server's default is hybrid; the console's is LEXICAL, because a
 * word search costs nothing and answers on every repository, while the
 * semantic arm spends an embedding call per query and needs a provider row
 * to exist at all. The choice is the reader's and it sticks (localStorage),
 * so a reader who wants the fused ranking picks it once. */

import type { SearchMode } from "@/lib/api/records"
import type { RecordFilter } from "@/lib/api/types"

export type { SearchMode }

export const SEARCH_MODES: readonly SearchMode[] = [
  "lexical",
  "hybrid",
  "semantic",
]

export const DEFAULT_SEARCH_MODE: SearchMode = "lexical"

/** What each arm is, in the words the page shows beside the picker. */
export const SEARCH_MODE_LABEL: Record<SearchMode, string> = {
  lexical: "Words",
  hybrid: "Words + meaning",
  semantic: "Meaning",
}

/** What each arm does, in the reader's words. */
export const SEARCH_MODE_DESCRIPTION: Record<SearchMode, string> = {
  lexical: "Matches the words you typed. Instant.",
  hybrid:
    "Also finds records that mean the same thing, when a model is set up.",
  semantic: "Only by meaning. Needs a model, and each search calls it.",
}

/** How each arm ranks, for technical mode. */
export const SEARCH_MODE_DETAIL: Record<SearchMode, string> = {
  lexical:
    "Full-text search over every indexed text of a record, ranked by how well the words match.",
  hybrid:
    "Both arms fused by rank: a record near the top of both the word ranking and the embedding similarity ranks highest. Falls back to words alone when no embeddings provider is configured.",
  semantic:
    "Embedding similarity alone, over the properties that opted into embedding. Spends one embedding call per search.",
}

export function isSearchMode(v: unknown): v is SearchMode {
  return typeof v === "string" && (SEARCH_MODES as string[]).includes(v)
}

const MODE_KEY = "substrate.search.mode"

export function loadSearchMode(): SearchMode {
  try {
    const raw = localStorage.getItem(MODE_KEY)
    return isSearchMode(raw) ? raw : DEFAULT_SEARCH_MODE
  } catch {
    return DEFAULT_SEARCH_MODE
  }
}

/** Persist the arm; the default removes the entry, so a reader who comes
 * back to the default has nothing stored. */
export function saveSearchMode(mode: SearchMode): void {
  try {
    if (mode === DEFAULT_SEARCH_MODE) localStorage.removeItem(MODE_KEY)
    else localStorage.setItem(MODE_KEY, mode)
  } catch {
    // Storage denied — the URL still carries the mode for this view.
  }
}

/** The search grammar, as the page's hint states it: one line per form. */
export const SEARCH_GRAMMAR: readonly { example: string; means: string }[] = [
  { example: "rack layout", means: "every word, in any order" },
  { example: "lay*", means: "a word starting with lay" },
  { example: '"rack layout"', means: "the words together, in order" },
  { example: "-lunch", means: "without this word" },
  { example: "rack OR lunch", means: "either word" },
]

/** Everyday search, on the Search page and in ⌘K: what a person keeps and
 * its details (`filter.purposes`, decision 0136). The substrate's own
 * machinery (accounts, sync state, the vocabulary) is left out, as it is in
 * the sidebar. */
export const EVERYDAY_PURPOSES: NonNullable<RecordFilter["purposes"]> = [
  "primary",
  "supporting",
]

/** The technical-mode choice that widens a search to every purpose. */
export const INCLUDE_SYSTEM_LABEL = "Include the substrate’s own records"

/** The purposes a search sends; `undefined` asks for every purpose. A
 * collection picked by name is its own scope, whatever its purpose, and the
 * substrate's own records are searched only on request, in technical mode. */
export function searchPurposes(opts: {
  narrowed?: boolean
  technical: boolean
  includeSystem: boolean
}): RecordFilter["purposes"] {
  if (opts.narrowed) return undefined
  if (opts.technical && opts.includeSystem) return undefined
  return EVERYDAY_PURPOSES
}
