import { describe, expect, it } from "vitest"

import type { ChangeRow, KindInfo } from "@/lib/api/types"
import {
  dayLabel,
  foldHistory,
  groupByDay,
  historyEntries,
  historyVerb,
  isSystemChange,
  kindsByReference,
  layoutSummary,
  propertyLabel,
  systemPhrase,
  viewActors,
} from "./history"
import { netMoves } from "./change-values"

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

describe("systemPhrase", () => {
  const CORE = "substrate.reamde.dev/core"
  const GOOGLE = "providers.substrate.reamde.dev/google"
  const GOOGLE_ACTOR = "bundle:providers.substrate.reamde.dev:google"

  function said(rows: ChangeRow[]) {
    const [entry] = foldHistory(rows)
    const moves =
      entry.records.length === 1
        ? netMoves(entry.rows, entry.records[0], entry.kind)
        : undefined
    return systemPhrase(entry, moves)
  }
  function versioned(kind: string, id: string, actor: string): ChangeRow {
    return row({
      kind: `${CORE}/${kind}`,
      recordId: id,
      actor,
      payload: { properties: ["version"] },
      affected: [
        {
          kind: `${CORE}/${kind}`,
          id,
          version: 3,
          properties: [{ name: "version", before: 34, after: 35 }],
        },
      ],
    })
  }

  it("says a provider moving to a new version", () => {
    expect(said([versioned("package", GOOGLE, GOOGLE_ACTOR)])?.words).toBe(
      "updated its package to version 35"
    )
    expect(said([versioned("bundle", GOOGLE, GOOGLE_ACTOR)])?.words).toBe(
      "updated to version 35"
    )
    expect(said([versioned("package", GOOGLE, "console")])?.words).toBe(
      "updated the Google package to version 35"
    )
  })

  it("says a sample package whose only change is the host's bookkeeping", () => {
    const phrase = said([
      row({
        kind: `${CORE}/package`,
        recordId: "ada.example.com/notes",
        actor: "bundle:ada.example.com:notes",
        payload: { properties: ["originDigest"] },
        affected: [
          {
            kind: `${CORE}/package`,
            id: "ada.example.com/notes",
            version: 2,
            properties: [
              { name: "originDigest", before: "8200", after: "075e" },
            ],
          },
        ],
      }),
    ])
    expect(phrase).toEqual({ words: "updated its package", complete: true })
  })

  it("names collections, tools and agents", () => {
    const kinds = said(
      ["gmailthread", "contact"].map((n) =>
        row({
          kind: `${CORE}/kind`,
          recordId: `${GOOGLE}/${n}`,
          actor: GOOGLE_ACTOR,
        })
      )
    )
    expect(kinds?.words).toBe("updated 2 collections")
    const one = said([
      row({ kind: `${CORE}/kind`, recordId: `${GOOGLE}/gmailthread` }),
    ])
    expect(one).toMatchObject({
      words: "updated the",
      collection: `${GOOGLE}/gmailthread`,
      tail: "collection",
    })
    expect(
      said([row({ kind: `${CORE}/function`, recordId: `${GOOGLE}/syncgmail` })])
        ?.words
    ).toBe("updated the tool Google Gmail sync")
    expect(
      said([
        row({
          kind: `${CORE}/agent`,
          recordId: "ada.example.com/notes/titler",
          payload: { created: true },
        }),
      ])?.words
    ).toBe("added the agent Titler")
  })

  it("says the console layout by what moved", () => {
    const phrase = said([
      row({
        kind: `${CORE}/consolepreference`,
        recordId: "navigation",
        op: "patch",
        payload: { properties: ["sidebarOpen", "tableWidth", "collapsed"] },
        affected: [
          {
            kind: `${CORE}/consolepreference`,
            id: "navigation",
            version: 6,
            properties: [
              { name: "sidebarOpen", before: true, after: false },
              { name: "tableWidth", before: "wide", after: "full" },
              { name: "collapsed", after: [] },
            ],
          },
        ],
      }),
    ])
    expect(phrase?.words).toBe(
      "changed your console layout (sidebar closed, table width Full)"
    )
    expect(layoutSummary(["density", "sidebarOpen"], undefined)).toBe(
      "rows, sidebar"
    )
  })

  it("says sign-ins and runs", () => {
    expect(
      said([
        row({
          kind: `${CORE}/token`,
          actor: "substrate",
          payload: { created: true },
        }),
      ])?.words
    ).toBe("signed you in")
    expect(
      said([row({ kind: `${CORE}/token`, actor: "console", op: "delete" })])
        ?.words
    ).toBe("signed out")
    expect(
      said(
        [1, 2, 3].map(() =>
          row({
            kind: `${CORE}/triggerrun`,
            actor: "substrate",
            payload: { created: true },
          })
        )
      )?.words
    ).toBe("recorded 3 runs")
  })

  it("leaves a person's own records to the ordinary sentence", () => {
    expect(said([row({})])).toBeUndefined()
  })
})

describe("historyEntries", () => {
  it("leaves housekeeping out unless technical details are on", () => {
    const rows = [row({ op: "gc" }), row({})]
    expect(historyEntries(rows, false)).toHaveLength(1)
    expect(historyEntries(rows, true)).toHaveLength(2)
  })
})
