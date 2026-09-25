import { describe, expect, it } from "vitest"

import type { ChangeRow, KindInfo } from "@/lib/api/types"
import {
  dayLabel,
  foldHistory,
  groupByDay,
  historyVerb,
  isSystemChange,
  kindsByReference,
  propertyLabel,
  viewActors,
} from "./history"

let seq = 100
function row(over: Partial<ChangeRow>): ChangeRow {
  seq -= 1
  return {
    seq,
    ts: "2026-09-24T12:00:00Z",
    actor: "console",
    op: "put",
    recordId: `r${seq}`,
    kind: "ada.example.com/tasks/task",
    ...over,
  }
}

describe("historyVerb", () => {
  it("says what a row did", () => {
    expect(historyVerb(row({ payload: { created: true } }))).toBe("added")
    expect(historyVerb(row({}))).toBe("changed")
    expect(historyVerb(row({ payload: { restored: true } }))).toBe("restored")
    expect(historyVerb(row({ op: "patch" }))).toBe("changed")
    expect(historyVerb(row({ op: "delete" }))).toBe("deleted")
    expect(historyVerb(row({ op: "gc" }))).toBe("cleaned up")
  })
})

describe("foldHistory", () => {
  it("folds a run of the same actor adding the same kind", () => {
    const rows = [
      row({ payload: { created: true }, ts: "2026-09-24T12:03:00Z" }),
      row({ payload: { created: true }, ts: "2026-09-24T12:02:00Z" }),
      row({ payload: { created: true }, ts: "2026-09-24T12:01:00Z" }),
    ]
    const folded = foldHistory(rows)
    expect(folded).toHaveLength(1)
    expect(folded[0].verb).toBe("added")
    expect(folded[0].records).toHaveLength(3)
    expect(folded[0].ts).toBe("2026-09-24T12:03:00Z")
  })

  it("breaks a run on another actor, another kind or a long gap", () => {
    const rows = [
      row({ payload: { created: true }, ts: "2026-09-24T12:30:00Z" }),
      row({ payload: { created: true }, ts: "2026-09-24T12:00:00Z" }),
      row({
        payload: { created: true },
        actor: "agent:ada.example.com:llm:helper",
        ts: "2026-09-24T11:59:00Z",
      }),
      row({
        payload: { created: true },
        kind: "ada.example.com/people/person",
        ts: "2026-09-24T11:58:00Z",
      }),
    ]
    expect(foldHistory(rows)).toHaveLength(4)
  })

  it("collects the properties a record's changes named, once each", () => {
    const rows = [
      row({ recordId: "t1", payload: { properties: ["status"] } }),
      row({ recordId: "t1", payload: { properties: ["status", "dueAt"] } }),
    ]
    const [entry] = foldHistory(rows)
    expect(entry.records).toEqual(["t1"])
    expect(entry.properties).toEqual(["status", "dueAt"])
  })
})

describe("days", () => {
  const now = Date.parse("2026-09-24T15:00:00")
  it("labels today and yesterday by name", () => {
    expect(dayLabel("2026-09-24T09:00:00", now)).toBe("Today")
    expect(dayLabel("2026-09-23T09:00:00", now)).toBe("Yesterday")
    expect(dayLabel("2026-09-20T09:00:00", now)).not.toBe("Yesterday")
  })

  it("groups entries under the day they happened", () => {
    const entries = foldHistory([
      row({ ts: "2026-09-24T10:00:00", recordId: "a" }),
      row({ ts: "2026-09-23T10:00:00", actor: "substratectl" }),
    ])
    const days = groupByDay(entries, now)
    expect(days.map((d) => d.label)).toEqual(["Today", "Yesterday"])
  })
})

describe("propertyLabel", () => {
  it("reads a property key as words", () => {
    expect(propertyLabel("dueAt")).toBe("Due at")
    expect(propertyLabel("display_name")).toBe("Display name")
    expect(propertyLabel("status")).toBe("Status")
  })
})

describe("viewActors", () => {
  const sources = {
    actors: ["console", "substratectl", "substrate", "bundle:core"],
    agents: ["ada.example.com/llm/helper"],
    functions: [
      "providers.substrate.reamde.dev/google/synccontacts",
      "ada.example.com/notes/stats",
    ],
    bundles: ["providers.substrate.reamde.dev/google", "ada.example.com/tasks"],
  }
  it("reads everything without an actor filter", () => {
    expect(viewActors("everything", sources)).toBeUndefined()
  })
  it("reads the person's doors for By you", () => {
    expect(viewActors("you", sources)).toEqual([
      "api",
      "console",
      "substratectl",
    ])
  })
  it("derives agent and provider actors from their declarations", () => {
    expect(viewActors("agents", sources)).toEqual([
      "agent:ada.example.com:llm:helper",
    ])
    expect(viewActors("providers", sources)).toEqual([
      "bundle:providers.substrate.reamde.dev:google",
      "function:providers.substrate.reamde.dev:google:synccontacts",
    ])
  })
})

function kindInfo(identity: string, purpose?: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "builtin",
    description: "",
    definition: purpose ? { purpose } : {},
  }
}

describe("system changes", () => {
  const kinds = kindsByReference([
    kindInfo("ada.example.com/tasks/task"),
    kindInfo("ada.example.com/tasks/checklist", "supporting"),
    kindInfo("ada.example.com/tasks/cursor", "internal"),
    kindInfo("substrate.reamde.dev/core/triggerrun"),
  ])

  it("is a write to an internal kind, core's included", () => {
    const at = (kind: string) => isSystemChange(row({ kind }), kinds)
    expect(at("ada.example.com/tasks/task")).toBe(false)
    expect(at("ada.example.com/tasks/checklist")).toBe(false)
    expect(at("ada.example.com/tasks/cursor")).toBe(true)
    expect(at("substrate.reamde.dev/core/triggerrun")).toBe(true)
  })

  it("judges a kind the registry no longer carries by its reference", () => {
    const at = (kind: string) => isSystemChange(row({ kind }), new Map())
    expect(at("substrate.reamde.dev/core/token")).toBe(true)
    expect(at("ada.example.com/gone/thing")).toBe(false)
  })
})
