/** The shared column factories: the time voices (wire ISO always rides the
 * hover — the cell text is the local rendering) and the config each factory
 * stamps on its ColumnDef (ids, labels, widths — what the Columns dropdown
 * and the fixed layout read). */

import { describe, expect, it } from "vitest"

import type { ChangeRow } from "@/lib/api/types"

import {
  changeActorColumn,
  changeAuthorityColumn,
  changeRecordColumn,
  changeKindColumn,
  changeOpColumn,
  changeSummaryColumn,
  changeTimeColumn,
  timeColumn,
  timeText,
} from "./columns"

const NOW = Date.parse("2026-08-06T15:00:00")

describe("timeText", () => {
  it("clock voice: seconds, dated only when not today's", () => {
    expect(timeText("2026-08-06T14:59:30", "clock", NOW)).toBe("14:59:30")
    expect(timeText("2026-08-04T09:05:07", "clock", NOW)).toBe(
      "2026-08-04 09:05:07"
    )
  })

  it("relative voice", () => {
    expect(timeText("2026-08-06T14:57:00", "relative", NOW)).toBe("3m ago")
  })

  it("datetime voice (the redline format)", () => {
    expect(timeText("2026-08-06T01:10:00", "datetime", NOW)).toBe(
      "Aug 6, 01:10"
    )
  })
})

describe("the feed column set", () => {
  const row: ChangeRow = {
    seq: 9,
    ts: "2026-08-06T12:00:00Z",
    actor: "providers.substrate.reamde.dev/github",
    op: "put",
    recordId: "issue-1",
    kind: "providers.substrate.reamde.dev/github/issue",
  }

  it("stamps stable ids and dropdown labels", () => {
    const cols = [
      changeTimeColumn(),
      changeActorColumn(),
      changeOpColumn(),
      changeRecordColumn([]),
      changeKindColumn(),
      changeAuthorityColumn(),
      changeSummaryColumn(),
    ]
    expect(cols.map((c) => c.id)).toEqual([
      "time",
      "actor",
      "action",
      "record",
      "kind",
      "authority",
      "summary",
    ])
    for (const col of cols) {
      expect(col.meta?.label).toBe(col.id === "action" ? "action" : col.id)
    }
  })

  it("accessors read the row the way the cell will speak it", () => {
    const accessor = (col: unknown) =>
      (col as { accessorFn: (r: ChangeRow, i: number) => unknown }).accessorFn(
        row,
        0
      )
    expect(accessor(changeOpColumn())).toBe("updated")
    expect(accessor(changeKindColumn())).toBe("issue")
    expect(accessor(changeAuthorityColumn())).toBe(
      "providers.substrate.reamde.dev"
    )
    expect(accessor(timeColumn<ChangeRow>({ id: "t", iso: (r) => r.ts }))).toBe(
      row.ts
    )
  })
})
