import { QueryClient } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  discoveryQueryOptions,
  fetchAuthPolicy,
  fetchDoorFeature,
  hasFeature,
  resetAuthPolicy,
} from "./discovery"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

describe("fetchAuthPolicy", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    resetAuthPolicy()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("reads the policy off GET /.well-known/substrate/server.json, anonymously", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        registration: { inviteRequired: false, totpRequired: false },
      })
    )
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: false,
      totpRequired: false,
    })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/.well-known/substrate/server.json")
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("shares one in-flight request, and asks again once it has settled", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { registration: { totpRequired: false } })
    )
    // Doors mounting together ask once between them.
    await Promise.all([fetchAuthPolicy(), fetchAuthPolicy()])
    expect(fetchMock.mock.calls.length).toBe(1)
    // A door mounting after that asks again: a dev substrate is restarted from
    // one door to the other, and a tab holding the old answer would send no
    // code to a substrate that wants one.
    fetchMock.mockResolvedValue(
      jsonResponse(200, { registration: { totpRequired: true } })
    )
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: true,
      totpRequired: true,
    })
    expect(fetchMock.mock.calls.length).toBe(2)
  })

  it("requires both when discovery says nothing about them", async () => {
    // An older substrate serves no `registration` block; the strict shape is
    // the only safe reading of silence.
    fetchMock.mockResolvedValue(jsonResponse(200, { versions: [] }))
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: true,
      totpRequired: true,
    })
  })

  it("reads the two answers apart", async () => {
    // A gated door with the factor off, and an open door with it on, are
    // both deployments; neither answer implies the other.
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        registration: { inviteRequired: true, totpRequired: false },
      })
    )
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: true,
      totpRequired: false,
    })
  })

  it("tells a server with no name host from one that says nothing", async () => {
    // A server with no public URL names an empty host: a name is typed
    // whole. An older one sends no field, and completes under the address
    // it was reached at.
    fetchMock.mockResolvedValue(
      jsonResponse(200, { registration: { nameHost: "" } })
    )
    expect((await fetchAuthPolicy()).nameHost).toBe("")
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        registration: { nameHost: "substrate.example.com" },
      })
    )
    expect((await fetchAuthPolicy()).nameHost).toBe("substrate.example.com")
    fetchMock.mockResolvedValue(jsonResponse(200, { registration: {} }))
    expect((await fetchAuthPolicy()).nameHost).toBeUndefined()
  })

  it("falls back to strict on an unreachable substrate, and asks again", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("offline"))
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: true,
      totpRequired: true,
    })
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        registration: { inviteRequired: false, totpRequired: false },
      })
    )
    expect(await fetchAuthPolicy()).toEqual({
      inviteRequired: false,
      totpRequired: false,
    })
  })
})

describe("fetchDoorFeature", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    resetAuthPolicy()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("reads a feature off the same request the door's policy reads", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        registration: { totpRequired: false },
        features: [
          { name: "operatorDefaults", stability: "experimental", revision: 1 },
        ],
      })
    )
    const [policy, listed, other] = await Promise.all([
      fetchAuthPolicy(),
      fetchDoorFeature("operatorDefaults"),
      fetchDoorFeature("somethingElse"),
    ])
    expect(policy.totpRequired).toBe(false)
    expect(listed).toBe(true)
    expect(other).toBe(false)
    expect(fetchMock.mock.calls.length).toBe(1)
  })

  it("reads an unreachable discovery as not listing it", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("offline"))
    expect(await fetchDoorFeature("operatorDefaults")).toBe(false)
  })

  it("reads a document with no features as listing none", () => {
    expect(hasFeature({}, "operatorDefaults")).toBe(false)
    expect(hasFeature(null, "operatorDefaults")).toBe(false)
    expect(
      hasFeature(
        {
          features: [
            {
              name: "operatorDefaults",
              stability: "experimental",
              revision: 1,
            },
          ],
        },
        "operatorDefaults"
      )
    ).toBe(true)
  })
})

describe("hasFeature with a minimum revision", () => {
  const doc = {
    features: [
      { name: "records", stability: "stable", revision: 3 },
      {
        name: "auth",
        stability: "beta",
        revision: 1,
        notice: "Before 1.0, sign-in names a person.",
      },
      // What a server older than the count sends.
      { name: "triggers", stability: "stable" },
    ],
  }

  it("holds at the revision asked for and at any below it", () => {
    expect(hasFeature(doc, "records", 3)).toBe(true)
    expect(hasFeature(doc, "records", 2)).toBe(true)
    expect(hasFeature(doc, "records")).toBe(true)
  })

  it("fails above the revision the server is at", () => {
    expect(hasFeature(doc, "records", 4)).toBe(false)
    expect(hasFeature(doc, "auth", 2)).toBe(false)
  })

  it("fails for a feature the server does not list, whatever is asked", () => {
    expect(hasFeature(doc, "units")).toBe(false)
    expect(hasFeature(doc, "units", 0)).toBe(false)
    expect(hasFeature(doc, "units", 1)).toBe(false)
  })

  // A caller asking for a revision is about to send something that revision
  // added; a server that counts none has none of it, and still lists the
  // feature for a caller that asks for nothing more.
  it("reads an entry without a revision as revision 0", () => {
    expect(hasFeature(doc, "triggers")).toBe(true)
    expect(hasFeature(doc, "triggers", 0)).toBe(true)
    expect(hasFeature(doc, "triggers", 1)).toBe(false)
  })

  it("reads a revision that is not a number as none", () => {
    const odd = {
      features: [
        { name: "records", stability: "stable", revision: "3" as never },
      ],
    }
    expect(hasFeature(odd, "records")).toBe(true)
    expect(hasFeature(odd, "records", 1)).toBe(false)
  })

  it("lists a feature whose revision is below 0", () => {
    const negative = {
      features: [{ name: "records", stability: "stable", revision: -1 }],
    }
    expect(hasFeature(negative, "records")).toBe(true)
    expect(hasFeature(negative, "records", 1)).toBe(false)
  })
})

describe("discoveryQueryOptions", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("reads the whole document anonymously, even with a session stored", async () => {
    const doc = {
      server: { version: "1.2.3" },
      features: [
        { name: "triggers", stability: "stable", revision: 1 },
        {
          name: "auth",
          stability: "beta",
          revision: 1,
          notice:
            "Before 1.0, sign-in names a person, who then picks a repository.",
        },
      ],
      surfaces: { rest: { endpoint: "/api/v1" } },
      registration: { inviteRequired: false, totpRequired: true },
    }
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(jsonResponse(200, doc))
    )
    vi.stubGlobal("fetch", fetchMock)
    vi.stubGlobal("localStorage", { getItem: () => "substrate_tok_stored" })

    const read = await new QueryClient().fetchQuery(discoveryQueryOptions)

    expect(read).toEqual(doc)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/.well-known/substrate/server.json")
    expect(init?.method).toBe("GET")
    expect(
      (init?.headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })
})
