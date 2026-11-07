/** The result shapes the console draws itself. A function tool's output is
 * matched against them in order: a list of web links becomes source pills,
 * a page read becomes the page's pill, and the call is said in words. The
 * match is on the shape a function returns, never on its name, so the
 * console names no provider (decision record 0166) and every function that
 * returns a known shape, a shipped provider's or the owner's own, draws the
 * same way. Adding one is a reader in SHAPES and, for a new shape, a case in
 * ToolResult and its view (components/agent/tool-views.tsx).
 *
 * What is read here a web page or a model chose, so nothing is trusted: a
 * link is kept only when it is an `http:` or `https:` URL, and an output
 * that matches no shape reads as nothing, which leaves the generic detail to
 * show it. */

import type { ToolCallView } from "@/lib/api/transcript"

/** One link a search found. */
export interface WebHit {
  title: string
  url: string
  snippet?: string
}

/** One page read. */
export interface WebPage {
  url: string
  title?: string
  /** How much of the page came back, in characters. */
  characters: number
}

/** A known shape, read. */
export type ToolResult =
  { type: "web-hits"; hits: WebHit[] } | { type: "web-page"; page: WebPage }

/** The readers, in the order they are tried. */
const SHAPES: ((output: Record<string, unknown>) => ToolResult | undefined)[] =
  [
    // A list of links: `results` (or `hits`) holding objects with a `url`.
    (output) => {
      const list = Array.isArray(output.results)
        ? output.results
        : Array.isArray(output.hits)
          ? output.hits
          : undefined
      if (!list) return undefined
      const hits: WebHit[] = []
      for (const item of list) {
        const hit = object(item)
        const url = webURL(hit.url)
        if (!url) continue
        const snippet = text(hit.snippet) || text(hit.description)
        hits.push({
          title: text(hit.title) || webHost(url),
          url,
          ...(snippet && { snippet }),
        })
      }
      // A list whose items are not links is some other list.
      return hits.length || list.length === 0
        ? { type: "web-hits", hits }
        : undefined
    },
    // One page: a `url` with the page's `content` or `title` beside it.
    (output) => {
      const url = webURL(output.url)
      if (
        !url ||
        (typeof output.content !== "string" && typeof output.title !== "string")
      )
        return undefined
      const title = text(output.title)
      return {
        type: "web-page",
        page: {
          url,
          ...(title && { title }),
          characters:
            typeof output.content === "string" ? output.content.length : 0,
        },
      }
    },
  ]

/** A function tool's settled result in a known shape, read from the
 * `{output, effects}` its function answered; undefined while it runs, when
 * it failed, or for an output of no known shape. */
export function toolResult(call: ToolCallView): ToolResult | undefined {
  if (call.ok !== true) return undefined
  const output = object(object(parse(call.output)).output)
  for (const read of SHAPES) {
    const result = read(output)
    if (result) return result
  }
  return undefined
}

/** A function tool's call in words from the shape of what it returned and
 * what it was asked: "Searched for “…”, 5 results", "Read bbc.co.uk".
 * Undefined when neither says anything a reader needs. */
export function toolResultSummary(call: ToolCallView): string | undefined {
  const query = text(object(parse(call.arguments)).query)
  const result = toolResult(call)
  if (result?.type === "web-page") return `Read ${webHost(result.page.url)}`
  if (result?.type === "web-hits") {
    const n = result.hits.length
    const found = n === 1 ? "1 result" : `${n} results`
    return query ? `Searched for “${query}”, ${found}` : `Found ${found}`
  }
  return query && call.ok !== false ? `Searched for “${query}”` : undefined
}

/** The address a link shows: its host, without a leading `www.`. */
export function webHost(url: string): string {
  try {
    return new URL(url).host.replace(/^www\./, "")
  } catch {
    return url
  }
}

/** A value as a web address the console may link: an absolute `http:` or
 * `https:` URL, normalised, else undefined. */
export function webURL(value: unknown): string | undefined {
  if (typeof value !== "string" || !value.trim()) return undefined
  try {
    const url = new URL(value.trim())
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

function parse(raw: string | undefined): unknown {
  if (!raw) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}
