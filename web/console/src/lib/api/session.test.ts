import { beforeEach, describe, expect, it, vi } from "vitest"

import {
  clearSession,
  getToken,
  hasSession,
  maskedToken,
  saveSession,
  sessionExpired,
  setSessionChangedHandler,
  setToken,
  setUnauthorizedHandler,
} from "./session"

describe("session", () => {
  beforeEach(() => {
    setSessionChangedHandler(null)
    clearSession()
    setUnauthorizedHandler(null)
  })

  it("stores and clears the token", () => {
    expect(hasSession()).toBe(false)
    setToken("substrate_tok_x")
    expect(getToken()).toBe("substrate_tok_x")
    expect(hasSession()).toBe(true)
    clearSession()
    expect(getToken()).toBeNull()
  })

  it("sessionExpired drops the token and fires the handler", () => {
    setToken("substrate_tok_x")
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    sessionExpired()
    expect(getToken()).toBeNull()
    expect(handler).toHaveBeenCalledOnce()
  })

  it("sessionExpired without a handler still drops the token", () => {
    setToken("substrate_tok_x")
    expect(() => sessionExpired()).not.toThrow()
    expect(getToken()).toBeNull()
  })
  // Every cached answer belongs to one repository and no query key names
  // which, so signing in as somebody else must drop the cache. These pin the
  // signal that does it: a sidebar built from the previous repository's kinds
  // links collections this one never imported.
  it("fires the session-changed handler on every identity write", () => {
    const changed = vi.fn()
    setSessionChangedHandler(changed)

    setToken("substrate_tok_probe")
    expect(changed).toHaveBeenCalledTimes(1)

    saveSession("substrate_tok_second", "ada", "tok-1")
    expect(changed).toHaveBeenCalledTimes(2)

    clearSession()
    expect(changed).toHaveBeenCalledTimes(3)
  })

  it("fires the session-changed handler when a 401 drops the session", () => {
    saveSession("substrate_tok_first", "ada", "tok-1")
    const changed = vi.fn()
    setSessionChangedHandler(changed)

    sessionExpired()
    expect(changed).toHaveBeenCalledTimes(1)
    expect(hasSession()).toBe(false)
  })
})

describe("maskedToken", () => {
  it("shows only the edges of a real token", () => {
    expect(maskedToken("substrate_tok_abcdef9f2c")).toBe("subs…9f2c")
  })

  it("never echoes a short value", () => {
    expect(maskedToken("tiny")).toBe("token")
  })
})
