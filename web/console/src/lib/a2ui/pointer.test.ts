import { describe, expect, it } from "vitest"

import {
  getAt,
  parsePointer,
  pathsOverlap,
  removeAt,
  resolvePath,
  setAt,
} from "./pointer"

describe("JSON pointers", () => {
  const doc = {
    ui: { q: "dune", "a/b": 1, "m~n": 2 },
    data: { books: [{ id: "b1" }, { id: "b2" }] },
  }

  it("parses tokens with their escapes", () => {
    expect(parsePointer("/a~1b/m~0n")).toEqual(["a/b", "m~n"])
    expect(parsePointer("")).toEqual([])
    expect(parsePointer("/")).toEqual([])
  })

  it("reads objects, arrays and escaped keys, and nothing past a miss", () => {
    expect(getAt(doc, "/ui/q")).toBe("dune")
    expect(getAt(doc, "/ui/a~1b")).toBe(1)
    expect(getAt(doc, "/ui/m~0n")).toBe(2)
    expect(getAt(doc, "/data/books/1/id")).toBe("b2")
    expect(getAt(doc, "/data/books/7/id")).toBeUndefined()
    expect(getAt(doc, "/data/books/x")).toBeUndefined()
    expect(getAt(doc, "/ui/q/deeper")).toBeUndefined()
    expect(getAt(doc, "/")).toBe(doc)
  })

  it("writes without touching the document it was given", () => {
    const next = setAt(doc, "/ui/status", ["read"]) as typeof doc
    expect(getAt(next, "/ui/status")).toEqual(["read"])
    expect(getAt(doc, "/ui/status")).toBeUndefined()
    expect(next.data).toBe(doc.data)
    expect(next.ui).not.toBe(doc.ui)
  })

  it("creates missing steps, an array where the next token is an index", () => {
    expect(setAt({}, "/form/name", "Ada")).toEqual({ form: { name: "Ada" } })
    expect(setAt({}, "/list/0", "x")).toEqual({ list: ["x"] })
    expect(setAt({ list: ["a"] }, "/list/-", "b")).toEqual({
      list: ["a", "b"],
    })
  })

  it("replaces the whole document at the root", () => {
    expect(setAt(doc, "/", { fresh: true })).toEqual({ fresh: true })
  })

  it("removes a key or an array item, and leaves a miss alone", () => {
    expect(removeAt(doc, "/ui/q")).toEqual({
      ...doc,
      ui: { "a/b": 1, "m~n": 2 },
    })
    expect(getAt(removeAt(doc, "/data/books/0"), "/data/books")).toEqual([
      { id: "b2" },
    ])
    expect(removeAt(doc, "/nope/deeper")).toBe(doc)
    expect(removeAt(doc, "/")).toEqual({})
  })

  it("resolves a relative path inside a template item's scope", () => {
    expect(resolvePath("/ui/q", "/data/books/0")).toBe("/ui/q")
    expect(resolvePath("properties/title", "/data/books/0")).toBe(
      "/data/books/0/properties/title"
    )
    expect(resolvePath("title")).toBe("/title")
  })

  it("says whether a write reaches a watched path", () => {
    expect(pathsOverlap("/ui/q", "/ui/q")).toBe(true)
    expect(pathsOverlap("/ui", "/ui/q")).toBe(true)
    expect(pathsOverlap("/ui/status/0", "/ui/status")).toBe(true)
    expect(pathsOverlap("/ui/form", "/ui/q")).toBe(false)
  })
})
