import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { envelopeError, request } from "./http"
import {
  clearSession,
  getToken,
  setToken,
  setUnauthorizedHandler,
} from "./session"
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

describe("request", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    clearSession()
    setUnauthorizedHandler(null)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("returns the parsed body on success", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { records: [] }))
    await expect(request("GET", "/api/x")).resolves.toEqual({ records: [] })
  })

  it("carries the stored bearer", async () => {
    setToken("substrate_tok_abc")
    fetchMock.mockResolvedValue(jsonResponse(200, {}))
    await request("GET", "/api/x")
    const headers = (fetchMock.mock.calls[0][1] as RequestInit)
      .headers as Record<string, string>
    expect(headers.Authorization).toBe("Bearer substrate_tok_abc")
  })

  it("prefers an explicit token over the stored one", async () => {
    setToken("stored")
    fetchMock.mockResolvedValue(jsonResponse(200, {}))
    await request("GET", "/api/x", undefined, { token: "candidate" })
    const headers = (fetchMock.mock.calls[0][1] as RequestInit)
      .headers as Record<string, string>
    expect(headers.Authorization).toBe("Bearer candidate")
  })

  it("rejects with the envelope's code, message and problems", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(422, {
        error: {
          code: "validation",
          message: "bad manifest",
          problems: ["properties.name: required"],
        },
      })
    )
    const err = await request("GET", "/api/x").catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({
      code: "validation",
      message: "bad manifest",
      problems: ["properties.name: required"],
      status: 422,
    })
  })

  it("falls back to a status-shaped code when the body has no envelope", async () => {
    fetchMock.mockResolvedValue(jsonResponse(404, undefined))
    const err = (await request("GET", "/api/x").catch(
      (e: unknown) => e
    )) as ApiError
    expect(err.code).toBe("not_found")
  })

  it("rejects network failures as code network, status 0", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"))
    const err = (await request("GET", "/api/x").catch(
      (e: unknown) => e
    )) as ApiError
    expect(err.code).toBe("network")
    expect(err.status).toBe(0)
  })

  it("drops the session and fires the handler on a 401", async () => {
    setToken("dead")
    const expired = vi.fn()
    setUnauthorizedHandler(expired)
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: "auth", message: "invalid token" } })
    )
    await expect(request("GET", "/api/x")).rejects.toMatchObject({
      code: "auth",
    })
    expect(getToken()).toBeNull()
    expect(expired).toHaveBeenCalledOnce()
  })

  it("keeps the session on a 401 for an explicit-token probe", async () => {
    setToken("live")
    const expired = vi.fn()
    setUnauthorizedHandler(expired)
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: "auth", message: "invalid token" } })
    )
    await expect(
      request("GET", "/api/x", undefined, { token: "candidate" })
    ).rejects.toMatchObject({ code: "auth" })
    expect(getToken()).toBe("live")
    expect(expired).not.toHaveBeenCalled()
  })

  it("keeps the session on an anonymous 401", async () => {
    setToken("live")
    fetchMock.mockResolvedValue(jsonResponse(401, undefined))
    await expect(
      request("POST", "/api/x", { otp: "1" }, { anonymous: true })
    ).rejects.toMatchObject({ code: "auth" })
    expect(getToken()).toBe("live")
  })

  it("reads Retry-After onto rate-limit errors", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(429, undefined, { "Retry-After": "12" })
    )
    const err = (await request("GET", "/api/x").catch(
      (e: unknown) => e
    )) as ApiError
    expect(err.code).toBe("rate_limited")
    expect(err.retryAfter).toBe(12)
  })
})

describe("envelopeError", () => {
  it("maps every fallback status", () => {
    expect(envelopeError(400, undefined).code).toBe("bad_request")
    expect(envelopeError(401, undefined).code).toBe("auth")
    expect(envelopeError(403, undefined).code).toBe("forbidden")
    expect(envelopeError(404, undefined).code).toBe("not_found")
    expect(envelopeError(409, undefined).code).toBe("conflict")
    expect(envelopeError(410, undefined).code).toBe("compacted")
    expect(envelopeError(422, undefined).code).toBe("validation")
    expect(envelopeError(429, undefined).code).toBe("rate_limited")
    expect(envelopeError(500, undefined).code).toBe("internal")
    expect(envelopeError(501, undefined).code).toBe("unsupported")
    expect(envelopeError(503, undefined).code).toBe("unavailable")
    // A transport failure that never reached the substrate stays `network`.
    expect(envelopeError(0, undefined).code).toBe("network")
  })
})
