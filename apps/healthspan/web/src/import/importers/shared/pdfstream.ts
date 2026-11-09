// A PDF's text read straight from its content streams, for a file a PDF
// library does not open whole (apps/healthspan/docs/importers/00-shared.md,
// PDFs): every stream inflated with fflate, which tolerates the byte some
// generators leave after the deflate data where the browser's
// DecompressionStream refuses it, its bytes read as Latin-1, and each text
// block placed by its operators' positions.

import { Effect } from "effect"
import { inflateSync } from "fflate"
import type { ImportError } from "../types"
import { strip } from "./numbers"
import { unreadable } from "./schema"

/** Bytes as true Latin-1, one character per byte (`TextDecoder("latin1")`
 * is windows-1252, which differs from 0x80 to 0x9f). */
export function latin1(bytes: Uint8Array): string {
  let out = ""
  for (let i = 0; i < bytes.length; i += 0x8000)
    out += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return out
}

/** Deflate data inflated, a zlib header before it or not; whatever follows
 * the last block (a checksum, an end-of-line byte) is ignored. Undefined
 * for data that does not inflate. */
export function inflate(data: Uint8Array): Uint8Array | undefined {
  const zlib =
    data.length > 2 &&
    (data[0] & 0x0f) === 8 &&
    data[0] >> 4 <= 7 &&
    ((data[0] << 8) | data[1]) % 31 === 0 &&
    !(data[1] & 0x20)
  try {
    return inflateSync(zlib ? data.subarray(2) : data)
  } catch {
    return undefined
  }
}

/** Every stream of a PDF, each cut at its `/Length` where the dictionary
 * gives it directly and in range, else at its `endstream`; inflated where
 * its filter is FlateDecode (left out where it does not inflate), kept as
 * it is where it has no filter, left out under any other filter. */
export function pdfStreams(bytes: Uint8Array): Uint8Array[] {
  const text = latin1(bytes)
  const out: Uint8Array[] = []
  const keyword = /stream\r?\n/g
  let m: RegExpExecArray | null
  while ((m = keyword.exec(text))) {
    if (text.slice(Math.max(0, m.index - 3), m.index) === "end") continue
    const start = m.index + m[0].length
    const objAt = text.lastIndexOf("obj", m.index)
    const dict = objAt < 0 ? "" : text.slice(objAt, m.index)
    const length = /\/Length\s+(\d+)(?!\s+\d+\s+R)/.exec(dict)
    let end = length ? start + Number(length[1]) : -1
    if (end < start || end > bytes.length) {
      end = text.indexOf("endstream", start)
      if (end < 0) end = bytes.length
      if (text[end - 1] === "\n") end--
      if (text[end - 1] === "\r") end--
    }
    keyword.lastIndex = Math.max(keyword.lastIndex, end)
    const data = bytes.subarray(start, end)
    const filters = [...dict.matchAll(/\/(\w+Decode)\b/g)].map((f) => f[1])
    if (!filters.length) out.push(data)
    else if (filters.length === 1 && filters[0] === "FlateDecode") {
      const inflated = inflate(data)
      if (inflated) out.push(inflated)
    }
  }
  return out
}

/** One text block of a content stream: where it sits, in PDF user space
 * as its operators place it (y grows upwards; no `cm` transform applied),
 * in which font, and what it shows. */
export interface TextRun {
  x: number
  y: number
  /** The font resource's name, without its slash (`F1`). */
  font?: string
  size?: number
  text: string
}

/** How a shown string's bytes become text, by the font it is shown in;
 * `hex` for a `<...>` string. */
export type GlyphDecoder = (
  bytes: Uint8Array,
  font: string | undefined,
  hex: boolean
) => string

/** Each byte as its Latin-1 character. */
export const latin1Glyphs: GlyphDecoder = (bytes) => latin1(bytes)

/** For a font whose glyph ids follow the standard order at an offset (a
 * subset some generators write as Identity-H): a hex string read two bytes
 * a glyph, each glyph the character `id + offset`, U+FFFD where that is
 * not printable ASCII; a literal string as Latin-1. */
export function glyphOffset(offset: number): GlyphDecoder {
  return (bytes, _font, hex) => {
    if (!hex) return latin1(bytes)
    let out = ""
    for (let i = 0; i + 1 < bytes.length; i += 2) {
      const c = ((bytes[i] << 8) | bytes[i + 1]) + offset
      out += c >= 32 && c < 127 ? String.fromCharCode(c) : "\ufffd"
    }
    return out
  }
}

type Operand =
  | { t: "num"; v: number }
  | { t: "name"; v: string }
  | { t: "str"; v: Uint8Array; hex: boolean }
  | { t: "arr"; v: Operand[] }
  | { t: "mark" }
  | { t: "dict" }

const DELIMS = "()<>[]{}/%"
const isSpace = (c: string) =>
  c === " " ||
  c === "\n" ||
  c === "\r" ||
  c === "\t" ||
  c === "\f" ||
  c === "\0"

const ESCAPES: Record<string, number> = {
  n: 10,
  r: 13,
  t: 9,
  b: 8,
  f: 12,
  "(": 40,
  ")": 41,
  "\\": 92,
}

/** A literal string from just after its `(`: its bytes and where it ends. */
function literal(s: string, i: number): { bytes: Uint8Array; end: number } {
  const out: number[] = []
  let depth = 1
  while (i < s.length) {
    const c = s[i]
    if (c === "\\") {
      const n = s[i + 1]
      if (n === undefined) break
      if (ESCAPES[n] !== undefined) {
        out.push(ESCAPES[n])
        i += 2
      } else if (/[0-7]/.test(n)) {
        const oct = /^[0-7]{1,3}/.exec(s.slice(i + 1, i + 4))![0]
        out.push(parseInt(oct, 8) & 0xff)
        i += 1 + oct.length
      } else if (n === "\r" || n === "\n") {
        i += n === "\r" && s[i + 2] === "\n" ? 3 : 2
      } else {
        out.push(n.charCodeAt(0))
        i += 2
      }
      continue
    }
    if (c === "(") depth++
    else if (c === ")" && --depth === 0)
      return { bytes: new Uint8Array(out), end: i + 1 }
    out.push(c.charCodeAt(0))
    i++
  }
  return { bytes: new Uint8Array(out), end: i }
}

/** The text blocks of one content stream, in order. A block is `BT` to
 * `ET`; it sits where its first `Tm` puts it (the matrix's last two
 * numbers), else its first `Td` or `TD`; one with neither is left out. Its
 * text is every string it shows (`Tj`, `TJ`, `'`, `"`) in order, stripped;
 * its font the one set by the last `Tf` before its first string, which
 * holds across blocks. */
export function contentRuns(
  content: string,
  decode: GlyphDecoder = latin1Glyphs
): TextRun[] {
  const runs: TextRun[] = []
  const stack: Operand[] = []
  let font: string | undefined
  let size: number | undefined
  let block:
    | {
        tm?: [number, number]
        td?: [number, number]
        font?: string
        size?: number
        text: string
      }
    | undefined
  const show = (o: Operand | undefined) => {
    if (!block || !o || o.t !== "str") return
    if (block.font === undefined && block.text === "") {
      block.font = font
      block.size = size
    }
    block.text += decode(o.v, font, o.hex)
  }
  const nums = (n: number): number[] | undefined => {
    const xs = stack.slice(-n)
    return xs.length === n && xs.every((x) => x.t === "num")
      ? xs.map((x) => (x as { v: number }).v)
      : undefined
  }
  const s = content
  let i = 0
  while (i < s.length) {
    const c = s[i]
    if (isSpace(c)) {
      i++
      continue
    }
    if (c === "%") {
      while (i < s.length && s[i] !== "\n" && s[i] !== "\r") i++
      continue
    }
    if (c === "(") {
      const lit = literal(s, i + 1)
      stack.push({ t: "str", v: lit.bytes, hex: false })
      i = lit.end
      continue
    }
    if (c === "<" && s[i + 1] === "<") {
      stack.push({ t: "dict" })
      i += 2
      continue
    }
    if (c === ">" && s[i + 1] === ">") {
      while (stack.length && stack[stack.length - 1].t !== "dict") stack.pop()
      i += 2
      continue
    }
    if (c === "<") {
      const end = s.indexOf(">", i + 1)
      const digits = s
        .slice(i + 1, end < 0 ? s.length : end)
        .replace(/[^0-9a-fA-F]/g, "")
      const padded = digits.length % 2 ? `${digits}0` : digits
      const bytes = new Uint8Array(padded.length / 2)
      for (let k = 0; k < bytes.length; k++)
        bytes[k] = parseInt(padded.slice(k * 2, k * 2 + 2), 16)
      stack.push({ t: "str", v: bytes, hex: true })
      i = end < 0 ? s.length : end + 1
      continue
    }
    if (c === "[") {
      stack.push({ t: "mark" })
      i++
      continue
    }
    if (c === "]") {
      const items: Operand[] = []
      while (stack.length && stack[stack.length - 1].t !== "mark")
        items.unshift(stack.pop()!)
      stack.pop()
      stack.push({ t: "arr", v: items })
      i++
      continue
    }
    if (c === "/") {
      let j = i + 1
      while (j < s.length && !isSpace(s[j]) && !DELIMS.includes(s[j])) j++
      stack.push({ t: "name", v: s.slice(i + 1, j) })
      i = j
      continue
    }
    let j = i
    while (j < s.length && !isSpace(s[j]) && !DELIMS.includes(s[j])) j++
    if (j === i) {
      i++
      continue
    }
    const word = s.slice(i, j)
    i = j
    if (/^[+-]?(\d+\.?\d*|\.\d+)$/.test(word)) {
      stack.push({ t: "num", v: Number(word) })
      continue
    }
    switch (word) {
      case "Tf": {
        const n = stack[stack.length - 2]
        const sz = stack[stack.length - 1]
        if (n?.t === "name") font = n.v
        if (sz?.t === "num") size = sz.v
        break
      }
      case "BT":
        block = { text: "" }
        break
      case "ET":
        if (block) {
          const at = block.tm ?? block.td
          if (at)
            runs.push({
              x: at[0],
              y: at[1],
              font: block.font ?? font,
              size: block.size ?? size,
              text: strip(block.text),
            })
        }
        block = undefined
        break
      case "Tm": {
        const m = nums(6)
        if (block && m && !block.tm) block.tm = [m[4], m[5]]
        break
      }
      case "Td":
      case "TD": {
        const m = nums(2)
        if (block && m && !block.td) block.td = [m[0], m[1]]
        break
      }
      case "Tj":
      case "'":
        show(stack[stack.length - 1])
        break
      case '"':
        show(stack[stack.length - 1])
        break
      case "TJ": {
        const a = stack[stack.length - 1]
        if (a?.t === "arr") for (const o of a.v) show(o)
        break
      }
      case "ID": {
        // An inline image's data runs to `EI`: skipped whole.
        const ei = /\sEI(?=[\s]|$)/g
        ei.lastIndex = i
        const e = ei.exec(s)
        i = e ? e.index + e[0].length : s.length
        break
      }
      default:
        break
    }
    stack.length = 0
  }
  return runs
}

/** The text runs of every content stream of a PDF, in stream order: the
 * streams that set a font (` Tf`) and show text (`BT`), each read by
 * `contentRuns`. */
export function textRuns(
  bytes: Uint8Array,
  decode: GlyphDecoder = latin1Glyphs
): TextRun[] {
  const runs: TextRun[] = []
  for (const stream of pdfStreams(bytes)) {
    const content = latin1(stream)
    if (!content.includes(" Tf") || !content.includes("BT")) continue
    runs.push(...contentRuns(content, decode))
  }
  return runs
}

/** `textRuns` as an effect, failing with `Unreadable` naming `path` where
 * the file shows no text at all. */
export function readTextRuns(
  bytes: Uint8Array,
  path: string,
  decode: GlyphDecoder = latin1Glyphs
): Effect.Effect<TextRun[], ImportError> {
  return Effect.suspend(() => {
    const runs = textRuns(bytes, decode)
    return runs.length
      ? Effect.succeed(runs)
      : Effect.fail(
          unreadable(path, "no text found in the PDF's content streams")
        )
  })
}
