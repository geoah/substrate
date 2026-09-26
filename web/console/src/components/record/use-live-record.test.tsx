// @vitest-environment jsdom
/** The record page's live half: a re-read that moved values under the reader
 * marks exactly those properties, briefly; the reader's own write does not. */

import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/hooks/use-live-invalidation", () => ({
  useLiveInvalidation: vi.fn(),
}))

import { movedProperties, useLiveRecord } from "./use-live-record"
import { noteWrite } from "@/components/property-sheet/use-record-patch"
import { useLiveInvalidation } from "@/hooks/use-live-invalidation"
import type { SubstrateRecord } from "@/lib/api/types"

const TASK = "ada.example.com/tasks/task"
const rec = (
  version: number,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id: "t1",
  kind: TASK,
  properties,
  labels: {},
  version,
  createdAt: "",
  updatedAt: "",
})

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe("movedProperties", () => {
  it("names what changed, was added or was removed", () => {
    expect(
      movedProperties(
        rec(1, { a: 1, b: [1], c: "x" }),
        rec(2, { a: 1, b: [1, 2], d: true })
      )
    ).toEqual(["b", "c", "d"])
  })
})

describe("useLiveRecord", () => {
  it("watches this one record on the feed", () => {
    renderHook(() => useLiveRecord(rec(1, {})))
    expect(useLiveInvalidation).toHaveBeenCalledWith({
      kinds: [TASK],
      recordIds: ["t1"],
    })
  })

  it("marks what moved under the reader, then lets it go", () => {
    const { result, rerender } = renderHook(({ r }) => useLiveRecord(r), {
      initialProps: { r: rec(1, { status: "open", name: "A" }) },
    })
    rerender({ r: rec(2, { status: "done", name: "A" }) })
    expect([...result.current]).toEqual(["status"])
    act(() => vi.advanceTimersByTime(3000))
    expect(result.current.size).toBe(0)
  })

  it("leaves the reader's own write unmarked", () => {
    const { result, rerender } = renderHook(({ r }) => useLiveRecord(r), {
      initialProps: { r: rec(4, { name: "A" }) },
    })
    noteWrite(TASK, "t1", 5)
    rerender({ r: rec(5, { name: "B" }) })
    expect(result.current.size).toBe(0)
  })

  it("lets a mark go even when the reader's own write lands before it expires", () => {
    const { result, rerender } = renderHook(({ r }) => useLiveRecord(r), {
      initialProps: { r: rec(10, { status: "open", name: "A" }) },
    })
    rerender({ r: rec(11, { status: "done", name: "A" }) })
    expect([...result.current]).toEqual(["status"])
    act(() => vi.advanceTimersByTime(1000))
    noteWrite(TASK, "t1", 12)
    rerender({ r: rec(12, { status: "done", name: "B" }) })
    act(() => vi.advanceTimersByTime(3000))
    expect(result.current.size).toBe(0)
  })

  it("keeps a mark its full time when an unchanged re-read lands", () => {
    const { result, rerender } = renderHook(({ r }) => useLiveRecord(r), {
      initialProps: { r: rec(20, { status: "open" }) },
    })
    rerender({ r: rec(21, { status: "done" }) })
    act(() => vi.advanceTimersByTime(1000))
    rerender({ r: rec(21, { status: "done" }) })
    expect([...result.current]).toEqual(["status"])
    act(() => vi.advanceTimersByTime(3000))
    expect(result.current.size).toBe(0)
  })
})
