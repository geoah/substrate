import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  changePassword,
  login,
  logout,
  mintToken,
  normalizeCode,
  register,
  registerEnroll,
  revokeToken,
  totpChange,
  totpEnroll,
} from "./auth"
import { clearSession, getToken, saveSession, setToken } from "./session"
import { ApiError } from "./types"

function jsonResponse(
  status: number,
  body: unknown,
  headers: Record<string, string> = {}
): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers,
  })
}

const MINT = {
  token: { id: "tok-1", label: "console", createdAt: "2026-08-12T00:00:00Z" },
  secret: "substrate_tok_minted",
}

describe("normalizeCode", () => {
  it("passes a bare six-digit code through", () => {
    expect(normalizeCode("123456")).toBe("123456")
  })

  it("strips authenticator spacing", () => {
    expect(normalizeCode("123 456")).toBe("123456")
    expect(normalizeCode("123-456")).toBe("123456")
    expect(normalizeCode(" 123456 ")).toBe("123456")
  })

  it("rejects everything that is not exactly six digits", () => {
    expect(normalizeCode("")).toBeNull()
    expect(normalizeCode("12345")).toBeNull()
    expect(normalizeCode("1234567")).toBeNull()
    expect(normalizeCode("12345a")).toBeNull()
    expect(normalizeCode("substrate_tok_x")).toBeNull()
  })
})

describe("the v1 door", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function lastCall(): [string, RequestInit] {
    return fetchMock.mock.calls.at(-1) as [string, RequestInit]
  }

  it("registerEnroll POSTs the invite code + username, anonymously, at the root", async () => {
    setToken("substrate_tok_stored")
    fetchMock.mockResolvedValue(
      jsonResponse(200, { totpSecret: "SEED", otpauthUri: "otpauth://x" })
    )
    const res = await registerEnroll("INV-1", "geoah")
    expect(res.totpSecret).toBe("SEED")
    const [url, init] = lastCall()
    expect(url).toBe("/register/enroll")
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body as string)).toEqual({
      inviteCode: "INV-1",
      username: "geoah",
    })
    // Anonymous: a stored bearer never rides the door.
    expect(
      (init.headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("register POSTs the full body and returns the minted token", async () => {
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    const res = await register({
      inviteCode: "INV-1",
      username: "geoah",
      password: "pw",
      totpSecret: "SEED",
      totpCode: "123456",
      label: "console",
    })
    expect(res.secret).toBe("substrate_tok_minted")
    const [url, init] = lastCall()
    expect(url).toBe("/register")
    expect(JSON.parse(init.body as string)).toMatchObject({
      inviteCode: "INV-1",
      username: "geoah",
      totpCode: "123456",
    })
  })

  it("login POSTs username/password/totpCode with a default label, anonymously", async () => {
    setToken("substrate_tok_stored")
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    const res = await login("geoah", "pw", "123456")
    expect(res.token.id).toBe("tok-1")
    const [url, init] = lastCall()
    expect(url).toBe("/login")
    expect(JSON.parse(init.body as string)).toEqual({
      username: "geoah",
      password: "pw",
      totpCode: "123456",
      label: "console",
    })
    expect(
      (init.headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("changePassword is anonymous — the password factor never rides a bearer", async () => {
    setToken("substrate_tok_stored")
    fetchMock.mockResolvedValue(jsonResponse(200, { username: "geoah" }))
    await changePassword("geoah", "pw", "123456", "newpw")
    const [url, init] = lastCall()
    expect(url).toBe("/password")
    expect(JSON.parse(init.body as string)).toEqual({
      username: "geoah",
      password: "pw",
      totpCode: "123456",
      newPassword: "newpw",
    })
    expect(
      (init.headers as Record<string, string>).Authorization
    ).toBeUndefined()
  })

  it("totpEnroll / totpChange POST the re-enrollment steps anonymously", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { totpSecret: "NEW", otpauthUri: "otpauth://y" })
    )
    await totpEnroll("geoah", "pw", "123456")
    expect(lastCall()[0]).toBe("/totp/enroll")

    fetchMock.mockResolvedValue(jsonResponse(200, { username: "geoah" }))
    await totpChange("geoah", "pw", "123456", "NEW", "654321")
    const [url, init] = lastCall()
    expect(url).toBe("/totp")
    expect(JSON.parse(init.body as string)).toEqual({
      username: "geoah",
      password: "pw",
      totpCode: "123456",
      newTotpSecret: "NEW",
      newTotpCode: "654321",
    })
  })

  it("mintToken carries the stored bearer and an optional expiry", async () => {
    setToken("substrate_tok_live")
    fetchMock.mockResolvedValue(jsonResponse(201, MINT))
    await mintToken("ci-runner", "2027-01-01T00:00:00Z")
    const [url, init] = lastCall()
    expect(url).toBe("/tokens")
    expect(init.method).toBe("POST")
    expect((init.headers as Record<string, string>).Authorization).toBe(
      "Bearer substrate_tok_live"
    )
    expect(JSON.parse(init.body as string)).toEqual({
      label: "ci-runner",
      expiresAt: "2027-01-01T00:00:00Z",
    })
  })

  it("revokeToken DELETEs the token record, id percent-encoded", async () => {
    setToken("substrate_tok_live")
    fetchMock.mockResolvedValue(jsonResponse(204, undefined))
    await revokeToken("tok/with slash")
    const [url, init] = lastCall()
    expect(url).toBe("/tokens/tok%2Fwith%20slash")
    expect(init.method).toBe("DELETE")
  })

  it("logout revokes the held token record, then clears the session", async () => {
    saveSession("substrate_tok_live", "geoah", "tok-held")
    fetchMock.mockResolvedValue(jsonResponse(204, undefined))
    await logout()
    const [url, init] = lastCall()
    expect(url).toBe("/tokens/tok-held")
    expect(init.method).toBe("DELETE")
    expect(getToken()).toBeNull()
  })

  it("logout clears locally even when the revoke fails", async () => {
    saveSession("substrate_tok_dead", "geoah", "tok-held")
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: "auth", message: "dead token" } })
    )
    await logout()
    expect(getToken()).toBeNull()
  })

  it("surfaces the login rate limit with its Retry-After", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(
        429,
        { error: { code: "rate_limited", message: "too many attempts" } },
        { "Retry-After": "5" }
      )
    )
    const err = (await login("geoah", "pw", "000000").catch(
      (e: unknown) => e
    )) as ApiError
    expect(err.code).toBe("rate_limited")
    expect(err.retryAfter).toBe(5)
  })

  it("a failed anonymous login never drops an existing session", async () => {
    setToken("substrate_tok_live")
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: "auth", message: "invalid code" } })
    )
    await expect(login("geoah", "pw", "000000")).rejects.toMatchObject({
      code: "auth",
    })
    expect(getToken()).toBe("substrate_tok_live")
  })
})
