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

/** What markdown reads literally inside a link: a backslash escape before
 * ASCII punctuation, and a character reference (`&amp;`, `&#38;`, `&#x26;`).
 * One pass, so `\&amp;` stays the text `&amp;`. */
const UNESCAPE =
  /\\([!-/:-@[-`{-~])|&(?:#([0-9]{1,7})|#[xX]([0-9a-fA-F]{1,6})|([A-Za-z][A-Za-z0-9]{1,31}));/g

/** The named references models write; any other name stays as written. */
const NAMED = new Map([
  ["amp", "&"],
  ["lt", "<"],
  ["gt", ">"],
  ["quot", '"'],
  ["apos", "'"],
  ["nbsp", "\u00a0"],
])

function unescape(source: string): string {
  return source.replace(
    UNESCAPE,
    (whole, escaped?: string, dec?: string, hex?: string, name?: string) => {
      if (escaped !== undefined) return escaped
      if (name !== undefined) return NAMED.get(name) ?? whole
      const point = dec !== undefined ? Number(dec) : parseInt(hex ?? "", 16)
      const valid =
        point > 0 && point <= 0x10ffff && !(point >= 0xd800 && point <= 0xdfff)
      return valid ? String.fromCodePoint(point) : "\ufffd"
    }
  )
}

/** One link's run: a record, a web page, or, for any other target, the link
 * text with the target beside it, as text. The target is decoded first and
 * the scheme checked after, so a target is never an `href` unless it is
 * `http:` or `https:`, and `javascript:` in any spelling stays inert. */
function linkRun(label: string, written: string): Inline {
  const text = unescape(label)
  const target = unescape(written)
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

/** Markdown's whitespace inside a link: a space or a tab. */
function isSpace(c: string | undefined): boolean {
  return c === " " || c === "\t"
}

/** For each `[` of a line, the `]` that closes it: brackets balanced,
 * backslash escapes skipped. One pass over the line, so a reply full of
 * unmatched brackets costs what one without them does. */
function bracketPairs(line: string): Map<number, number> {
  const pairs = new Map<number, number>()
  const open: number[] = []
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (c === "\\") {
      i++
    } else if (c === "[") {
      open.push(i)
    } else if (c === "]") {
      const at = open.pop()
      if (at !== undefined) pairs.set(at, i)
    }
  }
  return pairs
}

/** How many characters one line's link targets may scan in total, past the
 * line itself. A target scan that fails can cover the rest of the line, and
 * a line of failed `](` openings would otherwise scan it once each: past
 * this, the rest of the line is read without links. */
function scanBudget(line: string): { left: number } {
  return { left: 4 * line.length + 4096 }
}

/** The link whose `[` is at `start` and whose `]` is at `close`:
 * `[text](target)` with an optional `"title"`, parentheses in the target
 * balanced. Undefined when the source there is not one. Every character the
 * target scan reads is charged to `work`. */
function linkAt(
  line: string,
  start: number,
  close: number,
  work: { left: number }
): { run: Inline; end: number } | undefined {
  if (line[close + 1] !== "(" || work.left <= 0) return undefined
  let i = close + 2
  const from = i
  const done = (to: number) => {
    work.left -= to - from
    return work.left > 0
  }
  while (isSpace(line[i])) i++
  let target: string
  if (line[i] === "<") {
    const end = line.indexOf(">", i + 1)
    if (!done(end < 0 ? line.length : end) || end < 0) return undefined
    target = line.slice(i + 1, end)
    i = end + 1
  } else {
    let parens = 0
    const begin = i
    for (; i < line.length; i++) {
      const c = line[i]
      if (c === "\\") i++
      else if (isSpace(c)) break
      else if (c === "(") parens++
      else if (c === ")" && parens-- === 0) break
    }
    if (!done(i)) return undefined
    target = line.slice(begin, Math.min(i, line.length))
  }
  while (isSpace(line[i])) i++
  const quote = line[i]
  if (quote === '"' || quote === "'") {
    // The closing quote is the first one no backslash escapes.
    let end = i + 1
    while (end < line.length && line[end] !== quote) {
      end += line[end] === "\\" ? 2 : 1
    }
    if (!done(end) || end >= line.length) return undefined
    i = end + 1
    while (isSpace(line[i])) i++
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
  const pairs = bracketPairs(line)
  const work = scanBudget(line)
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
    } else if (c === "[" && pairs.has(i)) {
      const link = linkAt(line, i, pairs.get(i) ?? i, work)
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
