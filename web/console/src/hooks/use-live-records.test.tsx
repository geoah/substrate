// @vitest-environment jsdom
/** The shared tail's duties beyond a row: the handoff when a tail first
 * goes live (every watched read and the registry are stale until refetched,
 * a reconnect narrower), a row's invalidation narrowed to the records it
 * names, the recovery from a compacted cursor (the same handoff, then a fresh
 * tail at the head), and a terminal stop a screen can see and retry. */

import { act, cleanup, renderHook } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { watchChanges } from "@/lib/api/changes"
import type { ChangeRow } from "@/lib/api/types"
import {
  liveStatusSnapshot,
  liveTailKinds,
  retryLiveTail,
  useLiveRecords,
  useLiveStatus,
} from "./use-live-records"

vi.mock("@/lib/api/changes", () => ({ watchChanges: vi.fn() }))

const TASK = "ada.example.com/tasks/task"
const APP = "substrate.reamde.dev/core/app"
type Opened = Parameters<typeof watchChanges>[0]
let opened: Opened[] = []
let stops: ReturnType<typeof vi.fn>[] = []

beforeEach(() => {
  vi.useFakeTimers()
  opened = []
  stops = []
  vi.mocked(watchChanges).mockImplementation((opts) => {
    opened.push(opts)
    const stop = vi.fn()
    stops.push(stop)
    return { stop }
  })
})

afterEach(() => {
  cleanup()
  // The last unmount schedules the reconcile that closes the tail.
  act(() => void vi.advanceTimersByTime(0))
  vi.useRealTimers()
  expect(liveTailKinds()).toEqual([])
  expect(liveStatusSnapshot().state).toBe("off")
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

const row = (kind: string, id: string, affected?: ChangeRow["affected"]) =>
  ({
    seq: 1,
    ts: "2026-09-01T00:00:00Z",
    kind,
    recordId: id,
    op: "update",
    actor: "user",
    affected,
  }) as unknown as ChangeRow

describe("useLiveRecords", () => {
  it("watches the caller's kinds beside the app, kind and package collections", () => {
    mount([TASK])
    expect(opened).toHaveLength(1)
    expect(opened[0].from).toBeUndefined()
    expect(opened[0].filter?.kinds).toEqual([
      TASK,
      APP,
      "substrate.reamde.dev/core/kind",
      "substrate.reamde.dev/core/package",
    ])
    expect(liveTailKinds()).toEqual(opened[0].filter?.kinds)
  })

  it("hands off on the first live: lists, counts, every record read and the registry; a reconnect refreshes the lists alone", () => {
    const { keys, invalidated } = mount([TASK])
    act(() => opened[0].onStatus?.("live"))
    expect(liveStatusSnapshot()).toEqual({ state: "live", detail: undefined })
    const first = keys()
    for (const key of [
      ["records", "ada.example.com", "tasks", "task"],
      ["records-count", "ada.example.com", "tasks", "task"],
      ["record", "ada.example.com", "tasks", "task"],
      ["records", "substrate.reamde.dev", "core", "app"],
      ["record", "substrate.reamde.dev", "core", "app"],
      ["records", "substrate.reamde.dev", "core", "package"],
      ["registry"],
    ]) {
      expect(first).toContain(JSON.stringify(key))
    }

    invalidated.mockClear()
    act(() => opened[0].onStatus?.("live"))
    const again = keys()
    expect(again).toContain(
      JSON.stringify(["records", "ada.example.com", "tasks", "task"])
    )
    expect(again).toContain(
      JSON.stringify(["records", "substrate.reamde.dev", "core", "app"])
    )
    expect(again).not.toContain(JSON.stringify(["registry"]))
    expect(again).not.toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task"])
    )
    expect(again).not.toContain(
      JSON.stringify(["record", "substrate.reamde.dev", "core", "app"])
    )
  })

  it("narrows a row's invalidation to the records it names, batched for a beat", () => {
    const { keys, invalidated } = mount([TASK])
    act(() => opened[0].onStatus?.("live"))
    invalidated.mockClear()
    act(() => {
      opened[0].onRow(row(TASK, "t1"))
      opened[0].onRow(
        row(APP, "a1", [
          { kind: APP, id: "a1" },
          { kind: TASK, id: "t2" },
        ])
      )
    })
    expect(keys()).toEqual([])
    act(() => void vi.advanceTimersByTime(150))
    const touched = keys()
    expect(touched).toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task", "t1"])
    )
    expect(touched).toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task", "t2"])
    )
    expect(touched).toContain(
      JSON.stringify(["record", "substrate.reamde.dev", "core", "app", "a1"])
    )
    expect(touched).toContain(
      JSON.stringify(["records", "ada.example.com", "tasks", "task"])
    )
    expect(touched).not.toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task"])
    )
    expect(touched).not.toContain(JSON.stringify(["registry"]))
    // The kind collection moving is the one row that reaches the registry.
    invalidated.mockClear()
    act(() => {
      opened[0].onRow(row("substrate.reamde.dev/core/kind", "tasks/task"))
      vi.advanceTimersByTime(150)
    })
    expect(keys()).toContain(JSON.stringify(["registry"]))
  })

  it("on a compacted cursor invalidates what was watched and reopens at the head", () => {
    const { keys, invalidated } = mount([TASK])
    act(() => opened[0].onStatus?.("live"))
    invalidated.mockClear()
    act(() => {
      opened[0].onStatus?.("compacted", "generation gone")
      opened[0].onCompacted?.()
    })
    expect(liveStatusSnapshot()).toEqual({
      state: "compacted",
      detail: "generation gone",
    })
    const invalidatedKeys = keys()
    expect(invalidatedKeys).toContain(
      JSON.stringify(["records", "ada.example.com", "tasks", "task"])
    )
    expect(invalidatedKeys).toContain(
      JSON.stringify(["record", "ada.example.com", "tasks", "task"])
    )
    expect(invalidatedKeys).toContain(JSON.stringify(["registry"]))

    act(() => void vi.advanceTimersByTime(0))
    expect(opened).toHaveLength(2)
    expect(opened[1].from).toBeUndefined()
    expect(opened[1].filter?.kinds).toEqual(opened[0].filter?.kinds)
    // The terminated tail is discarded, not stopped twice.
    expect(stops[0]).not.toHaveBeenCalled()
    act(() => opened[1].onStatus?.("live"))
    expect(liveStatusSnapshot().state).toBe("live")
    // The fresh tail's first live is a handoff again.
    expect(keys()).toContain(JSON.stringify(["registry"]))
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

  it("reopens a stopped tail when the union changes", () => {
    const { wrapper } = mount([TASK])
    act(() => opened[0].onStatus?.("stopped", "stream failed"))
    const second = renderHook(
      () => useLiveRecords(["ada.example.com/tasks/project"]),
      { wrapper }
    )
    act(() => void vi.advanceTimersByTime(0))
    expect(opened).toHaveLength(2)
    expect(opened[1].filter?.kinds).toContain("ada.example.com/tasks/project")
    second.unmount()
  })

  it("ignores a callback from a tail it already replaced", () => {
    const { invalidated, wrapper } = mount([TASK])
    const second = renderHook(
      () => useLiveRecords(["ada.example.com/tasks/project"]),
      { wrapper }
    )
    act(() => void vi.advanceTimersByTime(0))
    expect(opened).toHaveLength(2)
    expect(stops[0]).toHaveBeenCalledTimes(1)
    act(() => opened[1].onStatus?.("live"))
    invalidated.mockClear()
    act(() => {
      opened[0].onStatus?.("stopped", "old")
      opened[0].onRow(row(TASK, "t1"))
      opened[0].onCompacted?.()
      vi.advanceTimersByTime(150)
    })
    expect(liveStatusSnapshot().state).toBe("live")
    expect(invalidated).not.toHaveBeenCalled()
    expect(opened).toHaveLength(2)
    second.unmount()
  })
})
