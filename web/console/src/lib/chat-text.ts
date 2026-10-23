/** An agent's reply as blocks and inline runs: the small markdown subset
 * models actually write (paragraphs, bullet and numbered lists, headings,
 * fenced code, `code`, **bold** and [links](…)), plus record paths, which the
 * agent is told to name records by and which render as the record itself.
 *
 * Deliberately a subset and deliberately inert: the text is model-authored,
 * so nothing here produces HTML — the component renders runs as elements,
 * and anything this does not recognise stays the text it was. */

import { splitRecordPath } from "@/lib/record-path"

export type Inline =
  | { type: "text"; text: string }
  | { type: "strong"; runs: Inline[] }
  | { type: "code"; text: string }
  | { type: "record"; kind: string; id: string; text: string }
  /** A web page: `href` is always an `http:` or `https:` URL. */
  | { type: "link"; href: string; text: string }

export type Block =
  | { type: "paragraph"; lines: Inline[][] }
  | { type: "heading"; runs: Inline[] }
  | { type: "list"; ordered: boolean; items: Inline[][] }
  | { type: "code"; text: string }

/** A record path in prose: a dotted authority, a package, a kind name and an
 * id. The lookbehind keeps a URL's path from reading as one. */
const RECORD_PATH =
  /(?<![\w/.:@-])([a-z0-9-]+(?:\.[a-z0-9-]+)+)\/([a-z][a-z0-9]*)\/([a-z][a-z0-9]*)\/([A-Za-z0-9][A-Za-z0-9_~.-]*)/g

/** The same grammar, anchored: a whole string that is one record path. */
const ONE_PATH =
  /^([a-z0-9-]+(?:\.[a-z0-9-]+)+)\/([a-z][a-z0-9]*)\/([a-z][a-z0-9]*)\/([A-Za-z0-9][A-Za-z0-9_~.-]*?)\.*$/

/** The whole string as one record path, or undefined. */
export function recordPathOf(
  text: string
): { kind: string; id: string } | undefined {
  const match = ONE_PATH.exec(text.trim())
  if (!match) return undefined
  return { kind: `${match[1]}/${match[2]}/${match[3]}`, id: match[4] }
}

/** Plain text with its record paths lifted out. */
function withRecords(text: string): Inline[] {
  const out: Inline[] = []
  let at = 0
  RECORD_PATH.lastIndex = 0
  for (let m = RECORD_PATH.exec(text); m; m = RECORD_PATH.exec(text)) {
    // A sentence's full stop is not part of the id.
    const id = m[4].replace(/\.+$/, "")
    const kind = `${m[1]}/${m[2]}/${m[3]}`
    const path = `${kind}/${id}`
    if (m.index > at) out.push({ type: "text", text: text.slice(at, m.index) })
    out.push({ type: "record", kind, id, text: path })
    at = m.index + path.length
    RECORD_PATH.lastIndex = at
  }
  if (at < text.length) out.push({ type: "text", text: text.slice(at) })
  return out
}

/** The record-link schemes: `substrate://`, the form the console's own
 * editor writes (decision record 0137), and `ref:`, which agents write too. */
const RECORD_SCHEMES = ["substrate://", "ref:"]

/** A markdown backslash escape: `\` before ASCII punctuation. */
const ESCAPE = /\\([!-/:-@[-`{-~])/g

/** One link's run: a record, a web page, or, for any other target, the link
 * text with the target beside it, as text. A target is never an `href`
 * unless it is `http:` or `https:`, so `javascript:` and the rest stay
 * inert. */
function linkRun(label: string, written: string): Inline {
  const text = label.replace(ESCAPE, "$1")
  const target = written.replace(ESCAPE, "$1")
  const lower = target.toLowerCase()
  for (const scheme of RECORD_SCHEMES) {
    if (!lower.startsWith(scheme)) continue
    const hit = splitRecordPath(target.slice(scheme.length))
    if (hit) return { type: "record", ...hit, text: text || hit.id }
  }
  if (/^https?:\/\//.test(lower)) {
    try {
      const url = new URL(target)
      if (url.protocol === "http:" || url.protocol === "https:") {
        return { type: "link", href: url.href, text: text || target }
      }
    } catch {
      // Not a URL after all: shown as text below.
    }
  }
  return {
    type: "text",
    text: !text || text === target ? target : `${text} (${target})`,
  }
}

/** The link starting at `start`, which holds `[`: `[text](target)` with an
 * optional `"title"`, brackets in the text balanced or escaped, parentheses
 * in the target balanced. Undefined when the source there is not one. */
function linkAt(
  line: string,
  start: number
): { run: Inline; end: number } | undefined {
  let depth = 0
  let close = -1
  for (let i = start; i < line.length; i++) {
    const c = line[i]
    if (c === "\\") {
      i++
    } else if (c === "[") {
      depth++
    } else if (c === "]" && --depth === 0) {
      close = i
      break
    }
  }
  if (close < 0 || line[close + 1] !== "(") return undefined
  let i = close + 2
  while (line[i] === " ") i++
  let target: string
  if (line[i] === "<") {
    const end = line.indexOf(">", i + 1)
    if (end < 0) return undefined
    target = line.slice(i + 1, end)
    i = end + 1
  } else {
    let parens = 0
    const from = i
    for (; i < line.length; i++) {
      const c = line[i]
      if (c === "\\") i++
      else if (c === " ") break
      else if (c === "(") parens++
      else if (c === ")" && parens-- === 0) break
    }
    target = line.slice(from, Math.min(i, line.length))
  }
  while (line[i] === " ") i++
  const quote = line[i]
  if (quote === '"' || quote === "'") {
    const end = line.indexOf(quote, i + 1)
    if (end < 0) return undefined
    i = end + 1
    while (line[i] === " ") i++
  }
  if (line[i] !== ")" || !target) return undefined
  return { run: linkRun(line.slice(start + 1, close), target), end: i + 1 }
}

/** One line's inline runs. */
export function inlineRuns(line: string): Inline[] {
  return runsOf(line, false)
}

/** Inside bold, links, code and record paths are read; bold does not nest. */
function runsOf(line: string, inBold: boolean): Inline[] {
  const out: Inline[] = []
  let plain = 0
  const take = (run: Inline, from: number) => {
    if (from > plain) out.push(...withRecords(line.slice(plain, from)))
    out.push(run)
  }
  let i = 0
  while (i < line.length) {
    const c = line[i]
    if (c === "`") {
      const end = line.indexOf("`", i + 1)
      if (end > i + 1) {
        const text = line.slice(i + 1, end)
        const path = recordPathOf(text)
        take(
          path ? { type: "record", ...path, text } : { type: "code", text },
          i
        )
        i = plain = end + 1
        continue
      }
    } else if (c === "*" && line[i + 1] === "*" && !inBold) {
      const end = line.indexOf("*", i + 2)
      if (end > i + 2 && line[end + 1] === "*") {
        take({ type: "strong", runs: runsOf(line.slice(i + 2, end), true) }, i)
        i = plain = end + 2
        continue
      }
    } else if (c === "[") {
      const link = linkAt(line, i)
      if (link) {
        take(link.run, i)
        i = plain = link.end
        continue
      }
    }
    i++
  }
  if (plain < line.length) out.push(...withRecords(line.slice(plain)))
  return out
}

const BULLET = /^\s*[-*•]\s+(.*)$/
const NUMBERED = /^\s*\d+[.)]\s+(.*)$/
const HEADING = /^\s*#{1,6}\s+(.*)$/

/** The reply as blocks. */
export function chatBlocks(text: string): Block[] {
  const blocks: Block[] = []
  const lines = text.replace(/\r\n/g, "\n").split("\n")
  let paragraph: string[] = []
  let list: { ordered: boolean; items: string[] } | undefined

  const flush = () => {
    if (paragraph.length) {
      blocks.push({ type: "paragraph", lines: paragraph.map(inlineRuns) })
      paragraph = []
    }
    if (list) {
      blocks.push({
        type: "list",
        ordered: list.ordered,
        items: list.items.map(inlineRuns),
      })
      list = undefined
    }
  }

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.trim().startsWith("```")) {
      flush()
      const body: string[] = []
      for (i++; i < lines.length && !lines[i].trim().startsWith("```"); i++) {
        body.push(lines[i])
      }
      blocks.push({ type: "code", text: body.join("\n") })
      continue
    }
    if (!line.trim()) {
      flush()
      continue
    }
    const heading = HEADING.exec(line)
    if (heading) {
      flush()
      blocks.push({ type: "heading", runs: inlineRuns(heading[1]) })
      continue
    }
    const bullet = BULLET.exec(line)
    const numbered = bullet ? undefined : NUMBERED.exec(line)
    const item = bullet ?? numbered
    if (item) {
      const ordered = Boolean(numbered)
      if (paragraph.length || (list && list.ordered !== ordered)) flush()
      list ??= { ordered, items: [] }
      list.items.push(item[1])
      continue
    }
    if (list) {
      // An indented continuation belongs to the item above it.
      if (/^\s+/.test(line) && list.items.length) {
        list.items[list.items.length - 1] += ` ${line.trim()}`
        continue
      }
      flush()
    }
    paragraph.push(line)
  }
  flush()
  return blocks
}
