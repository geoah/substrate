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
  afterEach(() => vi.restoreAllMocks())

  it("keeps a token once, and answers one kept already without writing", () => {
    const queue = new RevocationQueue("template")
    expect(queue.keep(session("a"))).toBe(true)
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError")
    })
    expect(queue.keep(session("a"))).toBe(true)
    expect(queue.keep(session("b"))).toBe(false)
    expect(queue.pending().map((s) => s.token)).toEqual(["a"])
  })

  it("forgets a token, and answers whether it is gone", () => {
    const queue = new RevocationQueue("template")
    queue.keep(session("a"))
    queue.keep(session("b"))
    expect(queue.drop(session("a"))).toBe(true)
    expect(queue.drop(session("x"))).toBe(true)
    expect(queue.pending().map((s) => s.token)).toEqual(["b"])
  })

  it("says each server and repository once", () => {
    expect(
      unrevokedSentences("Substrate Template", [session("a"), session("b")])
    ).toEqual([
      "Substrate Template's earlier access to ada.example.org still works on substrate.example.com. It keeps trying to revoke it; you can also revoke it yourself in substrate.example.com's Settings, under Apps.",
    ])
  })
})
