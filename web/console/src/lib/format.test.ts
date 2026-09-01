/** The table datetime voice: local timezone, `Aug 6, 01:10`, year only when
 * it differs. Expected values are built from Date's own local parts so the
 * tests hold in any timezone. */

import { describe, expect, it } from "vitest"

import {
  cellValue,
  referenceCell,
  referenceID,
  referenceObjects,
  shortDate,
  tableDateTime,
} from "./format"

const pad = (n: number) => String(n).padStart(2, "0")
const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
]

describe("tableDateTime", () => {
  it("renders the LOCAL month-day and time", () => {
    const iso = "2026-08-06T01:10:00Z"
    const d = new Date(iso)
    const now = Date.parse("2026-08-20T12:00:00Z")
    expect(tableDateTime(iso, now)).toBe(
      `${MONTHS[d.getMonth()]} ${d.getDate()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
    )
  })

  it("adds the year once it differs from now's", () => {
    const iso = "2025-03-04T09:05:00Z"
    const d = new Date(iso)
    const now = Date.parse("2026-08-20T12:00:00Z")
    expect(tableDateTime(iso, now)).toBe(
      `${MONTHS[d.getMonth()]} ${d.getDate()} ${d.getFullYear()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
    )
  })

  it("passes garbage through untouched", () => {
    expect(tableDateTime("not a date")).toBe("not a date")
  })
})

describe("shortDate", () => {
  it("names the LOCAL calendar day, not the UTC one", () => {
    const iso = "2026-08-06T23:59:00Z"
    const d = new Date(iso)
    expect(shortDate(iso)).toBe(
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    )
  })
})

describe("referenceID / referenceCell", () => {
  it("names the record, not the kind the column already says", () => {
    // The served shape: the path under `ref`, whether or not the declaration
    // hangs link properties off it.
    expect(
      referenceID({ ref: "core.substrate.reamde.dev/llmprovider/claude" })
    ).toBe("claude")
    expect(referenceID({ ref: "tasks.example.com/task/abc123" })).toBe("abc123")
    expect(referenceID({ ref: "tasks.example.com/task/p1", round: 2 })).toBe(
      "p1"
    )
    expect(
      referenceCell([
        { ref: "tasks.example.com/task/a" },
        { ref: "tasks.example.com/task/b" },
      ])
    ).toBe("a, b")
  })

  it("still reads the bare path, the write-time shorthand", () => {
    // A path string reaches these functions from an authored document the
    // console has not sent yet; the server serves the object.
    expect(referenceID("tasks.example.com/task/abc123")).toBe("abc123")
    expect(
      referenceCell(["tasks.example.com/task/a", "tasks.example.com/task/b"])
    ).toBe("a, b")
  })

  it("answers nothing for a value that is not a path", () => {
    expect(referenceID("claude")).toBe("")
    expect(referenceID({ kind: "task", id: "a" })).toBe("")
  })

  it("is the datatype's job, never cellValue's guess at a slash", () => {
    // cellValue is datatype-blind, so it must leave a URL (and any other
    // slashed string) whole rather than reading it as a path.
    expect(cellValue("https://example.com/x")).toBe("https://example.com/x")
  })
})

describe("referenceObjects", () => {
  it("reads the served object, one or repeated", () => {
    const one = { ref: "tasks.example.com/task/a" }
    expect(referenceObjects(one)).toEqual([one])
    const link = { ref: "tasks.example.com/task/a", round: 2 }
    expect(referenceObjects(link)).toEqual([link])
    expect(referenceObjects([one, link])).toEqual([one, link])
  })

  it("refuses everything a declaration would have had to vouch for", () => {
    // A bare path string: undeclared, it is a string with slashes in it.
    expect(referenceObjects("tasks.example.com/task/a")).toBeUndefined()
    // An object that names no path, and a `ref` that is not one.
    expect(referenceObjects({ kind: "task", id: "a" })).toBeUndefined()
    expect(referenceObjects({ ref: "claude" })).toBeUndefined()
    // One non-reference element disqualifies the whole list: half a column of
    // pills and half of JSON would read as two different datatypes.
    expect(
      referenceObjects([{ ref: "tasks.example.com/task/a" }, { n: 1 }])
    ).toBeUndefined()
    expect(referenceObjects([])).toBeUndefined()
    expect(referenceObjects(null)).toBeUndefined()
  })
})
