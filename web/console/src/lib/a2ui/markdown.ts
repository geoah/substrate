/** The Markdown an A2UI Text carries: headings, paragraphs, bullet and
 * numbered lists, fenced code, and **bold**, *italic* and `code` inside a
 * line. The catalog allows no HTML, links or images, and the text is an
 * agent's interpolated with record values, so nothing here produces HTML:
 * a link or an image reads as its own words, and anything unrecognised
 * stays the text it was. */

export type MdInline =
  | { type: "text"; text: string }
  | { type: "strong"; runs: MdInline[] }
  | { type: "em"; runs: MdInline[] }
  | { type: "code"; text: string }

export type MdBlock =
  | { type: "heading"; level: 1 | 2 | 3 | 4; runs: MdInline[] }
  | { type: "paragraph"; lines: MdInline[][] }
  | { type: "list"; ordered: boolean; items: MdInline[][] }
  | { type: "code"; text: string }

// A link or an image keeps its words and drops its target.
const LINK = /!?\[([^\]]*)\]\([^)]*\)/g

/** The runs of one line. Delimiters must close on the same line; an
 * unmatched one is text. */
export function mdInline(line: string): MdInline[] {
  const text = line.replace(LINK, "$1")
  const out: MdInline[] = []
  let plain = ""
  const flush = () => {
    if (plain) out.push({ type: "text", text: plain })
    plain = ""
  }
  let i = 0
  while (i < text.length) {
    const rest = text.slice(i)
    if (rest[0] === "\\" && rest.length > 1 && /[\\`*_]/.test(rest[1])) {
      plain += rest[1]
      i += 2
      continue
    }
    if (rest[0] === "`") {
      const end = text.indexOf("`", i + 1)
      if (end > i + 1) {
        flush()
        out.push({ type: "code", text: text.slice(i + 1, end) })
        i = end + 1
        continue
      }
    }
    const strong = /^(\*\*|__)(?=\S)([\s\S]*?\S)\1/.exec(rest)
    if (strong) {
      flush()
      out.push({ type: "strong", runs: mdInline(strong[2]) })
      i += strong[0].length
      continue
    }
    const em = /^(\*|_)(?=\S)([\s\S]*?\S)\1(?!\1)/.exec(rest)
    if (em && !(em[1] === "_" && /\w/.test(text[i - 1] ?? ""))) {
      flush()
      out.push({ type: "em", runs: mdInline(em[2]) })
      i += em[0].length
      continue
    }
    plain += rest[0]
    i++
  }
  flush()
  return out
}

const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/
const BULLET = /^\s*[-*+]\s+(.*)$/
const NUMBERED = /^\s*\d+[.)]\s+(.*)$/
const FENCE = /^\s*```/

export function mdBlocks(source: string): MdBlock[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n")
  const blocks: MdBlock[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    if (!line.trim()) {
      i++
      continue
    }
    if (FENCE.test(line)) {
      const body: string[] = []
      i++
      while (i < lines.length && !FENCE.test(lines[i])) body.push(lines[i++])
      i++
      blocks.push({ type: "code", text: body.join("\n") })
      continue
    }
    const heading = HEADING.exec(line)
    if (heading) {
      const level = Math.min(heading[1].length, 4) as 1 | 2 | 3 | 4
      blocks.push({ type: "heading", level, runs: mdInline(heading[2]) })
      i++
      continue
    }
    const listKind = BULLET.test(line)
      ? BULLET
      : NUMBERED.test(line)
        ? NUMBERED
        : undefined
    if (listKind) {
      const items: MdInline[][] = []
      while (i < lines.length) {
        const m = listKind.exec(lines[i])
        if (!m) break
        items.push(mdInline(m[1]))
        i++
      }
      blocks.push({ type: "list", ordered: listKind === NUMBERED, items })
      continue
    }
    const paragraph: MdInline[][] = []
    while (
      i < lines.length &&
      lines[i].trim() &&
      !FENCE.test(lines[i]) &&
      !HEADING.test(lines[i]) &&
      !BULLET.test(lines[i]) &&
      !NUMBERED.test(lines[i])
    ) {
      paragraph.push(mdInline(lines[i].trim()))
      i++
    }
    blocks.push({ type: "paragraph", lines: paragraph })
  }
  return blocks
}
