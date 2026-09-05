/** The catalog surface: listing the shipped closures (installed flag intact),
 * the two owner-gated doors (install for a provider, import for a sample, the
 * id percent-encoded as one segment either way), and the derived OAuth
 * callback URL the console shows the owner to register. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  catalogItemQueryOptions,
  catalogQueryOptions,
  importBundle,
  installBundle,
  oauthCallbackURL,
  OAUTH_CALLBACK_URL,
  type CatalogItem,
} from "./catalog"

function item(over: Partial<CatalogItem> = {}): CatalogItem {
  return {
    id: "providers.substrate.reamde.dev/google",
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    description: "Connects a Google account.",
    version: 1,
    tier: "provider",
    inputs: {
      client: {
        kind: "providers.substrate.reamde.dev/google/config",
        description: "The OAuth client record.",
      },
    },
    closure: { kinds: ["a", "b"], functions: ["c"] },
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
        JSON.stringify({
          items: [item(), item({ id: "x", installed: true })],
        }),
        { status: 200 }
      )
    )
    const res = await catalogQueryOptions.queryFn!({
      signal: new AbortController().signal,
    } as never)
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe("/api/v1/catalog")
    expect(init?.method).toBe("GET")
    expect(res).toHaveLength(2)
    expect(res[0].installed).toBe(false)
    expect(res[1].installed).toBe(true)
  })

  it("POSTs install to the catalog item's install path", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          id: "providers.substrate.reamde.dev/google",
          installed: true,
        }),
        {
          status: 200,
        }
      )
    )
    await installBundle("providers.substrate.reamde.dev/google")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/catalog/providers.substrate.reamde.dev%2Fgoogle/install"
    )
    expect(init?.method).toBe("POST")
  })

  it("POSTs import to the sample door, never the install one", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ id: "ada.example.com/tasks", installed: true }),
        { status: 200 }
      )
    )
    await importBundle("samples.substrate.reamde.dev/tasks")
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toBe(
      "/api/v1/catalog/samples.substrate.reamde.dev%2Ftasks/import"
    )
    expect(init?.method).toBe("POST")
  })

  it("percent-encodes a `/` in the catalog id as one segment (%2F)", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ installed: true }), { status: 200 })
    )
    await installBundle("acme.example.com/planner")
    expect(String(fetchMock.mock.calls[0][0])).toBe(
      "/api/v1/catalog/acme.example.com%2Fplanner/install"
    )
  })
})

describe("catalogItemQueryOptions", () => {
  it("shares the list cache and selects the entry by bundle id", () => {
    const items = [
      item(),
      item({ id: "slack.example.com/slack", name: "slack" }),
    ]
    const opts = catalogItemQueryOptions("slack.example.com/slack")
    expect(opts.queryKey).toEqual(catalogQueryOptions.queryKey)
    expect(opts.select?.(items)?.name).toBe("slack")
  })

  it("selects undefined when this repository's bundle is not a shipped closure", () => {
    const opts = catalogItemQueryOptions("appliedonly.example.com/local")
    expect(opts.select?.([item()])).toBeUndefined()
  })
})

describe("oauthCallbackURL", () => {
  it("is the substrate host's fixed callback, never the console origin", () => {
    expect(oauthCallbackURL()).toBe(
      "https://substrate.example.com/api/v1/oauth/callback"
    )
  })
  it("does not depend on window.location — a deployment setting is not guessed", () => {
    expect(oauthCallbackURL()).toBe(OAUTH_CALLBACK_URL)
    expect(
      OAUTH_CALLBACK_URL.startsWith("https://substrate.example.com/")
    ).toBe(true)
  })
})
