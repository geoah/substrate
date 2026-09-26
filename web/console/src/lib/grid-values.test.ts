import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import {
  doneStateProperty,
  dueTone,
  emptyColumnIds,
  enumLabel,
  friendlyDate,
  friendlyDay,
  hiddenKindsNote,
  isDueColumn,
  isEmptyValue,
  propertyLabel,
  subtaskCounts,
  gridSummary,
  titleBacking,
  titleProperties,
} from "./grid-values"

describe("propertyLabel", () => {
  it.each([
    ["name", "Name"],
    ["memberOf", "Member of"],
    ["recurrenceOf", "Recurrence of"],
    ["dueAt", "Due"],
    ["updatedAt", "Updated"],
    ["at", "When"],
    ["endsAt", "Ends"],
    ["url", "URL"],
    ["displayName", "Display name"],
    ["last_seen", "Last seen"],
  ])("%s reads %s", (key, label) => {
    expect(propertyLabel(key)).toBe(label)
  })
})

describe("enumLabel", () => {
  const prop: DeclaredProperty = {
    name: "relationship",
    kind: "enum",
    repeated: false,
    values: [
      { value: "friend", label: "" },
      { value: "publicfigure", label: "Public figure" },
    ],
  }
  it("takes the authored label and capitalises the rest", () => {
    expect(enumLabel(prop, "publicfigure")).toBe("Public figure")
    expect(enumLabel(prop, "friend")).toBe("Friend")
  })
})

describe("friendlyDate", () => {
  // A Thursday, mid-afternoon, local time.
  const now = new Date(2026, 8, 24, 15, 0).getTime()
  const at = (d: number, h = 9) => new Date(2026, 8, d, h, 0).toISOString()

  it("says the near days in words and the rest as a date", () => {
    expect(friendlyDate(at(24), now)).toBe("Today")
    expect(friendlyDate(at(25), now)).toBe("Tomorrow")
    expect(friendlyDate(at(23), now)).toBe("Yesterday")
    expect(friendlyDate(at(28), now)).toBe("Mon")
    expect(friendlyDate(at(20), now)).toBe("20 Sep")
    expect(friendlyDate(new Date(2025, 9, 3).toISOString(), now)).toBe(
      "3 Oct 2025"
    )
  })

  it("adds the clock when asked", () => {
    expect(friendlyDate(at(24, 14), now, { time: true })).toBe("Today, 14:00")
  })

  it("reads a bare date as a calendar day", () => {
    expect(friendlyDay("2026-09-25", now)).toBe("Tomorrow")
  })

  it("leaves a value that is not a date alone", () => {
    expect(friendlyDate("soon", now)).toBe("soon")
  })
})

describe("dueTone", () => {
  const now = Date.parse("2026-09-24T12:00:00Z")
  it("colours a deadline by how close it is", () => {
    expect(dueTone("2026-09-24T09:00:00Z", now)).toBe("overdue")
    expect(dueTone("2026-09-25T09:00:00Z", now)).toBe("soon")
    expect(dueTone("2026-09-30T09:00:00Z", now)).toBeUndefined()
  })
  it("mutes it once the record is done", () => {
    expect(dueTone("2026-09-20T09:00:00Z", now, true)).toBe("done")
  })
  it("knows a deadline by its name", () => {
    expect(isDueColumn("dueAt")).toBe(true)
    expect(isDueColumn("at")).toBe(false)
  })
})

describe("isEmptyValue", () => {
  it("counts blanks, empty lists and empty maps as nothing", () => {
    for (const v of [undefined, null, "", "  ", [], [""], {}]) {
      expect(isEmptyValue(v)).toBe(true)
    }
    for (const v of [0, false, "x", ["a"], { ref: "x" }]) {
      expect(isEmptyValue(v)).toBe(false)
    }
  })
})

function kind(definition: Record<string, unknown>): KindInfo {
  return {
    identity: "ada.example.com/people/person",
    name: "person",
    authority: "ada.example.com",
    package: "people",
    version: 1,
    source: "installed",
    description: "",
    definition,
  }
}

describe("titleBacking", () => {
  it("is the property a single-slot title is, its first choice", () => {
    expect(titleBacking(kind({ displayTemplate: "{name}" }))).toBe("name")
    expect(
      titleBacking(kind({ displayTemplate: "{displayName|localName}" }))
    ).toBe("displayName")
  })

  it("is none for a composed title, a path, or the title itself", () => {
    expect(
      titleBacking(kind({ displayTemplate: "{state} #{localName}" }))
    ).toBeUndefined()
    expect(
      titleBacking(kind({ displayTemplate: "{user.name}" }))
    ).toBeUndefined()
    expect(titleBacking(kind({ displayTemplate: "{title}" }))).toBeUndefined()
    expect(
      titleBacking(kind({ displayTemplate: "Slack connection" }))
    ).toBeUndefined()
    expect(titleBacking(kind({}))).toBeUndefined()
  })
})

describe("titleProperties", () => {
  it("names what the display template reads", () => {
    expect(
      titleProperties(kind({ displayTemplate: "{displayName|name}" }))
    ).toEqual(["displayName", "name"])
    expect(titleProperties(kind({ displayTemplate: "{name|title}" }))).toEqual([
      "name",
    ])
    expect(titleProperties(kind({}))).toEqual([])
  })
})

function row(id: string, properties: Record<string, unknown>): SubstrateRecord {
  return { id, properties } as unknown as SubstrateRecord
}

describe("emptyColumnIds", () => {
  it("names the columns nothing on the page fills in", () => {
    const rows = [row("a", { x: 1, y: "" }), row("b", { x: 2, z: [] })]
    const value = (r: SubstrateRecord, id: string) => r.properties[id]
    expect(emptyColumnIds(["x", "y", "z"], rows, value)).toEqual(["y", "z"])
  })
  it("hides nothing before there are rows to judge by", () => {
    expect(emptyColumnIds(["x"], [], () => undefined)).toEqual([])
  })
})

describe("subtaskCounts", () => {
  const status: DeclaredProperty = {
    name: "status",
    kind: "state",
    repeated: false,
    states: ["open", "done"],
    initial: "open",
  }
  it("counts the finished children where the kind can finish", () => {
    expect(doneStateProperty([status])).toBe(status)
    const kids = [
      row("a", { status: "done" }),
      row("b", { status: "open" }),
      row("c", { status: "done" }),
    ]
    expect(subtaskCounts(kids, status)).toEqual({ total: 3, done: 2 })
    expect(subtaskCounts(kids, undefined)).toEqual({ total: 3 })
  })
})

describe("hiddenKindsNote", () => {
  it("names two of what it leaves out", () => {
    const k = (name: string) => ({
      ...kind({}),
      identity: `ada.example.com/mail/${name}`,
      name,
    })
    expect(hiddenKindsNote([k("label"), k("attachment"), k("address")])).toBe(
      "3 more hold supporting details (like labels and attachments). You see them from the records they belong to, or here with Technical details on."
    )
    expect(hiddenKindsNote([])).toBe("")
  })
})

describe("gridSummary", () => {
  const nouns: [string, string] = ["task", "tasks"]
  const at = { page: 1, pageSize: 50, nouns }

  it("says how many there are, one fact", () => {
    expect(gridSummary({ ...at, total: { value: 72 }, rows: 50 })).toBe(
      "1–50 of 72 tasks"
    )
    expect(gridSummary({ ...at, total: { value: 12 }, rows: 12 })).toBe(
      "12 tasks"
    )
    expect(gridSummary({ ...at, total: { value: 1 }, rows: 1 })).toBe("1 task")
    expect(gridSummary({ ...at, rows: 12 })).toBeUndefined()
  })

  it("leads with the range when it pages, a floor saying so", () => {
    expect(
      gridSummary({
        ...at,
        page: 3,
        total: { value: 10000, capped: true },
        rows: 50,
      })
    ).toBe("101–150 of 10,000+ tasks")
  })

  it("says the top level beside the whole only where they differ", () => {
    expect(
      gridSummary({ ...at, all: { value: 72 }, total: { value: 60 }, rows: 50 })
    ).toBe("72 tasks · 60 at the top level")
    expect(
      gridSummary({ ...at, all: { value: 72 }, total: { value: 72 }, rows: 50 })
    ).toBe("72 tasks")
  })
})
