/** Reading source in the console: the lazy tinting hook and the payload
 * prettifier. This module imports shiki's TYPES only — the highlighter itself
 * is pulled by a dynamic `import()` inside the effect, so nothing here drags
 * the grammar bundle into the main chunk.
 *
 * The hook deliberately does NOT use react-query. Tinting is presentation, and
 * a block of source must render inside a plain `render()` — a unit test, or any
 * tree with no QueryClient above it — without the provider. Nothing is cached
 * across mounts either: the highlighter instance itself is memoized in
 * `shiki.ts`, so re-tinting a block is a call, not a download, and one uniform
 * "untinted first, colored a tick later" beats a cache that makes what the
 * reader sees depend on what some other block tinted earlier. */

import { useEffect, useState } from "react"

import type { CodeLang, CodeToken } from "./shiki"

/** The tinting for one block, or `undefined` until (or unless) it arrives.
 * Every caller renders the untinted text meanwhile, so nothing waits on it and
 * the layout never reflows — the line count is the source's, not shiki's. */
export function useCodeTokens(
  source: string,
  lang: CodeLang
): CodeToken[][] | undefined {
  const key = `${lang} ${source}`
  // The key rides WITH the tokens: a block whose source changed must not paint
  // the previous source's colors onto the new lines while the new tint is in
  // flight, and comparing the key is what makes that impossible.
  const [tinted, setTinted] = useState<{
    key: string
    tokens: CodeToken[][]
  } | null>(null)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const { tokenize } = await import("./shiki")
        const tokens = await tokenize(source, lang)
        if (!cancelled) setTinted({ key, tokens })
      } catch {
        // A grammar that fails to load leaves the text untinted, which is what
        // the reader sees before it lands anyway. Never an error state.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [key, source, lang])

  return tinted?.key === key ? tinted.tokens : undefined
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
