import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RevocationQueue } from "./revocations"
import { SessionStore, type Session } from "./session"
import { NotSignedOut, signOut } from "./signout"

const session = (token: string): Session => ({
  server: "https://substrate.example.com",
  repository: "ada.example.org",
  token,
  scopes: [],
  clientID: "https://app.example.com/client.json",
  details: [],
  signedInAt: 0,
})

const queueKey = "substrate.unrevoked.template"
let store: SessionStore
let queue: RevocationQueue
let sent: string[]

function server(answer: () => Promise<Response>) {
  vi.stubGlobal(
    "fetch",
    vi.fn((_url: string, init: RequestInit) => {
      sent.push(new URLSearchParams(init.body as URLSearchParams).get("token")!)
      return answer()
    })
  )
}

/** Storage that will not take the revocation queue, as a full one does. */
function fullQueue() {
  const real = Storage.prototype.setItem
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (
    this: Storage,
    k: string,
    v: string
  ) {
    if (k === queueKey) throw new DOMException("full", "QuotaExceededError")
    real.call(this, k, v)
  })
}

beforeEach(() => {
  localStorage.clear()
  store = new SessionStore("template")
  queue = new RevocationQueue("template")
  sent = []
})
afterEach(() => vi.unstubAllGlobals())

describe("signOut", () => {
  it("keeps the token, clears the session, then revokes it", async () => {
    store.save(session("a"))
    server(() => Promise.resolve(new Response("{}")))
    const out = await signOut(session("a"), store, queue)
    expect(out.type).toBe("signedOut")
    expect(store.load()).toBeNull()
    expect(await out.revoked).toBe(true)
    expect(sent).toEqual(["a"])
    expect(queue.pending()).toEqual([])
  })

  it("leaves the session another tab signed in since, and revokes only its own token", async () => {
    store.save(session("b"))
    server(() => Promise.reject(new TypeError("Failed to fetch")))
    const out = await signOut(session("a"), store, queue)
    expect(out).toMatchObject({ type: "replaced", session: { token: "b" } })
    expect(store.load()?.token).toBe("b")
    expect(await out.revoked).toBe(false)
    expect(sent).toEqual(["a"])
    expect(queue.pending().map((s) => s.token)).toEqual(["a"])
  })

  it("revokes first where the queue will not keep the token, and stays signed in when that fails too", async () => {
    store.save(session("a"))
    fullQueue()
    server(() => Promise.reject(new TypeError("Failed to fetch")))
    await expect(signOut(session("a"), store, queue)).rejects.toBeInstanceOf(
      NotSignedOut
    )
    expect(store.load()?.token).toBe("a")

    server(() => Promise.resolve(new Response("{}")))
    const out = await signOut(session("a"), store, queue)
    expect(out.type).toBe("signedOut")
    expect(await out.revoked).toBe(true)
    expect(store.load()).toBeNull()
    expect(sent).toEqual(["a", "a"])
  })
})
