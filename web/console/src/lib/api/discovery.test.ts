import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { fetchAuthPolicy, resetAuthPolicy } from "./discovery"

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

  it("reads the policy off GET /api, anonymously", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { auth: { totpRequired: false } })
    )
    expect(await fetchAuthPolicy()).toEqual({ totpRequired: false })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/api")
    expect(
      ((init as RequestInit).headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("shares one in-flight request, and asks again once it has settled", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { auth: { totpRequired: false } })
    )
    // Doors mounting together ask once between them.
    await Promise.all([fetchAuthPolicy(), fetchAuthPolicy()])
    expect(fetchMock.mock.calls.length).toBe(1)
    // A door mounting after that asks again: a dev substrate is restarted from
    // one door to the other, and a tab holding the old answer would send no
    // code to a substrate that wants one.
    fetchMock.mockResolvedValue(
      jsonResponse(200, { auth: { totpRequired: true } })
    )
    expect(await fetchAuthPolicy()).toEqual({ totpRequired: true })
    expect(fetchMock.mock.calls.length).toBe(2)
  })

  it("requires a code when discovery says nothing about it", async () => {
    // An older substrate serves no `auth` block; the strict shape is the only
    // safe reading of silence.
    fetchMock.mockResolvedValue(jsonResponse(200, { versions: [] }))
    expect(await fetchAuthPolicy()).toEqual({ totpRequired: true })
  })

  it("falls back to strict on an unreachable substrate, and asks again", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("offline"))
    expect(await fetchAuthPolicy()).toEqual({ totpRequired: true })
    fetchMock.mockResolvedValue(
      jsonResponse(200, { auth: { totpRequired: false } })
    )
    expect(await fetchAuthPolicy()).toEqual({ totpRequired: false })
  })
})
