import { QueryClient } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { operatorDefaultsQueryOptions } from "./operator"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

describe("operatorDefaultsQueryOptions", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function read() {
    return new QueryClient({
      defaultOptions: { queries: { retry: false } },
    }).fetchQuery(operatorDefaultsQueryOptions)
  }

  it("reads GET /api/v1/operator/defaults", async () => {
    const body = {
      operator: { name: "Example site" },
      items: [
        {
          id: "llm/openai",
          slot: "llm",
          label: "OpenAI",
          source: "operator",
          operatorOffers: true,
          readiness: "ready",
        },
      ],
    }
    fetchMock.mockResolvedValue(jsonResponse(200, body))
    expect(await read()).toEqual(body)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe("/api/v1/operator/defaults")
    expect(init?.method).toBe("GET")
  })

  it("reads a null item list as none", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { items: null }))
    expect(await read()).toEqual({ items: [] })
  })

  it("reads a server older than the route as none, not a failure", async () => {
    for (const status of [404, 405, 501]) {
      fetchMock.mockResolvedValueOnce(
        jsonResponse(status, {
          error: { code: "not_found", message: "no route" },
        })
      )
      expect(await read()).toBeNull()
    }
  })

  it("fails on any other refusal", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(403, { error: { code: "forbidden", message: "scoped" } })
    )
    await expect(read()).rejects.toMatchObject({ status: 403 })
  })
})
