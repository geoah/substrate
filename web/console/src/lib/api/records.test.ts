import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  countRecords,
  createRecord,
  recordIdSegment,
  formatCount,
  groupReferencing,
  listPath,
  patchRecord,
  referencingRows,
  type ReferencingRow,
} from "./records"
import type { Page, SubstrateRecord } from "./types"

describe("listPath", () => {
  it("carries first, the opaque cursor verbatim, the kind inside the filter, and orderBy", () => {
    // The server's own keyset token — a JSON blob, base64url — resent as-is.
    const cursor = "eyJrIjpbIjIwMjYtMDgtMDYiXSwiaWQiOiJhYmMifQ"
    const path = listPath({
      authority: "samples.substrate.reamde.dev",
      package: "people",
      name: "person",
      first: 50,
      after: cursor,
      filter: { properties: { prominence: { eq: "known" } } },
      orderBy: "updatedAt:desc",
    })
    const url = new URL(path, "http://x")
    // One route for every list; the kind rides in the filter, not the path.
    expect(url.pathname).toBe("/api/v1/records")
    expect(url.searchParams.get("first")).toBe("50")
    expect(url.searchParams.get("after")).toBe(cursor)
    expect(JSON.parse(url.searchParams.get("filter")!)).toEqual({
      kinds: ["samples.substrate.reamde.dev/people/person"],
      properties: { prominence: { eq: "known" } },
    })
    expect(url.searchParams.get("orderBy")).toBe("updatedAt:desc")
    // `withEdges` is gone with the edges it asked for: a pointer at another
    // record is a property, and every read already carries the properties.
    expect(url.searchParams.has("withEdges")).toBe(false)
  })

  it("names a collection's kind as its one filter.kinds entry", () => {
    const url = new URL(
      listPath({
        authority: "samples.substrate.reamde.dev",
        package: "tasks",
        name: "task",
      }),
      "http://x"
    )
    expect(url.pathname).toBe("/api/v1/records")
    expect(JSON.parse(url.searchParams.get("filter")!)).toEqual({
      kinds: ["samples.substrate.reamde.dev/tasks/task"],
    })
  })

  it("lists several kinds at once, or every kind with none", () => {
    const two = new URL(
      listPath({
        kinds: [
          "substrate.reamde.dev/core/setting",
          "substrate.reamde.dev/core/secret",
        ],
        first: 500,
      }),
      "http://x"
    )
    expect(JSON.parse(two.searchParams.get("filter")!)).toEqual({
      kinds: [
        "substrate.reamde.dev/core/setting",
        "substrate.reamde.dev/core/secret",
      ],
    })
    // No kind and nothing else to narrow by: no filter at all.
    const all = new URL(listPath({ kinds: [], first: 25 }), "http://x")
    expect(all.searchParams.has("filter")).toBe(false)
    // An implements arm alone is a legal cross-kind read.
    const trait = new URL(
      listPath({
        kinds: [],
        filter: { implements: "x.dev/core/accountconfig" },
      }),
      "http://x"
    )
    expect(JSON.parse(trait.searchParams.get("filter")!)).toEqual({
      implements: "x.dev/core/accountconfig",
    })
  })

  it("omits what is not asked: no cursor at page one, no empty arms, and expand only when named", () => {
    const url = new URL(
      listPath({
        authority: "g",
        package: "k",
        name: "p",
        first: 25,
        filter: { properties: {}, labels: {} },
      }),
      "http://x"
    )
    expect(url.searchParams.has("after")).toBe(false)
    expect(JSON.parse(url.searchParams.get("filter")!)).toEqual({
      kinds: ["g/k/p"],
    })
    expect(url.searchParams.has("expand")).toBe(false)
    expect(url.searchParams.has("withEdges")).toBe(false)
    const expanded = new URL(
      listPath({ authority: "g", package: "k", name: "p", expand: ["a", "b"] }),
      "http://x"
    )
    expect(expanded.searchParams.get("expand")).toBe("a,b")
  })
})

describe("recordIdSegment", () => {
  it("percent-encodes a `/` so a slash-bearing id is one segment (%2F)", () => {
    // A declaration record's id IS a kind reference; the API decodes once.
    expect(recordIdSegment("a/b")).toBe("a%2Fb")
    expect(recordIdSegment("samples.substrate.reamde.dev/people/person")).toBe(
      "samples.substrate.reamde.dev%2Fpeople%2Fperson"
    )
  })

  it("is encodeURIComponent — the v1 server decodes the segment exactly once", () => {
    expect(recordIdSegment("gcal-alice@example.com")).toBe(
      "gcal-alice%40example.com"
    )
    expect(recordIdSegment("a?b#c")).toBe("a%3Fb%23c")
    expect(recordIdSegment("a b")).toBe("a%20b")
  })

  it("percent-escapes multibyte characters byte-wise", () => {
    expect(recordIdSegment("aé")).toBe("a%C3%A9")
  })
})

describe("record writes (integrations flow)", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("createRecord POSTs to the records route with the kind in the body, no id (the substrate mints one)", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "abc", properties: {} }), {
        status: 201,
      })
    )
    await createRecord("providers.substrate.reamde.dev", "google", "accounts", {
      properties: {
        email: "alice@example.com",
        enabledContacts: true,
        syncFrequency: "daily",
      },
    })
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe("/api/v1/records")
    expect(init?.method).toBe("POST")
    expect(JSON.parse(String(init?.body))).toEqual({
      kind: "providers.substrate.reamde.dev/google/accounts",
      properties: {
        email: "alice@example.com",
        enabledContacts: true,
        syncFrequency: "daily",
      },
    })
  })

  it("createRecord with a chosen id PUTs at the record path, since the POST refuses an id", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "chosen", properties: {} }), {
        status: 201,
      })
    )
    await createRecord("providers.substrate.reamde.dev", "google", "accounts", {
      id: "chosen",
      properties: { email: "alice@example.com" },
    })
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/providers.substrate.reamde.dev/google/accounts/chosen"
    )
    expect(init?.method).toBe("PUT")
    expect(JSON.parse(String(init?.body))).toEqual({
      properties: { email: "alice@example.com" },
    })
  })

  it("patchRecord PATCHes the addressed record's properties", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "abc", properties: {} }), {
        status: 200,
      })
    )
    await patchRecord(
      "providers.substrate.reamde.dev",
      "google",
      "accounts",
      "abc",
      {
        properties: { syncFrequency: "hourly" },
      }
    )
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/providers.substrate.reamde.dev/google/accounts/abc"
    )
    expect(init?.method).toBe("PATCH")
    expect(JSON.parse(String(init?.body))).toEqual({
      properties: { syncFrequency: "hourly" },
    })
  })
})

describe("countRecords (bounded keyset walk)", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function page(n: number, cursor?: string): Response {
    return new Response(
      JSON.stringify({
        records: Array.from({ length: n }, (_, i) => ({ id: String(i) })),
        cursor,
      }),
      { status: 200 }
    )
  }

  it("sums pages, resends the server cursor verbatim, and stops when it is omitted", async () => {
    fetchMock
      .mockResolvedValueOnce(page(500, "CUR1"))
      .mockResolvedValueOnce(page(120))
    const count = await countRecords("g.dev", "k", "things", undefined)
    expect(count).toEqual({ value: 620, capped: false })
    // Page one asks with no cursor; page two resends the returned one verbatim.
    expect(String(fetchMock.mock.calls[0][0])).not.toContain("after=")
    expect(String(fetchMock.mock.calls[1][0])).toContain("after=CUR1")
  })

  it("answers a single cursorless page exactly", async () => {
    fetchMock.mockResolvedValueOnce(page(7))
    expect(await countRecords("g.dev", "k", "things", undefined)).toEqual({
      value: 7,
      capped: false,
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("caps a collection that outruns the ceiling", async () => {
    // Every page returns a cursor, so the walk hits its 20-page ceiling.
    fetchMock.mockImplementation(async () => page(500, "MORE"))
    const count = await countRecords("g.dev", "k", "things", undefined)
    expect(count.capped).toBe(true)
    expect(count.value).toBe(500 * 20)
    expect(fetchMock).toHaveBeenCalledTimes(20)
  })
})

describe("formatCount", () => {
  it("renders an exact size plainly and a capped one with a trailing +", () => {
    expect(formatCount({ value: 1234, capped: false })).toBe("1,234")
    expect(formatCount({ value: 10000, capped: true })).toBe("10,000+")
  })
})

describe("referencingRows", () => {
  const rec = (kind: string, id: string): SubstrateRecord => ({
    id,
    kind,
    properties: {},
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  })

  it("joins each record with every site matches lists for it", () => {
    const page: Page = {
      records: [rec("g.dev/k/pr", "1"), rec("g.dev/k/pr", "2")],
      head: 9,
      generation: "g",
      matches: {
        "g.dev/k/pr/1": [{ property: "author" }, { property: "reviewer" }],
        "g.dev/k/pr/2": [{ property: "steps", path: "steps.owner" }],
      },
    }
    expect(
      referencingRows(page).map((r) => [r.record.id, r.property, r.path])
    ).toEqual([
      ["1", "author", undefined],
      ["1", "reviewer", undefined],
      ["2", "steps", "steps.owner"],
    ])
  })

  it("keeps a record the page carries with no match entry", () => {
    const page: Page = {
      records: [rec("g.dev/k/pr", "1")],
      head: 1,
      generation: "g",
    }
    expect(referencingRows(page)).toHaveLength(1)
  })
})

describe("groupReferencing", () => {
  const row = (property: string, kind: string, id: string): ReferencingRow => ({
    property,
    record: {
      id,
      kind,
      properties: {},
      labels: {},
      version: 1,
      createdAt: "",
      updatedAt: "",
    },
  })

  it("folds rows into property × kind buckets, kind then property", () => {
    const groups = groupReferencing([
      row("author", "providers.substrate.reamde.dev/github/pr", "1"),
      row("author", "providers.substrate.reamde.dev/github/pr", "2"),
      row("author", "providers.substrate.reamde.dev/github/issue", "3"),
      row("subject", "providers.substrate.reamde.dev/google/contact", "4"),
    ])
    expect(groups.map((g) => [g.property, g.kind, g.rows.length])).toEqual([
      ["author", "providers.substrate.reamde.dev/github/issue", 1],
      ["author", "providers.substrate.reamde.dev/github/pr", 2],
      ["subject", "providers.substrate.reamde.dev/google/contact", 1],
    ])
  })

  it("collects a bucket whatever order the page interleaves it in", () => {
    // One source record's two properties come back adjacent (two rows of one
    // record) and the two sources of one property do not. An adjacency fold
    // would emit `author` twice.
    const groups = groupReferencing([
      row("author", "providers.substrate.reamde.dev/github/pr", "1"),
      row("reviewer", "providers.substrate.reamde.dev/github/pr", "1"),
      row("author", "providers.substrate.reamde.dev/github/pr", "2"),
      row("reviewer", "providers.substrate.reamde.dev/github/pr", "2"),
    ])
    expect(groups.map((g) => [g.property, g.rows.length])).toEqual([
      ["author", 2],
      ["reviewer", 2],
    ])
  })

  it("keeps buckets whole across page concatenation", () => {
    const pageOne = [
      row("author", "providers.substrate.reamde.dev/github/pr", "1"),
    ]
    const pageTwo = [
      row("author", "providers.substrate.reamde.dev/github/pr", "2"),
    ]
    const groups = groupReferencing([...pageOne, ...pageTwo])
    expect(groups).toHaveLength(1)
    expect(groups[0].rows).toHaveLength(2)
  })
})
