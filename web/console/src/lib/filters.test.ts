// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"

import {
  canMatch,
  canPrefix,
  choiceWord,
  decodeFilter,
  decodeFilters,
  displayValue,
  encodeFilter,
  filterValueText,
  isChoiceField,
  loadBrowsePrefs,
  opFor,
  parseValueInput,
  picksMany,
  saveBrowsePrefs,
  splitReferenceIds,
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
      { field: "email", op: "prefix", value: "geo" },
      { field: "name", op: "match", value: `rack lay* -"weekly sync"` },
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

  it("writes match verbatim: the server parses the grammar", () => {
    expect(
      toRecordFilter(
        [{ field: "notes", op: "match", value: `rack lay* -"weekly sync"` }],
        [prop({ name: "notes", kind: "markdown" })]
      )
    ).toEqual({ properties: { notes: { match: `rack lay* -"weekly sync"` } } })
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

describe("free text on a text field is a full-text match", () => {
  it("words, stars, phrases and exclusions ride to the server as match", () => {
    for (const raw of [
      "rack",
      "lay*",
      "*lay*",
      `"rack layout"`,
      "rack -lunch",
    ]) {
      expect(parseValueInput(raw, prop({}))).toEqual({
        op: "match",
        value: raw,
      })
    }
    expect(parseValueInput("rack", prop({ kind: "text" }))).toEqual({
      op: "match",
      value: "rack",
    })
    expect(parseValueInput("rack", prop({ kind: "markdown" }))).toEqual({
      op: "match",
      value: "rack",
    })
    // a repeated string matches over its items
    expect(parseValueInput("rack", prop({ repeated: true }))).toEqual({
      op: "match",
      value: "rack",
    })
    // an undeclared field is text
    expect(parseValueInput("rack", undefined)).toEqual({
      op: "match",
      value: "rack",
    })
  })

  it("a leading = asks for the exact value in the field's natural op", () => {
    expect(parseValueInput("=George", prop({}))).toEqual({
      op: "eq",
      value: "George",
    })
    expect(parseValueInput("=a,b", prop({}))).toEqual({
      op: "eq",
      value: "a,b",
    })
    expect(parseValueInput("=tag", prop({ repeated: true }))).toEqual({
      op: "contains",
      value: "tag",
    })
    // a bare = is a word to match, not an empty exact value
    expect(parseValueInput("=", prop({}))).toEqual({ op: "match", value: "=" })
  })

  it("only string, text and markdown fields match", () => {
    expect(canMatch(prop({}))).toBe(true)
    expect(canMatch(prop({ kind: "text" }))).toBe(true)
    expect(canMatch(prop({ kind: "markdown", repeated: true }))).toBe(true)
    expect(canMatch(undefined)).toBe(true)
    expect(canMatch(prop({ kind: "email" }))).toBe(false)
    expect(canMatch(prop({ kind: "enum" }))).toBe(false)
    expect(canMatch(prop({ kind: "int" }))).toBe(false)
    expect(canMatch(prop({ kind: "state" }))).toBe(false)
  })
})

describe("the trailing-* wildcard on identifier fields", () => {
  it("turns geo* into a prefix filter on an email, url or phone", () => {
    expect(parseValueInput("geo*", prop({ kind: "email" }))).toEqual({
      op: "prefix",
      value: "geo",
    })
    expect(parseValueInput("https://x*", prop({ kind: "url" }))).toEqual({
      op: "prefix",
      value: "https://x",
    })
  })

  it("leaves plain values, bare *, and non-string kinds alone", () => {
    expect(parseValueInput("geo", prop({ kind: "email" }))).toEqual({
      op: "eq",
      value: "geo",
    })
    expect(parseValueInput("*", prop({ kind: "email" }))).toEqual({
      op: "eq",
      value: "*",
    })
    expect(parseValueInput("4*", prop({ name: "n", kind: "int" }))).toEqual({
      op: "eq",
      value: "4*",
    })
    expect(
      parseValueInput("a*", prop({ kind: "email", repeated: true }))
    ).toEqual({ op: "contains", value: "a*" })
  })

  it("writes prefix onto the wire, merged with any eq on the field", () => {
    expect(
      toRecordFilter([{ field: "email", op: "prefix", value: "geo" }], [])
    ).toEqual({ properties: { email: { prefix: "geo" } } })
    expect(
      toRecordFilter(
        [
          { field: "email", op: "prefix", value: "geo" },
          { field: "email", op: "eq", value: "george@x.dev" },
        ],
        []
      )
    ).toEqual({
      properties: { email: { prefix: "geo", eq: "george@x.dev" } },
    })
  })

  it("only identifier-shaped scalars are prefixable", () => {
    expect(canPrefix(prop({ kind: "email" }))).toBe(true)
    expect(canPrefix(prop({ kind: "url" }))).toBe(true)
    expect(canPrefix(prop({ kind: "phone" }))).toBe(true)
    expect(canPrefix(prop({}))).toBe(false)
    expect(canPrefix(prop({ kind: "int" }))).toBe(false)
    expect(canPrefix(prop({ kind: "state" }))).toBe(false)
    expect(canPrefix(prop({ kind: "email", repeated: true }))).toBe(false)
    expect(canPrefix(undefined)).toBe(false)
  })
})

describe("displayValue reads back what parseValueInput takes", () => {
  it("a prefix wears its star, an exact text value its =, a match is bare", () => {
    expect(
      displayValue(
        { field: "e", op: "prefix", value: "geo" },
        prop({ kind: "email" })
      )
    ).toBe("geo*")
    expect(displayValue({ field: "n", op: "eq", value: "geo" }, prop({}))).toBe(
      "=geo"
    )
    expect(
      displayValue(
        { field: "tags", op: "contains", value: "x" },
        prop({ repeated: true })
      )
    ).toBe("=x")
    expect(
      displayValue({ field: "n", op: "match", value: "geo*" }, prop({}))
    ).toBe("geo*")
    // a non-text field's exact value is shown as typed
    expect(
      displayValue(
        { field: "prominence", op: "eq", value: "known" },
        prop({ kind: "state" })
      )
    ).toBe("known")
    expect(
      displayValue({ field: "n", op: "eq", value: "1" }, prop({ kind: "int" }))
    ).toBe("1")
  })

  it("round-trips through parseValueInput", () => {
    for (const [f, p] of [
      [{ field: "n", op: "eq", value: "George" }, prop({})],
      [{ field: "n", op: "match", value: `lay* -"a b"` }, prop({})],
      [{ field: "e", op: "prefix", value: "geo" }, prop({ kind: "email" })],
      [{ field: "t", op: "contains", value: "x" }, prop({ repeated: true })],
      [{ field: "s", op: "eq", value: "known" }, prop({ kind: "state" })],
    ] as const) {
      expect(parseValueInput(displayValue(f, p), p)).toEqual({
        op: f.op,
        value: f.value,
      })
    }
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

  it("remembers the tree switched off, and only off", () => {
    saveBrowsePrefs("g", "t", { nest: false })
    expect(loadBrowsePrefs("g", "t")).toEqual({ nest: false })
    // on is the default, so it is nothing to store
    saveBrowsePrefs("g", "t", { nest: true })
    expect(loadBrowsePrefs("g", "t")).toBeNull()
    saveBrowsePrefs("g", "t", { filter: ["a~eq~b"], nest: undefined })
    expect(loadBrowsePrefs("g", "t")).toEqual({ filter: ["a~eq~b"] })
  })

  it("survives garbage in the store", () => {
    localStorage.setItem("substrate.browse.g/t", "{not json")
    expect(loadBrowsePrefs("g", "t")).toBeNull()
    localStorage.setItem("substrate.browse.g/t", '{"filter":[1,2]}')
    expect(loadBrowsePrefs("g", "t")).toBeNull()
  })
})

describe("a reference filter", () => {
  const assignee = prop({ name: "assignee", kind: "reference", to: "person" })
  const owners = prop({
    name: "owners",
    kind: "reference",
    to: "person",
    repeated: true,
  })

  it("takes eq whether or not the property repeats, so several ids fold to in", () => {
    // The engine reads eq, contains and in on a pointer alike, and `in` is the
    // only several-values form on one property: a repeated reference that
    // took `contains` would send its comma-joined ids as one literal value.
    expect(opFor(assignee)).toBe("eq")
    expect(opFor(owners)).toBe("eq")
    expect(
      toRecordFilter(
        [{ field: "owners", op: "eq", value: "ada,grace" }],
        [owners]
      )
    ).toEqual({ properties: { owners: { in: ["ada", "grace"] } } })
    expect(
      toRecordFilter(
        [{ field: "assignee", op: "eq", value: "ada" }],
        [assignee]
      )
    ).toEqual({ properties: { assignee: { eq: "ada" } } })
  })

  it("is typed as the exact pointer, never matched as words", () => {
    expect(canMatch(assignee)).toBe(false)
    expect(canPrefix(assignee)).toBe(false)
    expect(parseValueInput("acme.test/people/person/ada", assignee)).toEqual({
      op: "eq",
      value: "acme.test/people/person/ada",
    })
    expect(
      displayValue({ field: "assignee", op: "eq", value: "ada" }, assignee)
    ).toBe("ada")
  })

  it("reads its ids back out of the comma-joined value, in order", () => {
    expect(splitReferenceIds("grace, ada,,")).toEqual(["grace", "ada"])
    expect(splitReferenceIds("")).toEqual([])
  })
})

describe("picked values", () => {
  const status: DeclaredProperty = {
    name: "status",
    kind: "state",
    repeated: false,
    states: ["proposed", "open"],
  }
  const priority: DeclaredProperty = {
    name: "priority",
    kind: "enum",
    repeated: false,
    values: [
      { value: "high", label: "Urgent" },
      { value: "inProgress", label: "" },
    ],
  }
  const tags: DeclaredProperty = { ...priority, name: "tags", repeated: true }
  const flag: DeclaredProperty = { name: "flag", kind: "bool", repeated: false }
  const note: DeclaredProperty = {
    name: "note",
    kind: "string",
    repeated: false,
  }

  it("knows a declared set from typed text", () => {
    expect([status, priority, flag, note].map(isChoiceField)).toEqual([
      true,
      true,
      true,
      false,
    ])
    expect(isChoiceField({ ...status, states: [] })).toBe(false)
  })

  it("picks several only where the wire folds them to any of", () => {
    expect([status, priority, tags, flag].map(picksMany)).toEqual([
      true,
      true,
      false,
      false,
    ])
  })

  it("says each value in the grid's words", () => {
    expect(choiceWord("proposed", status, true)).toBe("Suggested")
    expect(choiceWord("proposed", status, false)).toBe("proposed")
    expect(choiceWord("high", priority, true)).toBe("Urgent")
    expect(choiceWord("inProgress", priority, true)).toBe("In progress")
    expect(choiceWord("false", flag, true)).toBe("No")
  })

  it("reads an applied control in words, typed text as typed", () => {
    expect(
      filterValueText(
        { field: "status", op: "eq", value: "proposed,open" },
        status,
        true
      )
    ).toBe("Suggested, Open")
    expect(
      filterValueText({ field: "note", op: "eq", value: "a,b" }, note, true)
    ).toBe("=a, b")
  })
})
