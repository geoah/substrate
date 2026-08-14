import { describe, expect, it } from "vitest"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  decisionOf,
  deriveDiff,
  evidenceSignals,
  sameValue,
  signalText,
  verdictNote,
  verdictPatch,
} from "@/lib/mergerequests"

// ── evidence extraction ─────────────────────────────────────────────────────

describe("evidenceSignals", () => {
  it("reads the dedupe function's real shape: email carries value, name carries jaccard", () => {
    // Verbatim off the live wire (dupe-ldsx4l7jw7o6-p3w6en47ktph).
    const evidence = {
      loser: "ldsx4l7jw7o6",
      winner: "p3w6en47ktph",
      signals: [
        { signal: "email", value: "alice@example.com" },
        { jaccard: 1, signal: "name" },
      ],
    }
    expect(evidenceSignals(evidence)).toEqual([
      { kind: "email", value: "alice@example.com", score: undefined },
      { kind: "name", value: undefined, score: 1 },
    ])
  })

  it("accepts a generic score field", () => {
    const signals = evidenceSignals({
      signals: [{ signal: "phone", score: 0.5 }],
    })
    expect(signals).toEqual([{ kind: "phone", value: undefined, score: 0.5 }])
  })

  it("yields nothing for junk instead of guessing", () => {
    expect(evidenceSignals(undefined)).toEqual([])
    expect(evidenceSignals("text")).toEqual([])
    expect(evidenceSignals({ signals: "not-a-list" })).toEqual([])
    expect(evidenceSignals({ signals: [{ value: "no signal name" }] })).toEqual(
      []
    )
    expect(evidenceSignals({ signals: [null] })).toEqual([])
  })
})

describe("signalText", () => {
  it("says the email match plainly", () => {
    expect(signalText({ kind: "email", value: "alex@acme.com" })).toBe(
      "both carry alex@acme.com"
    )
  })

  it("says the name score plainly", () => {
    expect(signalText({ kind: "name", score: 0.7 })).toBe("names match, 0.70")
  })

  it("keeps unknown signals verbatim", () => {
    expect(signalText({ kind: "phone", value: "+3069", score: 0.5 })).toBe(
      "phone, +3069, 0.50"
    )
    expect(signalText({ kind: "mystery" })).toBe("mystery")
  })
})

// ── value equality ──────────────────────────────────────────────────────────

describe("sameValue", () => {
  it("compares repeated properties as multisets", () => {
    expect(sameValue(["a", "b"], ["b", "a"])).toBe(true)
    expect(sameValue(["a", "a"], ["a"])).toBe(false)
  })

  it("compares objects structurally, key order aside", () => {
    expect(sameValue({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(true)
    expect(sameValue({ a: 1 }, { a: 2 })).toBe(false)
  })

  it("distinguishes absent from empty", () => {
    expect(sameValue(undefined, "")).toBe(false)
    expect(sameValue(undefined, undefined)).toBe(true)
  })
})

// ── diff derivation ─────────────────────────────────────────────────────────

const personType: KindInfo = {
  identity: "people.substrate.reamde.dev/person",
  name: "person",
  authority: "people.substrate.reamde.dev",
  version: "",
  plural: "people",
  source: "installed",
  definition: {
    properties: {
      name: { type: "string", description: "the person's name" },
      emails: { type: "email", repeated: true },
      prominence: { type: "state", states: ["known", "utility"] },
    },
    edges: {
      memberOf: { to: "organization", description: "organizations joined" },
    },
  },
}

function person(
  id: string,
  properties: Record<string, unknown>,
  meta?: Record<string, string>,
  edges?: SubstrateRecord["edges"]
): SubstrateRecord {
  return {
    id,
    kind: "people.substrate.reamde.dev/person",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-08-05T00:00:00Z",
    updatedAt: "2026-08-05T00:00:00Z",
    edges,
    propertyMeta: meta
      ? Object.fromEntries(
          Object.entries(meta).map(([k, manager]) => [
            k,
            { manager, updatedAt: "2026-08-05T00:00:00Z" },
          ])
        )
      : undefined,
  }
}

describe("deriveDiff", () => {
  it("classifies equal, machine-held differing, and owner-held differing", () => {
    const winner = person(
      "w",
      { name: "George", emails: ["a@x.gr"], prominence: "known" },
      { name: "owner", emails: "github.connectors.substrate.reamde.dev" }
    )
    const loser = person(
      "l",
      { name: "Georgios", emails: ["b@x.gr"], prominence: "known" },
      {
        name: "github.connectors.substrate.reamde.dev",
        emails: "github.connectors.substrate.reamde.dev",
      }
    )
    const rows = deriveDiff(winner, loser, personType)
    const byKey = Object.fromEntries(rows.map((r) => [r.key, r]))
    // name differs and the winner holds it → the owner's choice.
    expect(byKey.name.posture).toBe("choice")
    // emails differ, both machine-held → recompute settles it.
    expect(byKey.emails.posture).toBe("recompute")
    expect(byKey.prominence.posture).toBe("equal")
  })

  it("flags owner-held when the LOSER side is the owner's", () => {
    const winner = person(
      "w",
      { name: "A" },
      { name: "github.connectors.substrate.reamde.dev" }
    )
    const loser = person("l", { name: "B" }, { name: "owner" })
    const rows = deriveDiff(winner, loser, personType)
    expect(rows.find((r) => r.key === "name")?.posture).toBe("choice")
  })

  it("treats repeated properties order-insensitively", () => {
    const winner = person("w", { emails: ["a@x.gr", "b@x.gr"] })
    const loser = person("l", { emails: ["b@x.gr", "a@x.gr"] })
    const rows = deriveDiff(winner, loser, personType)
    expect(rows.find((r) => r.key === "emails")?.posture).toBe("equal")
  })

  it("includes undeclared properties either side carries, marked undeclared", () => {
    const winner = person("w", { name: "A", title: "A" })
    const loser = person("l", { name: "A", title: "A", phones: ["+30"] })
    const rows = deriveDiff(winner, loser, personType)
    const title = rows.find((r) => r.key === "title")
    const phones = rows.find((r) => r.key === "phones")
    expect(title?.declared).toBe(false)
    expect(title?.posture).toBe("equal")
    // present on one side only = a difference, machine-held by default.
    expect(phones?.declared).toBe(false)
    expect(phones?.posture).toBe("recompute")
  })

  it("diffs edges by target id set and postures a difference as moves", () => {
    const winner = person("w", {}, undefined, {
      memberOf: [
        { id: "org1", kind: "people.substrate.reamde.dev/organization" },
      ],
    })
    const loser = person("l", {}, undefined, {
      memberOf: [
        { id: "org1", kind: "people.substrate.reamde.dev/organization" },
        { id: "org2", kind: "people.substrate.reamde.dev/organization" },
      ],
    })
    const rows = deriveDiff(winner, loser, personType)
    const edge = rows.find((r) => r.key === "memberOf")
    expect(edge?.kind).toBe("edge")
    expect(edge?.posture).toBe("moves")
    expect(edge?.description).toBe("organizations joined")
  })

  it("orders what needs the owner first, equal rows last", () => {
    const winner = person(
      "w",
      { name: "A", emails: ["a@x.gr"], prominence: "known" },
      { name: "owner" },
      {
        memberOf: [
          { id: "org1", kind: "people.substrate.reamde.dev/organization" },
        ],
      }
    )
    const loser = person(
      "l",
      { name: "B", emails: ["b@x.gr"], prominence: "known" },
      undefined,
      {
        memberOf: [
          { id: "org2", kind: "people.substrate.reamde.dev/organization" },
        ],
      }
    )
    const rows = deriveDiff(winner, loser, personType)
    expect(rows.map((r) => r.posture)).toEqual([
      "choice",
      "recompute",
      "moves",
      "equal",
    ])
  })

  it("skips declared properties neither side carries", () => {
    const winner = person("w", { name: "A" })
    const loser = person("l", { name: "A" })
    const rows = deriveDiff(winner, loser, personType)
    expect(rows.find((r) => r.key === "emails")).toBeUndefined()
    expect(rows.find((r) => r.key === "prominence")).toBeUndefined()
  })

  it("carries the record-56 description onto declared rows", () => {
    const winner = person("w", { name: "A" })
    const loser = person("l", { name: "B" })
    const rows = deriveDiff(winner, loser, personType)
    expect(rows.find((r) => r.key === "name")?.description).toBe(
      "the person's name"
    )
  })

  it("derives without a schema at all — every present key, undeclared", () => {
    const winner = person("w", { name: "A" })
    const loser = person("l", { name: "B" })
    const rows = deriveDiff(winner, loser, undefined)
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({
      key: "name",
      declared: false,
      posture: "recompute",
    })
  })
})

// ── the verdict patch ───────────────────────────────────────────────────────

describe("verdictPatch", () => {
  it("is the ordinary transition patch", () => {
    expect(verdictPatch("accepted")).toEqual({
      properties: { decision: "accepted" },
    })
    expect(verdictPatch("rejected")).toEqual({
      properties: { decision: "rejected" },
    })
  })

  it("rides the note as the owner/note annotation in the same write", () => {
    expect(verdictPatch("accepted", "  same person, checked emails  ")).toEqual(
      {
        properties: { decision: "accepted" },
        annotations: { "owner/note": "same person, checked emails" },
      }
    )
  })

  it("omits an empty note entirely", () => {
    expect(verdictPatch("rejected", "   ")).toEqual({
      properties: { decision: "rejected" },
    })
    expect(verdictPatch("rejected", "")).toEqual({
      properties: { decision: "rejected" },
    })
  })
})

describe("verdictNote / decisionOf", () => {
  it("reads owner/note back off the record", () => {
    const mr = person("m", {})
    mr.annotations = { "owner/note": "checked", "owner/other": "x" }
    expect(verdictNote(mr)).toBe("checked")
  })

  it("reads any actor's /note key, ignores non-strings", () => {
    const mr = person("m", {})
    mr.annotations = { "app.foo/note": "theirs" }
    expect(verdictNote(mr)).toBe("theirs")
    mr.annotations = { "owner/note": 7 }
    expect(verdictNote(mr)).toBeUndefined()
    mr.annotations = undefined
    expect(verdictNote(mr)).toBeUndefined()
  })

  it("defaults an unreadable decision to proposed", () => {
    expect(decisionOf(person("m", { decision: "accepted" }))).toBe("accepted")
    expect(decisionOf(person("m", { decision: "rejected" }))).toBe("rejected")
    expect(decisionOf(person("m", {}))).toBe("proposed")
    expect(decisionOf(person("m", { decision: 3 }))).toBe("proposed")
  })
})
