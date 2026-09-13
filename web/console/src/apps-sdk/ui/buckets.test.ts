/** The pure half of the kit's lists: the six buckets in order, the relative
 * phrase, the local day fold a timeline does, and how a `List` sections by
 * a property or a function. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  bucketOf,
  dayKey,
  dayLabel,
  groupByDay,
  groupRecords,
  initialsOf,
  relativeDay,
  sectionOf,
  timeSpan,
  timelineRows,
  titleOf,
} from "./buckets"

const DAY = 86_400_000
// A local noon, so a day offset never crosses midnight in any zone.
const now = new Date(2026, 8, 12, 12, 0, 0).getTime()
const at = (days: number, hours = 0) =>
  new Date(now + days * DAY + hours * 3_600_000).toISOString()

const row = (
  id: string,
  properties: Record<string, unknown>,
  kind = "ada.example.com/tasks/task"
): SubstrateRecord => ({
  id,
  kind,
  properties,
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

describe("bucketOf", () => {
  it("files an instant into one of six buckets", () => {
    expect(bucketOf(at(-1), now)).toBe("overdue")
    expect(bucketOf(at(0, -1), now)).toBe("overdue")
    expect(bucketOf(at(0, 2), now)).toBe("today")
    expect(bucketOf(at(1), now)).toBe("tomorrow")
    expect(bucketOf(at(2), now)).toBe("week")
    expect(bucketOf(at(6), now)).toBe("week")
    expect(bucketOf(at(7), now)).toBe("later")
    expect(bucketOf(undefined, now)).toBe("undated")
    expect(bucketOf("not a date", now)).toBe("undated")
  })
})

describe("relativeDay", () => {
  it("speaks in days across midnight and in hours and minutes inside today", () => {
    expect(relativeDay(at(1), now)).toBe("tomorrow")
    expect(relativeDay(at(-1), now)).toBe("yesterday")
    expect(relativeDay(at(3), now)).toBe("in 3 days")
    expect(relativeDay(at(0, 2), now)).toBe("in 2 hours")
    expect(relativeDay(new Date(now + 30_000).toISOString(), now)).toBe(
      "just now"
    )
    expect(relativeDay("garbage", now)).toBe("garbage")
  })
})

describe("the day fold", () => {
  it("keys a local day and names today, tomorrow and yesterday", () => {
    expect(dayKey(now)).toBe("2026-09-12")
    expect(dayLabel("2026-09-12", now)).toBe("Today")
    expect(dayLabel("2026-09-13", now)).toBe("Tomorrow")
    expect(dayLabel("2026-09-11", now)).toBe("Yesterday")
    expect(dayLabel("2026-09-15", now)).toMatch(/Tuesday/)
  })

  it("reads at, else dueAt, drops the undated and folds by day in order", () => {
    const rows = timelineRows([
      row("t2", { name: "Later", dueAt: at(1, 1) }),
      row("e1", { title: "Sync", at: at(0, -3), endsAt: at(0, -2.5) }),
      row("t1", { name: "Call", dueAt: at(0, -1) }),
      row("none", { name: "Undated" }),
    ])
    expect(rows.map((r) => r.record.id)).toEqual(["e1", "t1", "t2"])
    expect(rows[0].endsAt).toBeDefined()
    const days = groupByDay(rows)
    expect(days.map((d) => [d.key, d.rows.length])).toEqual([
      ["2026-09-12", 2],
      ["2026-09-13", 1],
    ])
  })

  it("shows a range as start–end and a cross-day end as its day", () => {
    const start = new Date(2026, 8, 12, 9, 0).getTime()
    expect(timeSpan(start)).toBe("09:00")
    expect(timeSpan(start, start + 30 * 60_000)).toBe("09:00–09:30")
    expect(timeSpan(start, start + DAY)).toBe("09:00–2026-09-13")
  })
})

describe("groupRecords", () => {
  it("buckets a datetime property from Overdue to Undated, in that order", () => {
    const groups = groupRecords(
      [
        row("later", { dueAt: at(20) }),
        row("none", {}),
        row("today", { dueAt: at(0, 1) }),
        row("week", { dueAt: at(3) }),
        row("late", { dueAt: at(-1) }),
        row("tmrw", { dueAt: at(1) }),
      ],
      "dueAt",
      undefined,
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

  it("trusts the declaration over the values' shape", () => {
    const groups = groupRecords(
      [row("a", { dueAt: at(1) })],
      "dueAt",
      { kind: "string" },
      now
    )
    expect(groups.map((g) => g.label)).toEqual([at(1)])
  })

  it("orders a state by declaration, labels an enum, and puts the valueless last", () => {
    const states = groupRecords(
      [
        row("b", { status: "done" }),
        row("a", { status: "open" }),
        row("c", {}),
      ],
      "status",
      { kind: "state", states: ["open", "done"] }
    )
    expect(states.map((g) => g.label)).toEqual(["open", "done", "None"])
    const enums = groupRecords(
      [row("x", { size: "l" }), row("y", { size: "s" })],
      "size",
      {
        kind: "enum",
        values: [
          { value: "s", label: "Small" },
          { value: "l", label: "Large" },
        ],
      }
    )
    expect(enums.map((g) => [g.key, g.label])).toEqual([
      ["s", "Small"],
      ["l", "Large"],
    ])
  })

  it("keeps arrival order without a declaration, and names a reference by its path", () => {
    const groups = groupRecords(
      [
        row("1", { project: { ref: "p/website" } }),
        row("2", { project: { ref: "p/app" } }),
        row("3", { project: { ref: "p/website" } }),
      ],
      "project"
    )
    expect(groups.map((g) => [g.label, g.records.length])).toEqual([
      ["p/website", 2],
      ["p/app", 1],
    ])
  })

  it("sorts a function's single-letter keys as an index and keeps other keys as they come", () => {
    const people = [
      row("z", { name: "Zoe" }),
      row("q", { name: "?" }),
      row("a", { name: "Ada" }),
    ]
    const index = groupRecords(people, (p) =>
      sectionOf(String(p.properties.name))
    )
    expect(index.map((g) => g.key)).toEqual(["A", "Z", "#"])
    const named = groupRecords(people, (p) =>
      p.id === "a" ? "Waiting on me" : "Mine"
    )
    expect(named.map((g) => g.key)).toEqual(["Mine", "Waiting on me"])
  })
})

describe("the text helpers", () => {
  it("files a heading under its first letter, accents stripped, the rest under #", () => {
    expect(sectionOf("Émile")).toBe("E")
    expect(sectionOf("ada")).toBe("A")
    expect(sectionOf("42 things")).toBe("#")
    expect(sectionOf("")).toBe("#")
  })
  it("takes two initials and titles a record from title, name, summary, id", () => {
    expect(initialsOf("Ada Lovelace")).toBe("AL")
    expect(initialsOf("  grace ")).toBe("G")
    expect(titleOf(row("x", { title: "T", name: "N" }))).toBe("T")
    expect(titleOf(row("x", { name: "N" }))).toBe("N")
    expect(titleOf(row("x", { summary: "S" }))).toBe("S")
    expect(titleOf(row("x", {}))).toBe("x")
  })
})
