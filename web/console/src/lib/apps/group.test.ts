/** How rows section: the temporal point into the six buckets in order, a
 * state in declared order, a reference by its referent's title, the
 * valueless last. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import type { PropSpec } from "@/lib/record-schema"
import { groupRecords } from "./group"

const spec = (over: Partial<PropSpec>): PropSpec => ({
  name: "p",
  label: "P",
  kind: "string",
  required: false,
  repeated: false,
  keyed: false,
  managed: false,
  ...over,
})

const row = (id: string, p: unknown): SubstrateRecord => ({
  id,
  kind: "k",
  properties: { p },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const DAY = 86_400_000
const now = Date.parse("2026-09-12T12:00:00Z")
const at = (days: number) => new Date(now + days * DAY).toISOString()

describe("groupRecords", () => {
  it("buckets a datetime from Overdue to Undated, in that order", () => {
    const groups = groupRecords(
      [
        row("later", at(20)),
        row("none", undefined),
        row("today", at(0.2)),
        row("week", at(3)),
        row("late", at(-1)),
        row("tmrw", at(1)),
      ],
      spec({ kind: "datetime" }),
      new Map(),
      now
    )
    expect(groups.map((g) => [g.label, g.records.map((r) => r.id)])).toEqual([
      ["Overdue", ["late"]],
      ["Today", ["today"]],
      ["Tomorrow", ["tmrw"]],
      ["This week", ["week"]],
      ["Later", ["later"]],
      ["Undated", ["none"]],
    ])
  })
  it("orders a state by declaration and puts the valueless last", () => {
    const groups = groupRecords(
      [row("b", "done"), row("a", "open"), row("c", undefined)],
      spec({ kind: "state", states: ["open", "done"] }),
      new Map()
    )
    expect(groups.map((g) => g.label)).toEqual(["open", "done", "None"])
  })
  it("names a reference group by the referent's title", () => {
    const titles = new Map([["k/x", "Taxes 2026"]])
    const groups = groupRecords(
      [row("a", { ref: "k/x" }), row("b", { ref: "k/y" })],
      spec({ kind: "reference" }),
      titles
    )
    expect(groups.map((g) => g.label)).toEqual(["Taxes 2026", "y"])
  })
})
