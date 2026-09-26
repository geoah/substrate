/** An agent's reply as blocks and inline runs: the small markdown subset
 * models actually write (paragraphs, bullet and numbered lists, headings,
 * fenced code, `code` and **bold**), plus record paths, which the agent is
 * told to name records by and which render as the record itself.
 *
 * Deliberately a subset and deliberately inert: the text is model-authored,
 * so nothing here produces HTML — the component renders runs as elements,
 * and anything this does not recognise stays the text it was. */

export type Inline =
  | { type: "text"; text: string }
  | { type: "strong"; text: string }
  | { type: "code"; text: string }
  | { type: "record"; kind: string; id: string; text: string }

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

/** One line's inline runs. */
export function inlineRuns(line: string): Inline[] {
  const out: Inline[] = []
  const pattern = /`([^`]+)`|\*\*([^*]+)\*\*/g
  let at = 0
  for (let m = pattern.exec(line); m; m = pattern.exec(line)) {
    if (m.index > at) out.push(...withRecords(line.slice(at, m.index)))
    if (m[1] !== undefined) {
      const path = recordPathOf(m[1])
      out.push(
        path
          ? { type: "record", ...path, text: m[1] }
          : { type: "code", text: m[1] }
      )
    } else {
      out.push({ type: "strong", text: m[2] })
    }
    at = m.index + m[0].length
  }
  if (at < line.length) out.push(...withRecords(line.slice(at)))
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
