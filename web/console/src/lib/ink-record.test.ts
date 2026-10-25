/** Which properties the record page draws as ink or as a PDF: by the
 * declaration's `mediaTypes`, or, where a blobref declares none, by the blob
 * it holds; and when a PDF and an ink blob pair into a marked-up PDF. */

import { describe, expect, it } from "vitest"

import { INK_MEDIA_TYPE, newInkDocument } from "./ink"
import type { KindInfo } from "./api/types"
import {
  byteSize,
  defaultNoteName,
  inkCreation,
  inkCounts,
  inkFileName,
  inkPlan,
  inkPlanNames,
  isInkSpec,
  isPdfSpec,
  withPageCount,
} from "./ink-record"
import {
  acceptFor,
  declaredFormat,
  declaresMediaType,
  mediaTypeMatches,
} from "./media-types"
import type { PropSpec } from "./record-schema"

const spec = (name: string, kind: string, mediaTypes?: string[]): PropSpec => ({
  name,
  label: name,
  kind,
  required: false,
  repeated: false,
  keyed: false,
  managed: false,
  mediaTypes,
})

const blob = (mediaType?: string, name?: string) => ({
  digest: `blob-sha256-${"a".repeat(64)}`,
  size: 10,
  mediaType,
  name,
  status: "stored",
})

describe("media types", () => {
  it("match exactly or by family, never by parameters", () => {
    expect(mediaTypeMatches("application/pdf", "application/pdf")).toBe(true)
    expect(mediaTypeMatches("application/pdf", "Application/PDF; v=1")).toBe(
      true
    )
    expect(mediaTypeMatches("image/*", "image/png")).toBe(true)
    expect(mediaTypeMatches("image/*", "application/pdf")).toBe(false)
    expect(mediaTypeMatches("application/pdf", "")).toBe(false)
  })

  it("offer the extension a browser does not know the type of", () => {
    expect(acceptFor(spec("f", "blobref", [INK_MEDIA_TYPE]))).toBe(
      `${INK_MEDIA_TYPE},.inkd`
    )
    expect(acceptFor(spec("f", "blobref", ["application/pdf"]))).toBe(
      "application/pdf,.pdf"
    )
    expect(acceptFor(spec("f", "blobref"))).toBeUndefined()
    expect(declaresMediaType(spec("f", "json"), INK_MEDIA_TYPE)).toBe(false)
  })
})

describe("declaredFormat", () => {
  it("pins a property only where every listed type is one format", () => {
    expect(declaredFormat({ mediaTypes: [INK_MEDIA_TYPE] })).toBe("inkd")
    expect(declaredFormat({ mediaTypes: ["application/pdf"] })).toBe("pdf")
    expect(declaredFormat({ mediaTypes: ["image/png", "image/*"] })).toBe(
      "other"
    )
    expect(
      declaredFormat({ mediaTypes: ["application/pdf", INK_MEDIA_TYPE] })
    ).toBeUndefined()
    expect(declaredFormat({ mediaTypes: ["application/*"] })).toBeUndefined()
    expect(declaredFormat({})).toBeUndefined()
  })
})

describe("isInkSpec and isPdfSpec", () => {
  it("read the declaration first", () => {
    expect(isInkSpec(spec("file", "blobref", [INK_MEDIA_TYPE]))).toBe(true)
    // Ink is a file: a json value is never drawn, whatever it declares.
    expect(isInkSpec(spec("ink", "json", [INK_MEDIA_TYPE]))).toBe(false)
    expect(isPdfSpec(spec("doc", "blobref", ["application/pdf"]))).toBe(true)
    // A declaration that names another type is not ink, whatever it holds.
    expect(
      isInkSpec(spec("file", "blobref", ["image/png"]), blob(INK_MEDIA_TYPE))
    ).toBe(false)
  })

  it("read the stored blob where the declaration says nothing", () => {
    expect(isInkSpec(spec("file", "blobref"), blob(INK_MEDIA_TYPE))).toBe(true)
    expect(
      isInkSpec(spec("file", "blobref"), blob(undefined, "Lecture.inkd"))
    ).toBe(true)
    expect(isPdfSpec(spec("file", "blobref"), blob("application/pdf"))).toBe(
      true
    )
    expect(isInkSpec(spec("file", "blobref"), blob("image/png"))).toBe(false)
    expect(isInkSpec(spec("file", "blobref"))).toBe(false)
  })

  it("let the stored file decide where the declaration admits several types", () => {
    // The docs' own example: a scan that may be a PDF or an image.
    const scan = spec("scan", "blobref", ["application/pdf", "image/*"])
    expect(isPdfSpec(scan, blob("image/png", "page.png"))).toBe(false)
    expect(isInkSpec(scan, blob("image/png", "page.png"))).toBe(false)
    expect(isPdfSpec(scan, blob("application/pdf", "page.pdf"))).toBe(true)
    // A family admits both formats, so one file is never both.
    const any = spec("file", "blobref", ["application/*"])
    expect(isPdfSpec(any, blob("application/pdf"))).toBe(true)
    expect(isInkSpec(any, blob("application/pdf"))).toBe(false)
    expect(isInkSpec(any, blob(INK_MEDIA_TYPE))).toBe(true)
    // Empty, such a property is neither: only a file could say.
    const both = spec("file", "blobref", ["application/pdf", INK_MEDIA_TYPE])
    expect(isPdfSpec(both)).toBe(false)
    expect(isInkSpec(both)).toBe(false)
  })

  it("never pair one property with itself", () => {
    const any = spec("file", "blobref", ["application/*"])
    const plan = inkPlan([any], { file: blob("application/pdf") })
    expect(plan).toEqual({ ink: [], markup: { document: any } })
  })

  it("never treat a list of files as one note", () => {
    const list = {
      ...spec("files", "blobref", [INK_MEDIA_TYPE]),
      repeated: true,
    }
    expect(isInkSpec(list)).toBe(false)
  })
})

describe("inkPlan", () => {
  it("pairs one PDF with an ink blob into a marked-up PDF", () => {
    const document = spec("document", "blobref", ["application/pdf"])
    const ink = spec("ink", "blobref", [INK_MEDIA_TYPE])
    const notes = spec("notes", "blobref", [INK_MEDIA_TYPE])
    const plan = inkPlan([document, ink, notes, spec("name", "string")])
    expect(plan.markup).toEqual({ document, ink })
    expect(plan.ink).toEqual([notes])
    expect(inkPlanNames(plan)).toEqual(["notes", "document", "ink"])
  })

  it("shows a lone PDF as it is, with nothing to annotate", () => {
    const file = spec("file", "blobref")
    const plan = inkPlan([file, spec("lab", "string")], {
      file: blob("application/pdf", "report.pdf"),
    })
    expect(plan).toEqual({ ink: [], markup: { document: file } })
    expect(inkPlanNames(plan)).toEqual(["file"])
  })

  it("draws ink on its own paper without exactly one PDF", () => {
    const ink = spec("ink", "blobref", [INK_MEDIA_TYPE])
    const pdfs = [
      spec("a", "blobref", ["application/pdf"]),
      spec("b", "blobref", ["application/pdf"]),
    ]
    expect(inkPlan([ink, ...pdfs])).toEqual({ ink: [ink] })
    expect(inkPlan([ink])).toEqual({ ink: [ink] })
  })
})

describe("withPageCount", () => {
  it("pads a layer to one page per PDF page and never cuts one", () => {
    const layer = newInkDocument()
    const padded = withPageCount(layer, 3, () => "new")
    expect(padded.pages).toHaveLength(3)
    expect(padded.pages[0]).toBe(layer.pages[0])
    expect(padded.pages[2]).toEqual({
      id: "new",
      background: { type: "blank" },
      strokes: [],
    })
    expect(withPageCount(padded, 2)).toBe(padded)
  })
})

describe("words", () => {
  it("name a file and say a size and a count", () => {
    expect(inkFileName("Lecture 4: notes/draft")).toBe(
      "Lecture 4 notes draft.inkd"
    )
    expect(inkFileName("", "markup")).toBe("markup.inkd")
    expect(byteSize(900)).toBe("900 B")
    expect(byteSize(80_715)).toBe("78.8 KB")
    expect(byteSize(3 << 20)).toBe("3.0 MB")
    expect(inkCounts({ pages: 1, strokes: 1 })).toBe("1 page, 1 stroke")
    expect(inkCounts({ pages: 4, strokes: 14 })).toBe("4 pages, 14 strokes")
  })
})

describe("inkCreation", () => {
  const kind = (properties: Record<string, unknown>, template = "{name}") =>
    ({
      identity: "ada.example.com/ink/note",
      name: "note",
      authority: "ada.example.com",
      package: "ink",
      version: 1,
      source: "installed",
      description: "",
      definition: {
        names: { singular: "note" },
        displayTemplate: template,
        properties,
      },
    }) as unknown as KindInfo
  const name = { type: "string", required: true }
  const file = { type: "blobref", required: true, mediaTypes: [INK_MEDIA_TYPE] }

  it("draws first for a note: one ink file and a heading", () => {
    const plan = inkCreation(
      kind({ name, file, takenAt: { type: "datetime" } })
    )
    expect(plan?.ink.name).toBe("file")
    expect(plan?.heading?.name).toBe("name")
  })

  it("keeps the general form where a create needs more", () => {
    // Another required property with nothing to fill it.
    expect(
      inkCreation(
        kind({ name, file, place: { type: "string", required: true } })
      )
    ).toBeUndefined()
    // A default fills it, so drawing first still works.
    expect(
      inkCreation(
        kind({
          name,
          file,
          place: { type: "string", required: true, default: "desk" },
        })
      )
    ).toBeDefined()
    // Two ink files, or a PDF: a marked-up PDF is not drawn first.
    expect(
      inkCreation(kind({ name, file, back: { ...file, required: false } }))
    ).toBeUndefined()
    expect(
      inkCreation(
        kind({
          name,
          file,
          document: { type: "blobref", mediaTypes: ["application/pdf"] },
        })
      )
    ).toBeUndefined()
    expect(inkCreation(kind({ name }))).toBeUndefined()
  })

  it("types only a plain text heading, and leaves any other to the form", () => {
    const takenAt = { type: "datetime", required: true }
    // A date heading the started-at name cannot fill: the form asks for it.
    expect(inkCreation(kind({ takenAt, file }, "{takenAt}"))).toBeUndefined()
    // An optional one is left empty, and the note is still drawn first.
    const plan = inkCreation(
      kind({ takenAt: { type: "datetime" }, file }, "{takenAt}")
    )
    expect(plan?.ink.name).toBe("file")
    expect(plan?.heading).toBeUndefined()
    // A string narrowed to a list of values is a choice, not free text.
    expect(
      inkCreation(
        kind(
          {
            mood: { type: "string", required: true, values: ["calm", "busy"] },
            file,
          },
          "{mood}"
        )
      )
    ).toBeUndefined()
  })
})

describe("defaultNoteName", () => {
  it("says when the note was started, in the reader's own words", () => {
    const at = new Date("2026-10-25T16:32:00Z")
    // Athens, the suites' zone, is UTC+2 once its clocks go back that morning.
    expect(defaultNoteName(at, "en-GB")).toBe("25 Oct 2026, 18:32")
  })
})
