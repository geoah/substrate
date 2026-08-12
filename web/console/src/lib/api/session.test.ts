import { beforeEach, describe, expect, it, vi } from "vitest"

import {
  clearSession,
  getToken,
  hasSession,
  maskedToken,
  sessionExpired,
  setToken,
  setUnauthorizedHandler,
} from "./session"

describe("session", () => {
  beforeEach(() => {
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
})

describe("maskedToken", () => {
  it("shows only the edges of a real token", () => {
    expect(maskedToken("substrate_tok_abcdef9f2c")).toBe("subs…9f2c")
  })

  it("never echoes a short value", () => {
    expect(maskedToken("tiny")).toBe("token")
  })
})
