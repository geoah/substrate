// @vitest-environment node
import { Effect } from "effect"
import { describe, expect, it } from "vitest"
import { pdfFixture, runResult } from "../testing"
import { pageLines, pdfPages, pdfText } from "./pdf"

const PAGES = [
  [
    "BT /F1 12 Tf 72 700 Td (Body composition) Tj ET",
    "BT /F2 9 Tf 1 0 0 1 100 650 Tm (Weight) Tj ET",
    "BT /F1 9 Tf 1 0 0 1 160 650 Tm (80.5) Tj ET",
  ].join("\n"),
  "BT /F1 10 Tf 72 500 Td [(Page) -250 (two)] TJ ET",
]

describe("a PDF through pdf.js", () => {
  it("gives each page's text items with their positions and sizes", async () => {
    const bytes = pdfFixture(PAGES, { compress: true })
    const pages = await Effect.runPromise(pdfPages(bytes, "scan.pdf"))
    expect(pages).toHaveLength(2)
    expect(pages[0]).toMatchObject({ number: 1, width: 595, height: 842 })
    const weight = pages[0].items.find((i) => i.text === "Weight")
    expect(weight).toMatchObject({ x: 100, y: 650, fontSize: 9 })
    expect(weight!.width).toBeGreaterThan(0)
    expect(pages[0].items.find((i) => i.text === "80.5")).toMatchObject({
      x: 160,
      y: 650,
    })
    expect(pages[1].items.map((i) => i.text).join("")).toMatch(/Page\s*two/)
    // pdf.js was given a copy: the caller's bytes are still whole.
    expect(bytes.length).toBeGreaterThan(0)
    expect(bytes[0]).toBe(0x25)
  })

  it("reads a page's items as lines, and a document as text", async () => {
    const pages = await Effect.runPromise(
      pdfPages(pdfFixture(PAGES), "scan.pdf")
    )
    expect(pageLines(pages[0])).toEqual(["Body composition", "Weight 80.5"])
    expect(pdfText(pages)).toMatch(
      /^Body composition\nWeight 80\.5\n\nPage\s+two$/
    )
    expect(pdfText([])).toBeUndefined()
  })

  it("is Unreadable where pdf.js cannot open it", async () => {
    const r = await runResult(
      pdfPages(new TextEncoder().encode("not a pdf"), "broken.pdf")
    )
    expect(r).toMatchObject({
      _tag: "Failure",
      failure: { _tag: "Unreadable", path: "broken.pdf" },
    })
  })
})
