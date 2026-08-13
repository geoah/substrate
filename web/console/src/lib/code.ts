/** Reading source in the console: the tinting query and the payload
 * prettifier. This module imports shiki's TYPES only — the highlighter itself
 * is pulled by a dynamic `import()` inside the query, so nothing here drags
 * the grammar bundle into the main chunk. */

import { useQuery } from "@tanstack/react-query"

import type { CodeLang } from "./shiki"

/** The highlighter, per (source, language). `staleTime: Infinity` because a
 * given text tints to the same tokens forever; the short `gcTime` is what keeps
 * a scrolled-past transcript's tokens from pinning memory. */
export function useCodeTokens(source: string, lang: CodeLang) {
  return useQuery({
    queryKey: ["code-tokens", lang, source],
    queryFn: async () => {
      const { tokenize } = await import("./shiki")
      return tokenize(source, lang)
    },
    staleTime: Infinity,
    gcTime: 60_000,
  })
}

/** JSON text as a reader wants it, and the honest answer when it is not JSON
 * at all: a tool result is whatever the function returned, so a plain string, a
 * stack trace or a truncated payload all reach this. `json: false` means the
 * text is rendered untinted rather than tinted as if it had parsed. */
export function prettyJSON(raw: string): { text: string; json: boolean } {
  const trimmed = raw.trim()
  if (!trimmed) return { text: "", json: false }
  try {
    return { text: JSON.stringify(JSON.parse(trimmed), null, 2), json: true }
  } catch {
    return { text: raw, json: false }
  }
}
