/** The `.inkd` format, client-side: what a reader refuses and what it reads
 * leniently, and that an edit keeps every field it did not touch, which the
 * spec requires of a writer so a newer app's note survives the console. */

import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"

import { InkPageView } from "@/components/ink/ink-page"
import {
  backgroundSpacing,
  backgroundType,
  inkPoint,
  inkSummary,
  inkText,
  inkTimestamp,
  newInkDocument,
  readInk,
  strokeColor,
  strokePoints,
  strokeTool,
  withBackground,
  withNewPage,
  withStroke,
  withoutStrokes,
  type InkDocument,
  type InkStroke,
} from "./ink"
import { cropBox, inkShape, strokesHit } from "./ink-render"

const NOW = new Date("2026-10-23T12:00:00.250Z")

function ids() {
  let n = 0
  return () => `id-${++n}`
}

/** A note a newer writer produced: unknown fields on every object. */
function newerNote(): InkDocument {
  return {
    format: "inkidian",
    formatVersion: 1,
    id: "doc",
    createdAt: "2026-10-06T17:00:00Z",
    modifiedAt: "2026-10-06T17:05:00Z",
    page: { width: 1240, height: 1754, unit: "pt" },
    futureRoot: { kept: true },
    pages: [
      {
        id: "p1",
        background: { type: "lined", spacing: 40, futureBackground: 1 },
        futurePage: "kept",
        strokes: [
          {
            id: "s1",
            tool: "pen",
            color: "#1C1C1E",
            opacity: 1,
            baseWidth: 2.5,
            futureStroke: [1, 2],
            points: [
              [10, 20, 0.4, 0, 0.5, 1.2, 99],
              [30, 40, 0.6, 0.1, 0.5, 1.2],
            ],
          },
        ],
      },
      { id: "p2", background: { type: "blank" }, strokes: [] },
    ],
  }
}

const stroke = (id: string, points: number[][]): InkStroke => ({
  id,
  tool: "pen",
  color: "#000000",
  opacity: 1,
  baseWidth: 2,
  points,
})

describe("readInk", () => {
  it("reads a note from its text or its parsed JSON", () => {
    const note = newerNote()
    const fromText = readInk(inkText(note))
    expect(fromText.ok && fromText.document).toEqual(note)
    const fromJson = readInk(note)
    expect(fromJson.ok && fromJson.document).toBe(note)
  })

  it("refuses what cannot be drawn, saying why", () => {
    expect(readInk("{not json")).toMatchObject({ ok: false, reason: "invalid" })
    expect(readInk({ format: "excalidraw" })).toMatchObject({
      ok: false,
      reason: "not-ink",
    })
    expect(readInk({ ...newerNote(), page: undefined })).toMatchObject({
      ok: false,
      message: "This note can't be displayed because its page size is missing.",
    })
    expect(readInk({ ...newerNote(), pages: [] })).toMatchObject({
      reason: "invalid",
    })
    expect(
      readInk({ ...newerNote(), pages: [{ id: "x", background: {} }] })
    ).toMatchObject({ reason: "invalid" })
  })

  it("refuses a stroke that is not an object, before anything reads it", () => {
    for (const bad of [null, 1, "s1", true, [[10, 20, 0.5]]]) {
      const note = newerNote()
      note.pages[1].strokes = [bad as unknown as InkStroke]
      expect(readInk(inkText(note)), JSON.stringify(bad)).toEqual({
        ok: false,
        reason: "invalid",
        message:
          "This note can't be displayed because a page holds a stroke that is not an object.",
      })
    }
  })

  it("reads an odd stroke object leniently: summed, drawn, unknown fields kept", () => {
    const note = newerNote()
    note.pages[0].strokes.push(
      { futureStroke: "kept", points: "not a list" } as unknown as InkStroke,
      { id: "s3", points: [null, [5, 5], "x", [6, 6, 2]] } as InkStroke
    )
    const read = readInk(inkText(note))
    if (!read.ok) throw new Error(read.message)
    const { document } = read
    expect(document.pages[0].strokes[1].futureStroke).toBe("kept")
    expect(inkSummary(document)).toEqual({ pages: 2, strokes: 3, points: 6 })
    for (const s of document.pages[0].strokes)
      expect(() => inkShape(s)).not.toThrow()
    expect(() => strokesHit(document.pages[0].strokes, 5, 5, 4)).not.toThrow()
    expect(() => cropBox(document.pages[0], 1240, 1754)).not.toThrow()
    const svg = renderToStaticMarkup(
      createElement(InkPageView, { document, pageIndex: 0 })
    )
    expect(svg).toContain('data-slot="ink-page"')
  })

  it("refuses a newer format version rather than drawing part of it", () => {
    const read = readInk({ ...newerNote(), formatVersion: 2 })
    expect(read).toMatchObject({ ok: false, reason: "newer-version" })
    expect(!read.ok && read.message).toContain("format version 2")
  })
})

describe("lenient reads at draw time", () => {
  it("draws an unknown tool as a pen and a bad color as black", () => {
    const odd = { ...stroke("s", [[0, 0]]), tool: "brush", color: "red" }
    expect(strokeTool(odd)).toBe("pen")
    expect(strokeColor(odd)).toBe("#000000")
  })

  it("skips a point without a position and defaults a missing pressure", () => {
    const points = strokePoints(
      stroke("s", [[1, 2], ["x", 3] as unknown as number[], [4, 5, 0.9]])
    )
    expect(points).toEqual([
      [1, 2, 0.5],
      [4, 5, 0.9],
    ])
  })

  it("draws an unknown background as blank, with the default spacing", () => {
    expect(backgroundType({ type: "hexagons" })).toBe("blank")
    expect(backgroundType({ type: "dots" })).toBe("dots")
    expect(backgroundSpacing({ type: "grid" })).toBe(32)
    expect(backgroundSpacing({ type: "grid", spacing: 24 })).toBe(24)
  })
})

describe("edits", () => {
  it("keep every field and object they did not change", () => {
    const note = newerNote()
    const drawn = withStroke(note, 1, stroke("s2", [[5, 5]]), NOW)
    // The untouched page is the same object, unknown fields and all.
    expect(drawn.pages[0]).toBe(note.pages[0])
    expect(drawn.futureRoot).toEqual({ kept: true })
    expect(drawn.pages[1].strokes.map((s) => s.id)).toEqual(["s2"])
    expect(drawn.modifiedAt).toBe("2026-10-23T12:00:00Z")
    expect(drawn.createdAt).toBe(note.createdAt)
    // And the original is not changed under the editor's undo history.
    expect(note.pages[1].strokes).toEqual([])

    const erased = withoutStrokes(drawn, 0, new Set(["s1"]), NOW)
    expect(erased.pages[0].futurePage).toBe("kept")
    expect(erased.pages[0].strokes).toEqual([])
    expect(erased.pages[1]).toBe(drawn.pages[1])

    const repapered = withBackground(note, 0, "grid", NOW)
    expect(repapered.pages[0].background).toEqual({
      type: "grid",
      spacing: 40,
      futureBackground: 1,
    })
  })

  it("leave the document alone when there is nothing to change", () => {
    const note = newerNote()
    expect(withoutStrokes(note, 0, new Set(), NOW)).toBe(note)
    expect(withStroke(note, 7, stroke("s", [[0, 0]]), NOW)).toBe(note)
  })

  it("add a page on the paper asked for", () => {
    const note = withNewPage(newerNote(), "dots", NOW, ids())
    expect(note.pages).toHaveLength(3)
    expect(note.pages[2]).toEqual({
      id: "id-1",
      background: { type: "dots" },
      strokes: [],
    })
  })
})

describe("a new note", () => {
  it("is a valid one-page v1 document", () => {
    const note = newInkDocument({ now: NOW, newId: ids(), background: "lined" })
    expect(note).toMatchObject({
      format: "inkidian",
      formatVersion: 1,
      id: "id-1",
      createdAt: "2026-10-23T12:00:00Z",
      modifiedAt: "2026-10-23T12:00:00Z",
      page: { width: 1240, height: 1754, unit: "pt" },
      pages: [{ id: "id-2", background: { type: "lined" }, strokes: [] }],
    })
    expect(readInk(inkText(note)).ok).toBe(true)
  })
})

describe("inkPoint", () => {
  it("rounds to the spec's precision and clamps the pressure", () => {
    expect(inkPoint(1.23456, 7.891, 1.4, 0.12345, 0.7854, 1.5708)).toEqual([
      1.23, 7.89, 1, 0.123, 0.785, 1.571,
    ])
  })

  it("defaults what a mouse does not report", () => {
    expect(inkPoint(0, 0)).toEqual([0, 0, 0.5, 0, 0, 1.571])
  })
})

describe("inkSummary and inkTimestamp", () => {
  it("count pages, strokes and points", () => {
    expect(inkSummary(newerNote())).toEqual({ pages: 2, strokes: 1, points: 2 })
  })

  it("stamp RFC 3339 in UTC without fractions", () => {
    expect(inkTimestamp(NOW)).toBe("2026-10-23T12:00:00Z")
  })
})
