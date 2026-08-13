/** The catalog surface: listing the shipped closures (installed flag intact),
 * the owner-gated install POST (id percent-encoded as one segment), and the
 * derived OAuth callback URL the console shows the owner to register. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  catalogItemQueryOptions,
  catalogQueryOptions,
  importBundle,
  oauthCallbackURL,
  OAUTH_CALLBACK_URL,
  type CatalogItem,
} from "./catalog"

function item(over: Partial<CatalogItem> = {}): CatalogItem {
  return {
    id: "google.bundles.substrate.reamde.dev",
    name: "google",
    authority: "google.bundles.substrate.reamde.dev",
    description: "Connects a Google account.",
    version: "v1",
    inputs: {
      client: {
        kind: "google.bundles.substrate.reamde.dev/config",
        description: "The OAuth client record.",
      },
    },
    resources: { kinds: ["a", "b"], functions: ["c"] },
    installed: false,
    ...over,
  }
}

describe("catalog reads and writes", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("GETs the catalog and unwraps the entries with their installed flag", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ catalog: [item(), item({ id: "x", installed: true })] }),
        { status: 200 }
      )
    )
    const res = await catalogQueryOptions.queryFn!({
      signal: new AbortController().signal,
    } as never)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe("/api/v1/core.substrate.reamde.dev/catalog")
    expect(init?.method).toBe("GET")
    expect(res).toHaveLength(2)
    expect(res[0].installed).toBe(false)
    expect(res[1].installed).toBe(true)
  })

  it("POSTs install to the catalog item's install path", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ id: "google.bundles.substrate.reamde.dev", installed: true }), {
        status: 200,
      })
    )
    await importBundle("google.bundles.substrate.reamde.dev")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/core.substrate.reamde.dev/catalog/google.bundles.substrate.reamde.dev/install"
    )
    expect(init?.method).toBe("POST")
  })

  it("percent-encodes a `/` in the catalog id as one segment (%2F)", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ installed: true }), { status: 200 })
    )
    await importBundle("acme.bundles.substrate.reamde.dev/planner")
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      "/api/v1/core.substrate.reamde.dev/catalog/acme.bundles.substrate.reamde.dev%2Fplanner/install"
    )
  })
})

describe("catalogItemQueryOptions", () => {
  it("shares the list cache and selects the entry by bundle id", () => {
    const items = [item(), item({ id: "slack.bundles.substrate.reamde.dev", name: "slack" })]
    const opts = catalogItemQueryOptions("slack.bundles.substrate.reamde.dev")
    expect(opts.queryKey).toEqual(catalogQueryOptions.queryKey)
    expect(opts.select?.(items)?.name).toBe("slack")
  })

  it("selects undefined when this repository's bundle is not a shipped closure", () => {
    const opts = catalogItemQueryOptions("applied-only.bundles.substrate.reamde.dev")
    expect(opts.select?.([item()])).toBeUndefined()
  })
})

describe("oauthCallbackURL", () => {
  it("is the substrate host's fixed callback, never the console origin", () => {
    expect(oauthCallbackURL()).toBe(
      "https://substrate.example.com/api/v1/core.substrate.reamde.dev/oauth/callback"
    )
  })
  it("does not depend on window.location — a deployment setting is not guessed", () => {
    expect(oauthCallbackURL()).toBe(OAUTH_CALLBACK_URL)
    expect(OAUTH_CALLBACK_URL.startsWith("https://substrate.example.com/")).toBe(true)
  })
})
