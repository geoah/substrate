// @vitest-environment jsdom
/** The live watch a page opens on the records it shows: which feed rows it
 * hears, which cached reads a change reaches, and the hook's one batched
 * invalidation per burst. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ChangeRow } from "@/lib/api/types"

const watch = vi.hoisted(() => ({
  calls: [] as {
    filter?: { kinds?: string[] }
    onRow: (row: ChangeRow) => void
    stopped: boolean
  }[],
}))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: {
    filter?: { kinds?: string[] }
    onRow: (row: ChangeRow) => void
  }) => {
    const call = { ...opts, stopped: false }
    watch.calls.push(call)
    return {
      stop: () => {
        call.stopped = true
      },
    }
  },
}))

import { createChangeBatcher } from "./use-live-records"
import {
  changedInScope,
  dedupeChanges,
  liveChangeReaches,
  useChangeMarks,
  useLiveInvalidation,
} from "./use-live-invalidation"

const TASK = "acme.example.com/tasks/task"
const PERSON = "acme.example.com/people/person"

function row(over: Partial<ChangeRow>): ChangeRow {
  return {
    seq: 1,
    ts: "2026-09-26T00:00:00Z",
    actor: "agent:acme.example.com:tasks:helper",
    op: "patch",
    recordId: "t1",
    kind: TASK,
    ...over,
  }
}

describe("changedInScope", () => {
  it("names the addressed record when the row lists nothing affected", () => {
    expect(changedInScope(row({}), { kinds: [TASK] })).toEqual([
      { kind: TASK, id: "t1", deleted: false },
    ])
    expect(changedInScope(row({ op: "delete" }), { kinds: [TASK] })).toEqual([
      { kind: TASK, id: "t1", deleted: true },
    ])
  })

  it("reads every affected record, narrowed to the scope", () => {
    const merge = row({
      op: "merge",
      affected: [
        { kind: TASK, id: "t1", version: 4 },
        { kind: TASK, id: "t2", deleted: true },
        { kind: PERSON, id: "p1", version: 2 },
      ],
    })
    expect(changedInScope(merge, { kinds: [TASK] })).toEqual([
      { kind: TASK, id: "t1", deleted: false },
      { kind: TASK, id: "t2", deleted: true },
    ])
    expect(changedInScope(merge, { kinds: [TASK], recordIds: ["t2"] })).toEqual(
      [{ kind: TASK, id: "t2", deleted: true }]
    )
    expect(changedInScope(merge, { recordIds: ["p1"] })).toEqual([
      { kind: PERSON, id: "p1", deleted: false },
    ])
  })
})

describe("dedupeChanges", () => {
  it("keeps one entry per record, deleted if any change deleted it", () => {
    expect(
      dedupeChanges([
        { kind: TASK, id: "t1", deleted: false },
        { kind: TASK, id: "t1", deleted: true },
        { kind: PERSON, id: "t1", deleted: false },
      ])
    ).toEqual([
      { kind: TASK, id: "t1", deleted: true },
      { kind: PERSON, id: "t1", deleted: false },
    ])
  })
})

describe("liveChangeReaches", () => {
  const change = { kind: TASK, id: "t1", deleted: false }
  it("reaches the record, its collection's pages and its counts", () => {
    expect(
      liveChangeReaches(
        ["record", "acme.example.com", "tasks", "task", "t1"],
        change
      )
    ).toBe(true)
    expect(liveChangeReaches(["records", [TASK], { first: 50 }], change)).toBe(
      true
    )
    expect(
      liveChangeReaches(
        ["records-count", "acme.example.com", "tasks", "task", null],
        change
      )
    ).toBe(true)
  })

  it("leaves another record and another collection alone", () => {
    expect(
      liveChangeReaches(
        ["record", "acme.example.com", "tasks", "task", "t2"],
        change
      )
    ).toBe(false)
    expect(liveChangeReaches(["records", [PERSON], {}], change)).toBe(false)
    expect(
      liveChangeReaches(
        ["records-count", "acme.example.com", "people", "person", null],
        change
      )
    ).toBe(false)
  })
})

describe("createChangeBatcher", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it("flushes once after a burst goes quiet", () => {
    const flush = vi.fn()
    const b = createChangeBatcher<number>(flush, { wait: 100, maxWait: 1000 })
    b.push(1)
    vi.advanceTimersByTime(50)
    b.push(2, 3)
    vi.advanceTimersByTime(99)
    expect(flush).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(flush).toHaveBeenCalledExactlyOnceWith([1, 2, 3])
  })

  it("never holds a long burst past maxWait", () => {
    const flush = vi.fn()
    const b = createChangeBatcher<number>(flush, { wait: 100, maxWait: 250 })
    for (let i = 0; i < 5; i++) {
      b.push(i)
      vi.advanceTimersByTime(60)
    }
    expect(flush).toHaveBeenCalledExactlyOnceWith([0, 1, 2, 3, 4])
  })

  it("drops what it holds on cancel", () => {
    const flush = vi.fn()
    const b = createChangeBatcher<number>(flush, { wait: 100 })
    b.push(1)
    b.cancel()
    vi.advanceTimersByTime(500)
    expect(flush).not.toHaveBeenCalled()
  })
})

describe("useLiveInvalidation", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    watch.calls = []
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  function setup(scope: { kinds?: string[]; recordIds?: string[] }) {
    const client = new QueryClient()
    const invalidate = vi.spyOn(client, "invalidateQueries")
    const onChange = vi.fn()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const hook = renderHook(() => useLiveInvalidation(scope, onChange), {
      wrapper,
    })
    return { client, invalidate, onChange, hook }
  }

  it("watches the scope's kinds and hears one batch per burst", async () => {
    const { invalidate, onChange } = setup({ kinds: [TASK] })
    expect(watch.calls).toHaveLength(1)
    expect(watch.calls[0].filter).toEqual({ kinds: [TASK] })
    await act(async () => {
      watch.calls[0].onRow(row({ recordId: "t1" }))
      watch.calls[0].onRow(row({ seq: 2, recordId: "t2" }))
      watch.calls[0].onRow(row({ seq: 3, recordId: "t1" }))
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledExactlyOnceWith([
      { kind: TASK, id: "t1", deleted: false },
      { kind: TASK, id: "t2", deleted: false },
    ])
  })

  it("stays quiet for records outside the scope", async () => {
    const { invalidate, onChange } = setup({
      kinds: [TASK],
      recordIds: ["t1"],
    })
    await act(async () => {
      watch.calls[0].onRow(row({ recordId: "t9" }))
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(invalidate).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("opens nothing for an empty scope and stops on unmount", () => {
    setup({})
    expect(watch.calls).toHaveLength(0)
    const { hook } = setup({ kinds: [TASK] })
    hook.unmount()
    expect(watch.calls[0].stopped).toBe(true)
  })
})

describe("useChangeMarks", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it("holds a mark, fades it, then drops it", () => {
    const { result } = renderHook(() => useChangeMarks())
    act(() => result.current.mark(["t1", "t2"]))
    expect(result.current.marks.get("t1")).toBe("fresh")
    act(() => void vi.advanceTimersByTime(1300))
    expect(result.current.marks.get("t1")).toBe("fading")
    act(() => result.current.mark(["t1"]))
    expect(result.current.marks.get("t1")).toBe("fresh")
    act(() => void vi.advanceTimersByTime(1600))
    expect(result.current.marks.has("t2")).toBe(false)
    expect(result.current.marks.get("t1")).toBe("fading")
    act(() => void vi.advanceTimersByTime(1600))
    expect(result.current.marks.size).toBe(0)
  })

  it("drops a mark whatever re-renders the page in between", () => {
    const { result, rerender } = renderHook(() => useChangeMarks())
    act(() => result.current.mark(["t1"]))
    rerender()
    act(() => void vi.advanceTimersByTime(500))
    rerender()
    act(() => void vi.advanceTimersByTime(3000))
    expect(result.current.marks.size).toBe(0)
  })
})
