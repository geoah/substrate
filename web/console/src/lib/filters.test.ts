import { beforeEach, describe, expect, it } from "vitest"

import {
  canPrefix,
  decodeFilter,
  decodeFilters,
  displayValue,
  encodeFilter,
  loadBrowsePrefs,
  opFor,
  parseValueInput,
  saveBrowsePrefs,
  toRecordFilter,
  type ActiveFilter,
} from "./filters"
import type { DeclaredProperty } from "./definition"

const prop = (over: Partial<DeclaredProperty>): DeclaredProperty => ({
  name: "x",
  kind: "string",
  repeated: false,
  ...over,
})

describe("the URL codec", () => {
  it("round-trips a filter, hostile values included", () => {
    const cases: ActiveFilter[] = [
      { field: "prominence", op: "eq", value: "known" },
      { field: "emails", op: "contains", value: "a+b@x.dev" },
      { field: "name", op: "eq", value: "tilde ~ comma, colon:" },
      { field: "name", op: "prefix", value: "geo" },
      { field: "n", op: "eq", value: "" },
    ]
    for (const f of cases) {
      expect(decodeFilter(encodeFilter(f))).toEqual(f)
    }
  })

  it("drops malformed tokens instead of crashing the page", () => {
    expect(
      decodeFilters(["junk", "a~gt~1", "prominence~eq~known", null as never])
    ).toEqual([{ field: "prominence", op: "eq", value: "known" }])
    expect(decodeFilters(null)).toEqual([])
  })
})

describe("toRecordFilter", () => {
  it("writes eq for a single value", () => {
    expect(
      toRecordFilter([{ field: "prominence", op: "eq", value: "known" }], [])
    ).toEqual({ properties: { prominence: { eq: "known" } } })
  })

  it("folds a comma list into membership", () => {
    expect(
      toRecordFilter(
        [{ field: "prominence", op: "eq", value: "known,utility" }],
        []
      )
    ).toEqual({ properties: { prominence: { in: ["known", "utility"] } } })
  })

  it("writes contains for repeated properties", () => {
    expect(
      toRecordFilter(
        [{ field: "emails", op: "contains", value: "a@x.dev" }],
        [prop({ name: "emails", kind: "email", repeated: true })]
      )
    ).toEqual({ properties: { emails: { contains: "a@x.dev" } } })
  })

  it("coerces by declared kind so jsonb compares like for like", () => {
    expect(
      toRecordFilter(
        [
          { field: "number", op: "eq", value: "42" },
          { field: "draft", op: "eq", value: "false" },
        ],
        [
          prop({ name: "number", kind: "int" }),
          prop({ name: "draft", kind: "bool" }),
        ]
      )
    ).toEqual({ properties: { number: { eq: 42 }, draft: { eq: false } } })
  })

  it("leaves unparseable numbers as text rather than NaN", () => {
    expect(
      toRecordFilter(
        [{ field: "number", op: "eq", value: "abc" }],
        [prop({ name: "number", kind: "int" })]
      )
    ).toEqual({ properties: { number: { eq: "abc" } } })
  })

  it("is absent when no filter is active", () => {
    expect(toRecordFilter([], [])).toBeUndefined()
  })
})

describe("opFor", () => {
  it("repeated matches item-wise, scalars by equality", () => {
    expect(opFor(prop({ repeated: true }))).toBe("contains")
    expect(opFor(prop({}))).toBe("eq")
    expect(opFor(undefined)).toBe("eq")
  })
})

describe("the trailing-* wildcard", () => {
  it("turns geo* into a prefix filter on string-ish scalars", () => {
    expect(parseValueInput("geo*", prop({}))).toEqual({
      op: "prefix",
      value: "geo",
    })
    expect(parseValueInput("geo*", prop({ kind: "email" }))).toEqual({
      op: "prefix",
      value: "geo",
    })
  })

  it("leaves plain values, bare *, and non-string kinds alone", () => {
    expect(parseValueInput("geo", prop({}))).toEqual({
      op: "eq",
      value: "geo",
    })
    expect(parseValueInput("*", prop({}))).toEqual({ op: "eq", value: "*" })
    expect(parseValueInput("4*", prop({ name: "n", kind: "int" }))).toEqual({
      op: "eq",
      value: "4*",
    })
    expect(
      parseValueInput("a*", prop({ kind: "string", repeated: true }))
    ).toEqual({ op: "contains", value: "a*" })
  })

  it("writes prefix onto the wire, merged with any eq on the field", () => {
    expect(
      toRecordFilter([{ field: "name", op: "prefix", value: "geo" }], [])
    ).toEqual({ properties: { name: { prefix: "geo" } } })
    expect(
      toRecordFilter(
        [
          { field: "name", op: "prefix", value: "geo" },
          { field: "name", op: "eq", value: "george" },
        ],
        []
      )
    ).toEqual({ properties: { name: { prefix: "geo", eq: "george" } } })
  })

  it("wears the star back in display", () => {
    expect(displayValue({ field: "n", op: "prefix", value: "geo" })).toBe(
      "geo*"
    )
    expect(displayValue({ field: "n", op: "eq", value: "geo" })).toBe("geo")
  })

  it("only string-ish scalars are prefixable", () => {
    expect(canPrefix(prop({}))).toBe(true)
    expect(canPrefix(prop({ kind: "int" }))).toBe(false)
    expect(canPrefix(prop({ kind: "state" }))).toBe(false)
    expect(canPrefix(prop({ repeated: true }))).toBe(false)
  })
})

describe("browse prefs persistence", () => {
  beforeEach(() => localStorage.clear())

  it("round-trips filters and sort per collection", () => {
    saveBrowsePrefs("samples.substrate.reamde.dev/people", "people", {
      filter: ["prominence~eq~known"],
      sort: "name:asc",
    })
    expect(
      loadBrowsePrefs("samples.substrate.reamde.dev/people", "people")
    ).toEqual({
      filter: ["prominence~eq~known"],
      sort: "name:asc",
    })
    // another collection sees nothing
    expect(
      loadBrowsePrefs("samples.substrate.reamde.dev/people", "organizations")
    ).toBeNull()
  })

  it("an all-default save removes the stored entry (clear clears)", () => {
    saveBrowsePrefs("g", "t", { filter: ["a~eq~b"], sort: "name:asc" })
    saveBrowsePrefs("g", "t", { filter: [], sort: undefined })
    expect(loadBrowsePrefs("g", "t")).toBeNull()
    expect(localStorage.getItem("substrate.browse.g/t")).toBeNull()
  })

  it("survives garbage in the store", () => {
    localStorage.setItem("substrate.browse.g/t", "{not json")
    expect(loadBrowsePrefs("g", "t")).toBeNull()
    localStorage.setItem("substrate.browse.g/t", '{"filter":[1,2]}')
    expect(loadBrowsePrefs("g", "t")).toBeNull()
  })
})
