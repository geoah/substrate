// A delimited file read in the browser: comma, tab or semicolon separated,
// fields quoted the RFC 4180 way (a doubled quote inside one, separators
// and line breaks kept), a byte order mark dropped, blank lines skipped.

export type Delimiter = "," | "\t" | ";"

export interface Table {
  delimiter: Delimiter
  headers: string[]
  /** Each data row, as many cells as the headers (short rows padded). */
  rows: string[][]
}

/** The separator a file uses: a `.tsv` name says tab; otherwise the one
 * that splits the first line most, outside quotes. */
export function detectDelimiter(text: string, fileName = ""): Delimiter {
  if (/\.tsv$/i.test(fileName)) return "\t"
  const first = firstLine(text)
  const counts: Record<Delimiter, number> = { ",": 0, "\t": 0, ";": 0 }
  let quoted = false
  for (const ch of first) {
    if (ch === '"') quoted = !quoted
    else if (!quoted && (ch === "," || ch === "\t" || ch === ";")) counts[ch]++
  }
  if (counts["\t"] > 0 && counts["\t"] >= counts[","]) return "\t"
  if (counts[";"] > counts[","]) return ";"
  return ","
}

function firstLine(text: string): string {
  let quoted = false
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (ch === '"') quoted = !quoted
    else if (!quoted && (ch === "\n" || ch === "\r")) return text.slice(0, i)
  }
  return text
}

/** Every record of the text as cells. */
export function parseDelimited(text: string, delimiter: Delimiter): string[][] {
  const src = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text
  const out: string[][] = []
  let row: string[] = []
  let cell = ""
  let quoted = false
  let i = 0
  const endRow = () => {
    row.push(cell)
    cell = ""
    if (row.some((c) => c.trim() !== "")) out.push(row)
    row = []
  }
  while (i < src.length) {
    const ch = src[i]
    if (quoted) {
      if (ch === '"') {
        if (src[i + 1] === '"') {
          cell += '"'
          i += 2
          continue
        }
        quoted = false
        i++
        continue
      }
      cell += ch
      i++
      continue
    }
    if (ch === '"' && cell.trim() === "") {
      quoted = true
      cell = ""
      i++
      continue
    }
    if (ch === delimiter) {
      row.push(cell)
      cell = ""
      i++
      continue
    }
    if (ch === "\r" || ch === "\n") {
      endRow()
      i += ch === "\r" && src[i + 1] === "\n" ? 2 : 1
      continue
    }
    cell += ch
    i++
  }
  if (cell !== "" || row.length) endRow()
  return out
}

/** A file's text as a table: its first non-blank record the headers. */
export function readTable(text: string, fileName = ""): Table {
  const delimiter = detectDelimiter(
    text.charCodeAt(0) === 0xfeff ? text.slice(1) : text,
    fileName
  )
  const records = parseDelimited(text, delimiter)
  const headers = (records[0] ?? []).map((h) => h.trim())
  const rows = records.slice(1).map((r) => {
    const cells = r.map((c) => c.trim())
    while (cells.length < headers.length) cells.push("")
    return cells
  })
  return { delimiter, headers, rows }
}

/** One field as a CSV writes it: quoted where it holds a separator, a
 * quote or a line break. */
export function csvField(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value
}
