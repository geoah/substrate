// MyChart's HTML (letters, notes, imaging reports, messages) as markdown,
// read from a tokenizer's events in the order the markup writes them, as an
// HTML parser that repairs nothing reads it: no implied closes, no moved
// nodes (apps/healthspan/docs/importers/06-mychart.md, HTML as markdown).

import { strip } from "../shared"

/** One event of the markup, in the order it is written. */
export type HtmlToken =
  | { type: "start"; name: string; selfClosing: boolean }
  | { type: "end"; name: string }
  | { type: "text"; text: string }

// ---- character references ----

/** The Latin-1 names, U+00A0 to U+00FF in order: each may also be written
 * without its semicolon. */
const LATIN1 = (
  "nbsp iexcl cent pound curren yen brvbar sect uml copy ordf laquo not shy " +
  "reg macr deg plusmn sup2 sup3 acute micro para middot cedil sup1 ordm " +
  "raquo frac14 frac12 frac34 iquest Agrave Aacute Acirc Atilde Auml Aring " +
  "AElig Ccedil Egrave Eacute Ecirc Euml Igrave Iacute Icirc Iuml ETH Ntilde " +
  "Ograve Oacute Ocirc Otilde Ouml times Oslash Ugrave Uacute Ucirc Uuml " +
  "Yacute THORN szlig agrave aacute acirc atilde auml aring aelig ccedil " +
  "egrave eacute ecirc euml igrave iacute icirc iuml eth ntilde ograve " +
  "oacute ocirc otilde ouml divide oslash ugrave uacute ucirc uuml yacute " +
  "thorn yuml"
).split(" ")

/** The other names read with their semicolon only. */
const NAMED: Readonly<Record<string, number>> = {
  apos: 0x27,
  OElig: 0x152,
  oelig: 0x153,
  Scaron: 0x160,
  scaron: 0x161,
  Yuml: 0x178,
  fnof: 0x192,
  circ: 0x2c6,
  tilde: 0x2dc,
  ensp: 0x2002,
  emsp: 0x2003,
  thinsp: 0x2009,
  zwnj: 0x200c,
  zwj: 0x200d,
  lrm: 0x200e,
  rlm: 0x200f,
  ndash: 0x2013,
  mdash: 0x2014,
  lsquo: 0x2018,
  rsquo: 0x2019,
  sbquo: 0x201a,
  ldquo: 0x201c,
  rdquo: 0x201d,
  bdquo: 0x201e,
  dagger: 0x2020,
  Dagger: 0x2021,
  bull: 0x2022,
  hellip: 0x2026,
  permil: 0x2030,
  prime: 0x2032,
  Prime: 0x2033,
  lsaquo: 0x2039,
  rsaquo: 0x203a,
  oline: 0x203e,
  frasl: 0x2044,
  euro: 0x20ac,
  trade: 0x2122,
  larr: 0x2190,
  uarr: 0x2191,
  rarr: 0x2192,
  darr: 0x2193,
  harr: 0x2194,
  minus: 0x2212,
  lowast: 0x2217,
  radic: 0x221a,
  infin: 0x221e,
  asymp: 0x2248,
  ne: 0x2260,
  equiv: 0x2261,
  le: 0x2264,
  ge: 0x2265,
  loz: 0x25ca,
  check: 0x2713,
}

/** Every name the table reads, with `;` and, for the names an HTML parser
 * also takes without it, without. */
const ENTITIES: ReadonlyMap<string, string> = (() => {
  const m = new Map<string, string>()
  const legacy = (name: string, cp: number) => {
    const c = String.fromCodePoint(cp)
    m.set(`${name};`, c)
    m.set(name, c)
  }
  LATIN1.forEach((name, i) => legacy(name, 0xa0 + i))
  for (const [name, cp] of [
    ["amp", 0x26],
    ["lt", 0x3c],
    ["gt", 0x3e],
    ["quot", 0x22],
    ["AMP", 0x26],
    ["LT", 0x3c],
    ["GT", 0x3e],
    ["QUOT", 0x22],
    ["COPY", 0xa9],
    ["REG", 0xae],
  ] as const)
    legacy(name, cp)
  for (const [name, cp] of Object.entries(NAMED))
    m.set(`${name};`, String.fromCodePoint(cp))
  return m
})()

const WINDOWS_1252 = new TextDecoder("windows-1252")

/** Whether a code point is one a numeric reference may not name and
 * which is dropped: controls, noncharacters. */
function dropped(n: number): boolean {
  return (
    (n >= 0x1 && n <= 0x8) ||
    n === 0xb ||
    (n >= 0xe && n <= 0x1f) ||
    (n >= 0x7f && n <= 0x9f) ||
    (n >= 0xfdd0 && n <= 0xfdef) ||
    (n & 0xfffe) === 0xfffe
  )
}

function numeric(n: number): string {
  if (n === 0) return "\ufffd"
  if (n === 0x0d) return "\r"
  if (n >= 0x80 && n <= 0x9f) return WINDOWS_1252.decode(Uint8Array.of(n))
  if ((n >= 0xd800 && n <= 0xdfff) || n > 0x10ffff) return "\ufffd"
  if (dropped(n)) return ""
  return String.fromCodePoint(n)
}

const REFERENCE = /&(#[0-9]+;?|#[xX][0-9a-fA-F]+;?|[^\t\n\f <&#;]{1,32};?)/g

/** Text with its character references read as an HTML parser reads them:
 * numeric references (a control or a surrogate as the parser replaces
 * it), and the named references of the table, the Latin-1 ones also
 * without their semicolon and as the longest name a run of letters starts
 * with. A name outside the table is kept as written. */
export function unescapeHtml(text: string): string {
  if (!text.includes("&")) return text
  return text.replace(REFERENCE, (whole, s: string) => {
    if (s.startsWith("#")) {
      const digits = s.replace(/;$/, "")
      const n =
        digits[1] === "x" || digits[1] === "X"
          ? parseInt(digits.slice(2), 16)
          : parseInt(digits.slice(1), 10)
      return numeric(n)
    }
    const known = ENTITIES.get(s)
    if (known !== undefined) return known
    for (let x = s.length - 1; x > 1; x--) {
      const prefix = ENTITIES.get(s.slice(0, x))
      if (prefix !== undefined) return prefix + s.slice(x)
    }
    return whole
  })
}

// ---- the tokenizer ----

const NAME_START = /[a-zA-Z]/
const NAME = /[a-zA-Z][^\t\n\r\f />]*/y
/** The elements whose content is raw text up to their end tag. */
const RAW = new Set(["script", "style"])

/** The markup's events in order: start tags (names lower case), end tags
 * and text with its references read. Comments, declarations and
 * processing instructions are dropped; a `<` that starts none of them is
 * text. */
export function tokenize(html: string): HtmlToken[] {
  const out: HtmlToken[] = []
  let text = ""
  const flushText = () => {
    if (text) out.push({ type: "text", text: unescapeHtml(text) })
    text = ""
  }
  const n = html.length
  let i = 0
  while (i < n) {
    const lt = html.indexOf("<", i)
    if (lt < 0) {
      text += html.slice(i)
      break
    }
    text += html.slice(i, lt)
    i = lt
    const next = html[i + 1] ?? ""
    if (NAME_START.test(next)) {
      NAME.lastIndex = i + 1
      const m = NAME.exec(html)!
      const name = m[0].toLowerCase()
      const end = tagEnd(html, NAME.lastIndex)
      if (end < 0) {
        text += html.slice(i)
        break
      }
      flushText()
      const selfClosing = html[end - 1] === "/"
      out.push({ type: "start", name, selfClosing })
      i = end + 1
      if (selfClosing) out.push({ type: "end", name })
      else if (RAW.has(name)) {
        const close = new RegExp(`</${name}[\\s/>]`, "ig")
        close.lastIndex = i
        const c = close.exec(html)
        const stop = c ? c.index : n
        if (stop > i) out.push({ type: "text", text: html.slice(i, stop) })
        i = stop
      }
      continue
    }
    if (next === "/") {
      let j = i + 2
      while (j < n && /\s/.test(html[j])) j++
      const gt = html.indexOf(">", j)
      if (gt < 0) {
        text += html.slice(i)
        break
      }
      flushText()
      if (NAME_START.test(html[j] ?? "")) {
        NAME.lastIndex = j
        const m = NAME.exec(html)!
        out.push({ type: "end", name: m[0].toLowerCase() })
      }
      i = gt + 1
      continue
    }
    if (html.startsWith("<!--", i)) {
      flushText()
      const close = /--\s*>/g
      close.lastIndex = i + 4
      const c = close.exec(html)
      i = c ? c.index + c[0].length : n
      continue
    }
    if (next === "!" || next === "?") {
      flushText()
      const gt = html.indexOf(">", i + 2)
      i = gt < 0 ? n : gt + 1
      continue
    }
    text += "<"
    i += 1
  }
  flushText()
  return out
}

/** Where a start tag's `>` is, from just after its name: attribute values
 * in quotes may hold a `>`. -1 where the tag never closes. */
function tagEnd(html: string, from: number): number {
  let quote: string | undefined
  let afterEquals = false
  for (let j = from; j < html.length; j++) {
    const c = html[j]
    if (quote) {
      if (c === quote) quote = undefined
      continue
    }
    if (c === ">") return j
    if ((c === '"' || c === "'") && afterEquals) {
      quote = c
      afterEquals = false
      continue
    }
    if (c === "=") afterEquals = true
    else if (!/\s/.test(c)) afterEquals = false
  }
  return -1
}

// ---- markdown ----

const BLOCK = new Set([
  "p",
  "div",
  "tr",
  "table",
  "ul",
  "ol",
  "section",
  "article",
  "header",
  "footer",
  "blockquote",
])
const HEADING = new Set(["h1", "h2", "h3", "h4", "h5", "h6"])
const SKIP = new Set(["style", "script", "head", "title"])
/** "Seen by patient <name> on <time>": rewritten each time a result is
 * opened, so it is no part of the record. */
const SEEN = /^Seen by patient .* on \d/

/** HTML as markdown: a line per block, list item and line break, a
 * heading in bold, table cells joined by ` | `, one blank line between
 * runs of blocks; styles, scripts and the head left out; the "Seen by
 * patient" line left out. Null where no words are left. */
export function markdown(html: string | null | undefined): string | null {
  if (!html) return null
  const lines: string[] = []
  let line: string[] = []
  let skip = 0
  let head = false
  const flush = () => {
    const text = strip(line.join("").replace(/[ \t\u00a0\u200b]+/g, " "))
    if (text === "-") {
      // a list item whose words sit in a block inside it: keep its marker
      line = ["- "]
      return
    }
    line = []
    if (text) lines.push(head ? `**${text}**` : text)
    else if (lines.length && lines[lines.length - 1] !== "") lines.push("")
  }
  for (const t of tokenize(html)) {
    if (t.type === "text") {
      if (!skip) line.push(t.text)
    } else if (t.type === "start") {
      if (SKIP.has(t.name)) skip++
      else if (t.name === "br") flush()
      else if (BLOCK.has(t.name) || HEADING.has(t.name) || t.name === "li") {
        flush()
        head = HEADING.has(t.name)
        if (t.name === "li") line.push("- ")
      } else if ((t.name === "td" || t.name === "th") && strip(line.join("")))
        line.push(" | ")
    } else if (SKIP.has(t.name)) skip = Math.max(0, skip - 1)
    else if (BLOCK.has(t.name) || HEADING.has(t.name) || t.name === "li") {
      flush()
      head = false
    }
  }
  flush()
  const out = lines.filter((l) => !SEEN.test(l))
  while (out.length && out[out.length - 1] === "") out.pop()
  while (out.length && out[0] === "") out.shift()
  return out.map((l) => l.replace(/^\|\s*/, "")).join("\n") || null
}
