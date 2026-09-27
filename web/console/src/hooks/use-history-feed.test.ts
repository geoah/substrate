import { describe, expect, it } from "vitest"

import type { HistoryPage } from "@/lib/api/changes"
import type { ChangeRow } from "@/lib/api/types"
import { feedEntries } from "./use-history-feed"

const TASK = "ada.example.com/tasks/task"

function row(seq: number, over: Partial<ChangeRow> = {}): ChangeRow {
  return {
    seq,
    ts: "2026-09-24T12:00:00Z",
    actor: "console",
    op: "put",
    recordId: `r${seq}`,
    kind: TASK,
    payload: { created: true },
    ...over,
  }
}

const runs: HistoryPage = {
  runs: [
    {
      actor: "console",
      kind: TASK,
      verb: "create",
      count: 60,
      records: 60,
      newestSeq: 90,
      oldestSeq: 31,
      newestTs: "2026-09-24T12:00:00Z",
      oldestTs: "2026-09-24T11:00:00Z",
    },
    {
      actor: "substrate",
      kind: "substrate.reamde.dev/core/triggerrun",
      verb: "gc",
      count: 4,
      records: 4,
      newestSeq: 30,
      oldestSeq: 27,
      newestTs: "2026-09-24T10:00:00Z",
      oldestTs: "2026-09-24T10:00:00Z",
    },
  ],
  head: 95,
  generation: "g",
}

const base = {
  live: [] as ChangeRow[],
  filter: {},
  values: false,
  hasOlder: false,
  technical: true,
}

describe("feedEntries", () => {
  it("says each run with its exact count, however far below the page it reaches", () => {
    const [added] = feedEntries({ ...base, pages: [runs] })
    expect(added).toMatchObject({ verb: "added", count: 60, recordCount: 60 })
    expect(added.openEnded).toBeUndefined()
  })

  it("joins the tail's new rows onto the newest run, and drops what the page told", () => {
    const [added] = feedEntries({
      ...base,
      pages: [runs],
      live: [row(92), row(91), row(90)],
    })
    expect(added.count).toBe(62)
    expect(added.recordCount).toBe(62)
  })

  it("leaves housekeeping for technical details", () => {
    expect(feedEntries({ ...base, pages: [runs] })).toHaveLength(2)
    expect(
      feedEntries({ ...base, pages: [runs], technical: false })
    ).toHaveLength(1)
  })

  it("leaves out an internal kind the everyday filter could not name", () => {
    const page: HistoryPage = {
      changes: [
        row(5),
        row(4, { kind: "substrate.reamde.dev/core/retired", actor: "api" }),
      ],
      head: 5,
      generation: "g",
    }
    const everyday = { excludeKinds: ["substrate.reamde.dev/core/token"] }
    expect(feedEntries({ ...base, pages: [page] })).toHaveLength(2)
    expect(
      feedEntries({ ...base, pages: [page], filter: everyday })
    ).toHaveLength(1)
  })

  it("folds rows itself against a server without runs, the oldest said without a count while more lie below", () => {
    const page: HistoryPage = {
      changes: [row(5), row(4), row(3)],
      cursor: 3,
      head: 5,
      generation: "g",
    }
    const [entry] = feedEntries({ ...base, pages: [page], hasOlder: true })
    expect(entry.count).toBe(3)
    expect(entry.openEnded).toBe(true)
    const [closed] = feedEntries({ ...base, pages: [page] })
    expect(closed.openEnded).toBeUndefined()
  })
})
