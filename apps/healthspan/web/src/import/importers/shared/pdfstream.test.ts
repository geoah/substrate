import { Effect } from "effect"
import { zlibSync, deflateSync } from "fflate"
import { describe, expect, it } from "vitest"
import { pdfFixture, runResult } from "../testing"
import {
  contentRuns,
  glyphOffset,
  inflate,
  latin1,
  pdfStreams,
  readTextRuns,
  textRuns,
} from "./pdfstream"

/** Text as a hex string of glyph ids at an offset of 29, as the body
 * composition sheet's generator writes its Helvetica subsets. */
const glyphs = (text: string) =>
  `<${[...text].map((c) => (c.charCodeAt(0) - 29).toString(16).padStart(4, "0")).join("")}>`

const latin = (s: string) => Uint8Array.from(s, (c) => c.charCodeAt(0))

// A sheet in the generator's shape: the font set outside each text block
// and held across blocks, positions by Tm or Td, hex glyph strings in TJ.
const SHEET = [
  "/F2 9 Tf",
  `BT 1 0 0 1 40.5 700.25 Tm [${glyphs("Weight")}] TJ ET`,
  `BT 130.2 700 Td [${glyphs("80")} -120 ${glyphs(".5")}] TJ ET`,
  "/F3 7 Tf",
  `BT 1 0 0 1 300 650 Tm [${glyphs("5.9")}<0063>] TJ ET`,
  "BT [(no position)] TJ ET",
  "q 1 0 0 1 0 0 cm Q",
  "BT 10 20 Td 0 0 Td (Hello \\(x\\) \\101) Tj ET",
].join("\n")

describe("a PDF's content streams", () => {
  it("are read whole, deflated with a trailing byte, cut at /Length", () => {
    const pdf = pdfFixture([SHEET], { compress: true, trailingEol: true })
    const streams = pdfStreams(pdf)
    expect(streams).toHaveLength(1)
    expect(latin1(streams[0])).toBe(SHEET)
  })

  it("are kept as they are without a filter", () => {
    expect(latin1(pdfStreams(pdfFixture([SHEET]))[0])).toBe(SHEET)
  })

  it("are left out under a filter that is not FlateDecode", () => {
    const image = latin(
      "1 0 obj\n<< /Length 4 /Filter /DCTDecode >>\nstream\n\xff\xd8\xff\xe0\nendstream\nendobj\n"
    )
    expect(pdfStreams(image)).toEqual([])
  })

  it("end at endstream where /Length is indirect", () => {
    const data = zlibSync(latin("BT 1 2 Td (x) Tj ET /F1 1 Tf"))
    const head = latin(
      "4 0 obj\n<< /Length 9 0 R /Filter /FlateDecode >>\nstream\n"
    )
    const tail = latin("\r\nendstream\nendobj\n")
    const file = new Uint8Array([...head, ...data, ...tail])
    expect(latin1(pdfStreams(file)[0])).toBe("BT 1 2 Td (x) Tj ET /F1 1 Tf")
  })
})

describe("inflating", () => {
  it("tolerates what follows the deflate data, with or without a zlib header", () => {
    const text = latin("BT ET ".repeat(20))
    const z = zlibSync(text)
    expect(inflate(new Uint8Array([...z, 0x0a]))).toEqual(text)
    expect(inflate(z.subarray(0, z.length - 4))).toEqual(text)
    expect(inflate(deflateSync(text))).toEqual(text)
    expect(inflate(latin("not deflate at all"))).toBeUndefined()
  })

  it("reads bytes as true Latin-1", () => {
    expect(latin1(new Uint8Array([0x41, 0x80, 0x9f, 0xe9]))).toBe(
      "A\u0080\u009fé"
    )
  })
})

describe("text runs", () => {
  it("are placed by each block's operators, in the font set before them", () => {
    const runs = textRuns(
      pdfFixture([SHEET], { compress: true, trailingEol: true }),
      glyphOffset(29)
    )
    expect(runs).toEqual([
      { x: 40.5, y: 700.25, font: "F2", size: 9, text: "Weight" },
      { x: 130.2, y: 700, font: "F2", size: 9, text: "80.5" },
      { x: 300, y: 650, font: "F3", size: 7, text: "5.9\ufffd" },
      { x: 10, y: 20, font: "F3", size: 7, text: "Hello (x) A" },
    ])
  })

  it("prefer a block's Tm to its Td, and take a font set inside the block", () => {
    expect(
      contentRuns("BT 5 6 Td /F1 12 Tf 1 0 0 1 70 80 Tm (a) Tj ( b ) ' ET")
    ).toEqual([{ x: 70, y: 80, font: "F1", size: 12, text: "a b" }])
  })

  it("skip an inline image's data", () => {
    expect(
      contentRuns("BI /W 1 /H 1 ID \x00\xffBT junk EI BT 1 2 Td (ok) Tj ET")
    ).toEqual([{ x: 1, y: 2, font: undefined, size: undefined, text: "ok" }])
  })

  it("fail as Unreadable where a PDF shows no text", async () => {
    const r = await runResult(readTextRuns(pdfFixture(["q Q"]), "sheet.pdf"))
    expect(r).toMatchObject({
      _tag: "Failure",
      failure: { _tag: "Unreadable", path: "sheet.pdf" },
    })
    const ok = await Effect.runPromise(
      readTextRuns(pdfFixture([SHEET]), "s.pdf")
    )
    expect(ok[0].text).toBe("\u0000:\u0000H\u0000L\u0000J\u0000K\u0000W")
  })
})
