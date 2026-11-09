import { describe, expect, it } from "vitest"
import {
  blobDigest,
  canonicalJson,
  documentName,
  extensionFor,
  extensionOf,
  jsonDocument,
  mediaTypeOf,
  plannedDocument,
} from "./documents"

const utf8 = (s: string) => new TextEncoder().encode(s)

describe("a document", () => {
  it("is stored by the SHA-256 of its bytes", () => {
    expect(blobDigest(utf8("abc"))).toBe(
      "blob-sha256-ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    )
  })

  it("is named neutrally, never by the provider's file name", () => {
    const pdf = plannedDocument(utf8("%PDF"), {
      provider: "Randox",
      kind: "health report",
      date: "2026-01-19",
      mediaType: "application/pdf",
    })
    expect(pdf).toMatchObject({
      name: "Randox health report 2026-01-19.pdf",
      mediaType: "application/pdf",
      digest: blobDigest(utf8("%PDF")),
    })
    expect(
      plannedDocument(utf8("x"), {
        provider: "DXA",
        kind: "trend page",
        date: "2026-01-01",
        mediaType: "image/jpeg; charset=binary",
      }).name
    ).toBe("DXA trend page 2026-01-01.jpg")
    expect(
      plannedDocument(utf8("x"), {
        provider: "NHS",
        kind: "document",
        date: "2026-01-01",
        ext: "TIF",
      })
    ).toMatchObject({
      name: "NHS document 2026-01-01.tif",
      mediaType: "image/tiff",
    })
    expect(documentName("NHS", "letter", "", "")).toBe("NHS letter")
  })

  it("has its media type from its extension", () => {
    expect(extensionOf("files/report/a.b.PDF")).toBe("pdf")
    expect(extensionOf("folder.d/README")).toBe("")
    expect(mediaTypeOf("x/scan.jpeg")).toBe("image/jpeg")
    expect(mediaTypeOf("x/blob")).toBe("application/octet-stream")
    expect(mediaTypeOf("x/blob", "")).toBe("")
    expect(extensionFor("text/html; charset=utf-8")).toBe("html")
    expect(extensionFor("application/x-unknown")).toBe("")
  })
})

describe("canonical JSON", () => {
  it("sorts keys by code point and prints numbers as JavaScript does", () => {
    expect(
      canonicalJson({
        b: 1,
        a: [140.0, { d: null, c: "é" }],
        "\u{1f600}": 1,
        "\uffff": 2,
      })
    ).toBe('{"a":[140,{"c":"é","d":null}],"b":1,"\uffff":2,"\u{1f600}":1}')
  })

  it("leaves out an undefined member and writes an undefined element as null", () => {
    expect(canonicalJson({ a: undefined, b: [undefined, 1e-7, "x\ny"] })).toBe(
      '{"b":[null,1e-7,"x\\ny"]}'
    )
    expect(canonicalJson("x")).toBe('"x"')
    expect(canonicalJson(null)).toBe("null")
  })

  it("is a JSON document's bytes", () => {
    const doc = jsonDocument(
      { test: { short_id: "T1" }, review: null },
      { provider: "Numan", kind: "test", date: "2026-01-05" }
    )
    expect(new TextDecoder().decode(doc.bytes)).toBe(
      '{"review":null,"test":{"short_id":"T1"}}'
    )
    expect(doc).toMatchObject({
      name: "Numan test 2026-01-05.json",
      mediaType: "application/json",
      digest: blobDigest(doc.bytes),
    })
  })
})
