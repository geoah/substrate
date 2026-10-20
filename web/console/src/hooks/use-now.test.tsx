// @vitest-environment jsdom
/** useNow: the server's clock, read again on every tick and as soon as the
 * console learns a new offset, so a row that mounted before the first
 * response does not keep the browser's clock until its next tick. */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { noteServerDate, resetServerClock } from "@/lib/api/http"

import { useNow } from "./use-now"

const BROWSER = Date.parse("2026-10-20T09:20:00Z")

describe("useNow", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(BROWSER)
    resetServerClock()
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    resetServerClock()
  })

  it("reads the clock again on every tick", () => {
    const { result } = renderHook(() => useNow(60_000))
    expect(result.current).toBe(BROWSER)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(result.current).toBe(BROWSER + 60_000)
  })

  it("moves to the server's clock when the first response arrives", () => {
    const { result } = renderHook(() => useNow(60_000))
    expect(result.current).toBe(BROWSER)
    // The browser runs 20 minutes ahead of the server.
    act(() => {
      noteServerDate("Tue, 20 Oct 2026 09:00:00 GMT", BROWSER)
    })
    expect(result.current).toBe(Date.parse("2026-10-20T09:00:00Z"))
  })

  it("stops listening once unmounted", () => {
    const { result, unmount } = renderHook(() => useNow(60_000))
    unmount()
    noteServerDate("Tue, 20 Oct 2026 09:00:00 GMT", BROWSER)
    expect(result.current).toBe(BROWSER)
  })
})
