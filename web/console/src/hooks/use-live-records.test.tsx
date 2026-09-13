// @vitest-environment jsdom
/** The shared tail's three duties beyond a row: the handoff when a tail
 * first goes live (every watched read, the registry and the implementors are
 * stale until refetched), the recovery from a compacted cursor (the same
 * handoff, then a fresh tail at the head), and a terminal stop a screen can
 * see and retry. */

import { act, cleanup, renderHook } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { watchChanges } from "@/lib/api/changes"
import {
  liveStatusSnapshot,
  liveTailKinds,
  retryLiveTail,
  useLiveRecords,
  useLiveStatus,
} from "./use-live-records"

vi.mock("@/lib/api/changes", () => ({ watchChanges: vi.fn() }))

const TASK = "ada.example.com/tasks/task"
type Opened = Parameters<typeof watchChanges>[0]
let opened: Opened[] = []

beforeEach(() => {
  vi.useFakeTimers()
  opened = []
  vi.mocked(watchChanges).mockImplementation((opts) => {
    opened.push(opts)
    return { stop: vi.fn() }
  })
})

afterEach(() => {
  cleanup()
  // The last unmount schedules the reconcile that closes the tail.
  act(() => void vi.advanceTimersByTime(0))
  vi.useRealTimers()
  expect(liveTailKinds()).toEqual([])
})

function mount(kinds: string[]) {
  const client = new QueryClient()
  const invalidated = vi.spyOn(client, "invalidateQueries")
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const hook = renderHook(() => useLiveRecords(kinds), { wrapper })
  act(() => void vi.advanceTimersByTime(0))
  const keys = () =>
    invalidated.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey))
  return { client, invalidated, keys, hook, wrapper }
}

describe("useLiveRecords", () => {
  it("hands off on the first live: lists, counts, every record read, the registry and the implementors", () => {
    const { keys, invalidated } = mount([TASK])
    expect(opened).toHaveLength(1)
    expect(opened[0].from).toBeUndefined()
    expect(opened[0].filter?.kinds).toContain(TASK)
    expect(opened[0].filter?.kinds).toContain(
      "substrate.reamde.dev/core/package"
    )

    act(() => opened[0].onStatus?.("live"))
    expect(liveStatusSnapshot()).toEqual({ state: "live", detail: undefined })
    const first = keys()
    for (const key of [
      ["records", "ada.example.com", "tasks", "task"],
      ["records-count", "ada.example.com", "tasks", "task"],
      ["record", "ada.example.com", "tasks", "task"],
      ["records", "substrate.reamde.dev", "core", "view"],
      ["registry"],
      ["trait", "implementors"],
    ]) {
      expect(first).toContain(JSON.stringify(key))
    }

    // A reconnect resumes from a cursor: the lists alone are refreshed.
    invalidated.mockClear()
    act(() => opened[0].onStatus?.("live"))
    const again = keys()
    expect(again).toContain(
      JSON.stringify(["records", "ada.example.com", "tasks", "task"])
    )
    expect(again).not.toContain(JSON.stringify(["registry"]))
    expect(again).not.toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task"])
    )
  })

  it("on a compacted cursor invalidates what was watched and reopens at the head", () => {
    const { keys } = mount([TASK])
    act(() => {
      opened[0].onStatus?.("compacted", "generation gone")
      opened[0].onCompacted?.()
    })
    expect(liveStatusSnapshot()).toEqual({
      state: "compacted",
      detail: "generation gone",
    })
    const invalidated = keys()
    expect(invalidated).toContain(
      JSON.stringify(["records", "ada.example.com", "tasks", "task"])
    )
    expect(invalidated).toContain(JSON.stringify(["registry"]))
    expect(invalidated).toContain(JSON.stringify(["trait", "implementors"]))

    act(() => void vi.advanceTimersByTime(0))
    expect(opened).toHaveLength(2)
    expect(opened[1].from).toBeUndefined()
    expect(opened[1].filter?.kinds).toEqual(opened[0].filter?.kinds)
    act(() => opened[1].onStatus?.("live"))
    expect(liveStatusSnapshot().state).toBe("live")
  })

  it("keeps a terminal stop visible and reopens on retry", () => {
    const { hook } = mount([TASK])
    const status = renderHook(() => useLiveStatus())
    act(() => opened[0].onStatus?.("stopped", "stream failed"))
    expect(status.result.current).toEqual({
      state: "stopped",
      detail: "stream failed",
    })
    // Nothing reopens on its own.
    act(() => void vi.advanceTimersByTime(10_000))
    expect(opened).toHaveLength(1)

    act(() => {
      retryLiveTail()
      vi.advanceTimersByTime(0)
    })
    expect(opened).toHaveLength(2)
    act(() => opened[1].onStatus?.("connecting"))
    expect(status.result.current.state).toBe("connecting")
    hook.unmount()
  })

  it("ignores a callback from a tail it already replaced", () => {
    const { invalidated, wrapper } = mount([TASK])
    const second = renderHook(
      () => useLiveRecords(["ada.example.com/tasks/project"]),
      { wrapper }
    )
    act(() => void vi.advanceTimersByTime(0))
    expect(opened).toHaveLength(2)
    invalidated.mockClear()
    act(() => opened[0].onStatus?.("stopped", "old"))
    expect(liveStatusSnapshot().state).not.toBe("stopped")
    second.unmount()
  })
})
