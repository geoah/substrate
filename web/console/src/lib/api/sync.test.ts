// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { syncWords } from "@/lib/providers"
import { requestPending, syncFieldsOf } from "@/lib/sync"

import { request, resetServerClock, serverNow } from "./http"
import { requestSync } from "./sync"

const SERVER = Date.parse("2026-10-20T08:40:00Z")
const AHEAD_MS = 3_000

function serverAnswer(at: number): Response {
  return new Response("{}", {
    status: 200,
    headers: { Date: new Date(at).toUTCString() },
  })
}

describe("requestSync", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal("fetch", fetchMock)
    resetServerClock()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    resetServerClock()
  })

  function stamped(): string {
    const body = JSON.parse(
      (fetchMock.mock.calls.at(-1)?.[1] as RequestInit).body as string
    ) as { properties: { syncRequestedAt: string } }
    return body.properties.syncRequestedAt
  }

  it("dates the request by the server's clock, not a browser clock running ahead", async () => {
    // The browser runs 3 s ahead; the server's last answer carried its own
    // clock in `Date`.
    vi.setSystemTime(SERVER + AHEAD_MS)
    fetchMock.mockImplementation(async () => serverAnswer(SERVER))
    await request("GET", "/api/x")
    expect(serverNow().getTime()).toBe(SERVER)

    vi.advanceTimersByTime(100)
    await requestSync({ kind: "a.example/p/account", id: "ada" })
    const at = stamped()
    expect(Date.parse(at)).toBeLessThanOrEqual(SERVER + 100)

    // The requested run started 0.4 s after the click by the server's clock
    // and parked: no acknowledgement, the start after the request, and the
    // failure shows again.
    const parked = syncFieldsOf({
      syncState: "erroring",
      syncError: "runner: deadline exceeded",
      syncRequestedAt: at,
      lastSyncStartedAt: new Date(SERVER + 500).toISOString(),
    })
    expect(requestPending(parked)).toBe(false)
    expect(syncWords(parked, "WHOOP")).toEqual({
      text: "Having trouble: runner: deadline exceeded",
      tone: "bad",
    })

    // The browser's own instant would have hidden it.
    expect(
      requestPending({
        ...parked,
        requestedAt: new Date(SERVER + AHEAD_MS + 100).toISOString(),
      })
    ).toBe(true)
  })

  it("uses the browser's clock until the server has answered once", async () => {
    vi.setSystemTime(SERVER)
    fetchMock.mockResolvedValue(new Response("{}", { status: 200 }))
    await requestSync({ kind: "a.example/p/account", id: "ada" })
    expect(stamped()).toBe(new Date(SERVER).toISOString())
  })
})
