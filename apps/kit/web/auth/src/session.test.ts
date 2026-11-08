import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SessionStore, type Session } from "./session"

const session = (token: string): Session => ({
  server: "https://substrate.example.com",
  repository: "ada.example.org",
  token,
  scopes: [],
  clientID: "https://app.example.com/client.json",
  details: [],
  signedInAt: 0,
})

describe("SessionStore", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.restoreAllMocks())

  it("answers an ended session as none and writes nothing: a change under the lock clears it", () => {
    const store = new SessionStore("template")
    store.save({ ...session("old"), expiresAt: 1000 })
    const remove = vi.spyOn(Storage.prototype, "removeItem")
    expect(store.load()).toBeNull()
    expect(store.current("old")).toBe(false)
    expect(store.read()?.token).toBe("old")
    expect(remove).not.toHaveBeenCalled()
  })

  it("hears another tab change the session", () => {
    const store = new SessionStore("template")
    const heard = vi.fn()
    const stop = store.watch(heard)
    store.save(session("b"))
    window.dispatchEvent(
      new StorageEvent("storage", { key: "substrate.session.template" })
    )
    window.dispatchEvent(
      new StorageEvent("storage", { key: "substrate.session.other" })
    )
    expect(heard).toHaveBeenCalledTimes(1)
    expect((heard.mock.calls[0][0] as Session).token).toBe("b")
    stop()
    window.dispatchEvent(new StorageEvent("storage", { key: null }))
    expect(heard).toHaveBeenCalledTimes(1)
  })
})
