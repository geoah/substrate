import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RevocationQueue, unrevokedSentences } from "./revocations"
import type { Session } from "./session"

const session = (token: string, over: Partial<Session> = {}): Session => ({
  server: "https://substrate.example.com",
  repository: "ada.example.org",
  token,
  scopes: [],
  clientID: "https://app.example.com/client.json",
  details: [],
  signedInAt: 0,
  ...over,
})

describe("RevocationQueue", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.unstubAllGlobals())

  it("keeps a token the server did not confirm revoking, and tries again", async () => {
    const queue = new RevocationQueue("template")
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("Failed to fetch")))
    )
    expect(await queue.revoke(session("a"))).toBe(false)
    expect(queue.pending().map((s) => s.token)).toEqual(["a"])
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("{}")))
    )
    expect(await queue.retry()).toEqual([])
    expect(queue.pending()).toEqual([])
  })

  it("forgets a token that has ended without asking the server", async () => {
    const queue = new RevocationQueue("template")
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("offline")))
    )
    await queue.revoke(session("old", { expiresAt: 1000 }))
    const fetch = vi.fn()
    vi.stubGlobal("fetch", fetch)
    expect(await queue.retry(2000)).toEqual([])
    expect(fetch).not.toHaveBeenCalled()
  })

  it("counts only a 2xx as revoked: a 400 or a 401 leaves the token live", async () => {
    const queue = new RevocationQueue("template")
    for (const status of [400, 401]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(() =>
          Promise.resolve(
            new Response('{"error":"invalid_client"}', { status })
          )
        )
      )
      expect(await queue.revoke(session("a"))).toBe(false)
      expect(queue.pending().map((s) => s.token)).toEqual(["a"])
    }
  })

  it("keeps a token before it is sent, so nothing else need hold it", async () => {
    const queue = new RevocationQueue("template")
    expect(queue.keep(session("a"))).toBe(true)
    expect(queue.pending().map((s) => s.token)).toEqual(["a"])
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("{}")))
    )
    expect(await queue.send(session("a"))).toBe(true)
    expect(queue.pending()).toEqual([])
  })

  it("says each server and repository once", () => {
    expect(
      unrevokedSentences("Substrate Template", [session("a"), session("b")])
    ).toEqual([
      "Substrate Template's earlier access to ada.example.org still works on substrate.example.com. It tries again each time it opens; you can also revoke it yourself in substrate.example.com's Settings, under Apps.",
    ])
  })
})
