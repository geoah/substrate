/** The catalog surface: listing the shipped closures (installed flag intact),
 * the two owner-gated doors (install for a provider, import for a sample, the
 * id percent-encoded as one segment either way), and the derived OAuth
 * callback URL the console shows the owner to register. */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  catalogQueryOptions,
  heldCopyOf,
  importBundle,
  installBundle,
  landedCatalog,
  oauthCallbackURL,
  OAUTH_CALLBACK_URL,
  rehomeAuthority,
  type CatalogItem,
} from "./catalog"
import type { BundleStatus } from "./types"

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
    closure: {
      traits: null,
      triggers: null,
      agents: null,
      mappings: null,
      records: null,
      kinds: ["a", "b"],
      functions: ["c"],
    },
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

/** A held copy meets its catalog entry by the origin its import stamped, and
 * by id only when it carries none: two publishers' samples of one package
 * word both land at `<home>/<package>`, so the id names both. */
describe("heldCopyOf", () => {
  const HOME = "ada.example.com"
  const tasks = (authority: string) =>
    item({
      id: `${authority}/tasks`,
      name: "tasks",
      authority,
      package: "tasks",
      tier: "sample",
    })
  const copy = (over: Partial<BundleStatus> = {}): BundleStatus => ({
    id: `${HOME}/tasks`,
    name: "tasks",
    authority: HOME,
    package: "tasks",
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 1,
    liveRecords: 0,
    ...over,
  })

  it("hands a stamped copy to the entry it names and to no other", () => {
    const held = [copy({ origin: "z.example.com/tasks" })]
    expect(heldCopyOf(tasks("z.example.com"), held, HOME)).toBe(held[0])
    expect(heldCopyOf(tasks("a.example.com"), held, HOME)).toBeUndefined()
  })

  it("finds a stamped copy wherever it sits", () => {
    const held = [copy({ id: `${HOME}/todo`, origin: "a.example.com/tasks" })]
    expect(heldCopyOf(tasks("a.example.com"), held, HOME)).toBe(held[0])
  })

  it("matches an unstamped copy by the id the entry lands at", () => {
    const held = [copy()]
    expect(heldCopyOf(tasks("a.example.com"), held, HOME)).toBe(held[0])
  })

  it("matches an unstamped copy held verbatim under the shipped id", () => {
    const held = [
      copy({ id: "a.example.com/tasks", authority: "a.example.com" }),
    ]
    expect(heldCopyOf(tasks("a.example.com"), held, HOME)).toBe(held[0])
  })

  it("matches a provider by its published id", () => {
    const held = [copy({ id: "providers.substrate.reamde.dev/google" })]
    expect(heldCopyOf(item(), held, HOME)).toBe(held[0])
    expect(heldCopyOf(item(), [copy()], HOME)).toBeUndefined()
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

/** A sample is authored under a placeholder authority and lands under this
 * repository's own, so the preview has to be read the way it will land:
 * anything less previews records this repository will never have. */
describe("landedCatalog", () => {
  const HOME = "ada.example.com"
  const sample = (): CatalogItem =>
    item({
      id: "samples.substrate.reamde.dev/pebble",
      name: "pebble",
      authority: "samples.substrate.reamde.dev",
      package: "pebble",
      tier: "sample",
      requires: ["samples.substrate.reamde.dev/tasks"],
      closure: {
        kinds: ["samples.substrate.reamde.dev/pebble/recording"],
        kindDescriptions: {
          "samples.substrate.reamde.dev/pebble/recording": "one capture",
        },
        kindPurposes: {
          "samples.substrate.reamde.dev/pebble/recording": "supporting",
        },
        traits: null,
        functions: ["samples.substrate.reamde.dev/pebble/ingest"],
        agents: null,
        mappings: null,
        triggers: ["pebble-webhook"],
        triggerCallables: {
          "pebble-webhook":
            "substrate.reamde.dev/core/function/samples.substrate.reamde.dev/pebble/ingest",
        },
        records: [
          {
            kind: "substrate.reamde.dev/core/trigger",
            id: "pebble-webhook",
          },
        ],
      },
    })

  it("rewrites the authority wherever it appears, not only at the front", () => {
    const landed = landedCatalog(sample(), HOME)
    expect(landed.authority).toBe(HOME)
    expect(landed.requires).toEqual([`${HOME}/tasks`])
    expect(landed.closure.kinds).toEqual([`${HOME}/pebble/recording`])
    expect(landed.closure.kindDescriptions).toEqual({
      [`${HOME}/pebble/recording`]: "one capture",
    })
    expect(landed.closure.kindPurposes).toEqual({
      [`${HOME}/pebble/recording`]: "supporting",
    })
    // A callable is a core kind reference with the sample's identity INSIDE
    // it: a prefix rewrite left the shipped authority sitting in the middle.
    expect(landed.closure.triggerCallables).toEqual({
      "pebble-webhook": `substrate.reamde.dev/core/function/${HOME}/pebble/ingest`,
    })
    // The id addresses the shipped closure, which is what the door is called
    // with, so it is the one thing left alone.
    expect(landed.id).toBe("samples.substrate.reamde.dev/pebble")
  })

  it("leaves a provider and a repository with no authority alone", () => {
    expect(landedCatalog(item(), HOME)).toEqual(item())
    expect(landedCatalog(sample(), "")).toEqual(sample())
  })
})

describe("rehomeAuthority", () => {
  it("moves a whole name and never one inside a longer one", () => {
    expect(
      rehomeAuthority(
        "samples.example.com/tasks",
        "samples.example.com",
        "ada.example.com"
      )
    ).toBe("ada.example.com/tasks")
    expect(
      rehomeAuthority(
        "notsamples.example.com/tasks",
        "samples.example.com",
        "ada.example.com"
      )
    ).toBe("notsamples.example.com/tasks")
    expect(
      rehomeAuthority(
        "samples.example.com.uk/tasks",
        "samples.example.com",
        "ada.example.com"
      )
    ).toBe("samples.example.com.uk/tasks")
  })

  it("moves every mention in one string", () => {
    expect(
      rehomeAuthority(
        "a/samples.example.com/one and samples.example.com/two",
        "samples.example.com",
        "ada.example.com"
      )
    ).toBe("a/ada.example.com/one and ada.example.com/two")
  })
})
