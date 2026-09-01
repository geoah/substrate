import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  countRecords,
  createRecord,
  recordIdSegment,
  formatCount,
  groupIncoming,
  listPath,
  patchRecord,
} from "./records"
import type { IncomingReference } from "./types"

describe("listPath", () => {
  it("carries first, the opaque cursor verbatim, filter and orderBy", () => {
    // The server's own keyset token — a JSON blob, base64url — resent as-is.
    const cursor = "eyJrIjpbIjIwMjYtMDgtMDYiXSwiaWQiOiJhYmMifQ"
    const path = listPath({
      authority: "people.substrate.reamde.dev",
      name: "person",
      first: 50,
      after: cursor,
      filter: { properties: { prominence: { eq: "known" } } },
      orderBy: "updatedAt:desc",
    })
    const url = new URL(path, "http://x")
    expect(url.pathname).toBe("/api/v1/people.substrate.reamde.dev/person")
    expect(url.searchParams.get("first")).toBe("50")
    expect(url.searchParams.get("after")).toBe(cursor)
    expect(JSON.parse(url.searchParams.get("filter")!)).toEqual({
      properties: { prominence: { eq: "known" } },
    })
    expect(url.searchParams.get("orderBy")).toBe("updatedAt:desc")
    // `withEdges` is gone with the edges it asked for: a pointer at another
    // record is a property, and every read already carries the properties.
    expect(url.searchParams.has("withEdges")).toBe(false)
  })

  it("addresses a collection by its authority and kind name", () => {
    const url = new URL(
      listPath({ authority: "tasks.substrate.reamde.dev", name: "task" }),
      "http://x"
    )
    expect(url.pathname).toBe("/api/v1/tasks.substrate.reamde.dev/task")
  })

  it("omits what is not asked: no cursor at page one, no empty filter", () => {
    const url = new URL(
      listPath({ authority: "g", name: "p", first: 25, filter: {} }),
      "http://x"
    )
    expect(url.searchParams.has("after")).toBe(false)
    expect(url.searchParams.has("filter")).toBe(false)
    expect(url.searchParams.has("withEdges")).toBe(false)
  })
})

describe("recordIdSegment", () => {
  it("percent-encodes a `/` so a slash-bearing id is one segment (%2F)", () => {
    // A declaration record's id IS a kind reference; the API decodes once.
    expect(recordIdSegment("a/b")).toBe("a%2Fb")
    expect(recordIdSegment("people.substrate.reamde.dev/person")).toBe(
      "people.substrate.reamde.dev%2Fperson"
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

  it("createRecord POSTs the properties to the collection, no id (the substrate mints one)", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "abc", properties: {} }), {
        status: 201,
      })
    )
    await createRecord("google.bundles.substrate.reamde.dev", "accounts", {
      properties: {
        email: "alice@example.com",
        enabledContacts: true,
        syncFrequency: "daily",
      },
    })
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/google.bundles.substrate.reamde.dev/accounts"
    )
    expect(init?.method).toBe("POST")
    expect(JSON.parse(String(init?.body))).toEqual({
      properties: {
        email: "alice@example.com",
        enabledContacts: true,
        syncFrequency: "daily",
      },
    })
  })

  it("patchRecord PATCHes the addressed record's properties", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "abc", properties: {} }), {
        status: 200,
      })
    )
    await patchRecord(
      "google.bundles.substrate.reamde.dev",
      "accounts",
      "abc",
      {
        properties: { syncFrequency: "hourly" },
      }
    )
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/google.bundles.substrate.reamde.dev/accounts/abc"
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
    const count = await countRecords("g.dev", "things", undefined)
    expect(count).toEqual({ value: 620, capped: false })
    // Page one asks with no cursor; page two resends the returned one verbatim.
    expect(String(fetchMock.mock.calls[0][0])).not.toContain("after=")
    expect(String(fetchMock.mock.calls[1][0])).toContain("after=CUR1")
  })

  it("answers a single cursorless page exactly", async () => {
    fetchMock.mockResolvedValueOnce(page(7))
    expect(await countRecords("g.dev", "things", undefined)).toEqual({
      value: 7,
      capped: false,
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("caps a collection that outruns the ceiling", async () => {
    // Every page returns a cursor, so the walk hits its 20-page ceiling.
    fetchMock.mockImplementation(async () => page(500, "MORE"))
    const count = await countRecords("g.dev", "things", undefined)
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

describe("groupIncoming", () => {
  const row = (
    property: string,
    kind: string,
    id: string
  ): IncomingReference => ({
    property,
    from: { id, kind },
  })

  it("folds rows into property × kind buckets, kind then property", () => {
    const groups = groupIncoming([
      row("author", "github.bundles.substrate.reamde.dev/pr", "1"),
      row("author", "github.bundles.substrate.reamde.dev/pr", "2"),
      row("author", "github.bundles.substrate.reamde.dev/issue", "3"),
      row("subject", "google.bundles.substrate.reamde.dev/contact", "4"),
    ])
    expect(groups.map((g) => [g.property, g.kind, g.rows.length])).toEqual([
      ["author", "github.bundles.substrate.reamde.dev/issue", 1],
      ["author", "github.bundles.substrate.reamde.dev/pr", 2],
      ["subject", "google.bundles.substrate.reamde.dev/contact", 1],
    ])
  })

  it("collects a bucket the refs order interleaves", () => {
    // The index walks (src_kind, src, property, …), so one source record's two
    // properties come back adjacent and the two sources of one property do
    // not. An adjacency fold would emit `author` twice.
    const groups = groupIncoming([
      row("author", "github.bundles.substrate.reamde.dev/pr", "1"),
      row("reviewer", "github.bundles.substrate.reamde.dev/pr", "1"),
      row("author", "github.bundles.substrate.reamde.dev/pr", "2"),
      row("reviewer", "github.bundles.substrate.reamde.dev/pr", "2"),
    ])
    expect(groups.map((g) => [g.property, g.rows.length])).toEqual([
      ["author", 2],
      ["reviewer", 2],
    ])
  })

  it("keeps buckets whole across page concatenation", () => {
    const pageOne = [
      row("author", "github.bundles.substrate.reamde.dev/pr", "1"),
    ]
    const pageTwo = [
      row("author", "github.bundles.substrate.reamde.dev/pr", "2"),
    ]
    const groups = groupIncoming([...pageOne, ...pageTwo])
    expect(groups).toHaveLength(1)
    expect(groups[0].rows).toHaveLength(2)
  })
})
