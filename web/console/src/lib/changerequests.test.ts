import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  appliesNothing,
  applyConflict,
  changeOp,
  changeTarget,
  deciderOf,
  decisionNote,
  decisionOf,
  decisionPatch,
  deriveChangeRows,
  describeProposedEdge,
  diffCannotApply,
  diffNamesNothing,
  effectiveCAS,
  proposedDiff,
  proposerOf,
  sameApplied,
  targetDrift,
} from "@/lib/changerequests"

const TASK_KIND = "tasks.substrate.reamde.dev/task"

const taskKind: KindInfo = {
  identity: TASK_KIND,
  name: "task",
  authority: "tasks.substrate.reamde.dev",
  version: "v1alpha1",
  plural: "tasks",
  source: "installed",
  definition: {
    properties: {
      summary: { type: "string", description: "what the task is" },
      status: { type: "state", states: ["open", "done"] },
    },
    edges: {
      assignee: { to: "person", description: "who owns it" },
    },
  },
}

/** A change request as the single read serves it. */
function requestRecord(
  properties: Record<string, unknown>,
  opts: {
    edges?: SubstrateRecord["edges"]
    annotations?: Record<string, unknown>
    meta?: Record<string, string>
    version?: number
  } = {}
): SubstrateRecord {
  return {
    id: "cr-1",
    kind: "core.substrate.reamde.dev/recordpatchrequest",
    properties,
    labels: {},
    annotations: opts.annotations,
    version: opts.version ?? 1,
    createdAt: "2026-08-14T00:00:00Z",
    updatedAt: "2026-08-14T00:00:00Z",
    edges: opts.edges,
    propertyMeta: opts.meta
      ? Object.fromEntries(
          Object.entries(opts.meta).map(([k, manager]) => [
            k,
            { manager, updatedAt: "2026-08-14T00:00:00Z" },
          ])
        )
      : undefined,
  }
}

function targetRecord(
  properties: Record<string, unknown>,
  opts: { version?: number; meta?: Record<string, string> } = {}
): SubstrateRecord {
  return {
    id: "task-1",
    kind: TASK_KIND,
    properties,
    labels: {},
    version: opts.version ?? 3,
    createdAt: "2026-08-13T00:00:00Z",
    updatedAt: "2026-08-13T00:00:00Z",
    propertyMeta: opts.meta
      ? Object.fromEntries(
          Object.entries(opts.meta).map(([k, manager]) => [
            k,
            { manager, updatedAt: "2026-08-13T00:00:00Z" },
          ])
        )
      : undefined,
  }
}

const targetEdge = {
  target: [{ id: "task-1", kind: TASK_KIND, title: "Ship the inbox" }],
}

// ── the op ──────────────────────────────────────────────────────────────────

describe("changeOp", () => {
  it("reads an absent op as patch, which is what the declaration says", () => {
    expect(changeOp(requestRecord({}))).toBe("patch")
    expect(changeOp(requestRecord({ op: "" }))).toBe("patch")
  })

  it("reads the three ops", () => {
    expect(changeOp(requestRecord({ op: "patch" }))).toBe("patch")
    expect(changeOp(requestRecord({ op: "create" }))).toBe("create")
    expect(changeOp(requestRecord({ op: "delete" }))).toBe("delete")
  })

  it("refuses to guess at an op it does not know", () => {
    expect(changeOp(requestRecord({ op: "merge" }))).toBeUndefined()
    expect(changeOp(requestRecord({ op: 3 }))).toBeUndefined()
  })
})

// ── the target ──────────────────────────────────────────────────────────────

describe("changeTarget", () => {
  it("reads a patch's target off the edge, title and all", () => {
    const target = changeTarget(requestRecord({}, { edges: targetEdge }))
    expect(target).toEqual({
      kind: TASK_KIND,
      id: "task-1",
      title: "Ship the inbox",
      via: "edge",
    })
  })

  it("reads a create's target off targetKind/targetId, which no edge can name", () => {
    const target = changeTarget(
      requestRecord({ op: "create", targetKind: TASK_KIND, targetId: "task-9" })
    )
    expect(target).toEqual({ kind: TASK_KIND, id: "task-9", via: "declared" })
  })

  it("prefers the declared pair on a create and the edge on a patch", () => {
    const both = {
      targetKind: TASK_KIND,
      targetId: "task-9",
    }
    expect(
      changeTarget(
        requestRecord({ op: "create", ...both }, { edges: targetEdge })
      )?.id
    ).toBe("task-9")
    expect(
      changeTarget(
        requestRecord({ op: "patch", ...both }, { edges: targetEdge })
      )?.id
    ).toBe("task-1")
  })

  it("has no target when nothing names one", () => {
    expect(changeTarget(requestRecord({}))).toBeUndefined()
    expect(
      changeTarget(requestRecord({ op: "create", targetKind: TASK_KIND }))
    ).toBeUndefined()
  })
})

// ── the stored diff ─────────────────────────────────────────────────────────

describe("proposedDiff", () => {
  it("reads properties under properties, edges under edges", () => {
    const diff = proposedDiff(
      requestRecord({
        op: "create",
        diff: {
          properties: { summary: "Write it down" },
          edges: [
            {
              rel: "assignee",
              to: { kind: "people.substrate.reamde.dev/person", id: "p1" },
              properties: { since: "2026-08-14" },
            },
            { rel: "blockedBy", to: { id: "task-2" } },
          ],
        },
      })
    )
    expect(diff.properties).toEqual({ summary: "Write it down" })
    expect(diff.edges).toEqual([
      {
        rel: "assignee",
        kind: "people.substrate.reamde.dev/person",
        id: "p1",
        properties: { since: "2026-08-14" },
      },
      {
        rel: "blockedBy",
        kind: undefined,
        id: "task-2",
        properties: undefined,
      },
    ])
    expect(diff.refused).toEqual([])
    expect(diff.unreadable).toBe(false)
  })

  it("names the top-level keys the strict decode would refuse", () => {
    // The wrapper-less shape a real model writes: `saved` is not a PatchInput
    // field, so the accept fails the decode rather than applying nothing.
    expect(
      proposedDiff(requestRecord({ diff: { saved: true } })).refused
    ).toEqual(["saved"])
    // `edges` decodes on a create (PutInput) and not on a patch (PatchInput).
    expect(
      proposedDiff(requestRecord({ diff: { edges: [] } })).refused
    ).toEqual(["edges"])
    expect(
      proposedDiff(requestRecord({ op: "create", diff: { edges: [] } })).refused
    ).toEqual([])
  })

  it("drops edge entries that name no rel or no id", () => {
    const diff = proposedDiff(
      requestRecord({
        op: "create",
        diff: { edges: [{ rel: "assignee" }, { to: { id: "p1" } }, "junk"] },
      })
    )
    expect(diff.edges).toEqual([])
  })

  it("flags a diff that is not an object, and empties a missing one", () => {
    expect(proposedDiff(requestRecord({ diff: "nope" })).unreadable).toBe(true)
    expect(proposedDiff(requestRecord({ diff: [1] })).unreadable).toBe(true)
    const absent = proposedDiff(requestRecord({}))
    expect(absent).toEqual({
      properties: {},
      labels: undefined,
      annotations: undefined,
      addFinalizers: [],
      removeFinalizers: [],
      ifVersion: undefined,
      edges: [],
      refused: [],
      malformed: [],
      unreadable: false,
    })
  })

  it("carries the finalizers and the diff's own ifVersion, which the accept applies", () => {
    const diff = proposedDiff(
      requestRecord({
        diff: {
          addFinalizers: ["owner/hold"],
          removeFinalizers: ["app/lock"],
          ifVersion: 11,
        },
      })
    )
    expect(diff.addFinalizers).toEqual(["owner/hold"])
    expect(diff.removeFinalizers).toEqual(["app/lock"])
    expect(diff.ifVersion).toBe(11)
    // A finalizer-only diff names something: the accept is not refused as empty.
    expect(diffNamesNothing(diff)).toBe(false)
    expect(diffCannotApply(diff)).toBe(false)
  })

  it("names nothing only when it truly names nothing", () => {
    expect(diffNamesNothing(proposedDiff(requestRecord({})))).toBe(true)
    expect(
      diffNamesNothing(
        proposedDiff(requestRecord({ diff: { labels: { a: "1" } } }))
      )
    ).toBe(false)
  })

  it("keeps a malformed wrapper visible instead of previewing an empty diff", () => {
    // Each of these decodes fine as JSON and FAILS the substrate's strict
    // decode, so a silent empty preview would be a lie.
    const diff = proposedDiff(
      requestRecord({
        diff: {
          properties: [],
          labels: "x",
          addFinalizers: [3],
          ifVersion: "soon",
        },
      })
    )
    expect(diff.properties).toEqual({})
    // Key order, so the list renders the same way twice.
    expect(diff.malformed).toEqual([
      { key: "addFinalizers", raw: [3] },
      { key: "ifVersion", raw: "soon" },
      { key: "labels", raw: "x" },
      { key: "properties", raw: [] },
    ])
    // The keys themselves are admitted here, so this is a shape problem, not a
    // refused key, and either way no accept can succeed.
    expect(diff.refused).toEqual([])
    expect(diffCannotApply(diff)).toBe(true)
    expect(diffNamesNothing(diff)).toBe(true)
  })

  it("keeps an edges value that is not a list", () => {
    const diff = proposedDiff(
      requestRecord({ op: "create", diff: { edges: { assignee: "p1" } } })
    )
    expect(diff.edges).toEqual([])
    expect(diff.malformed).toEqual([{ key: "edges", raw: { assignee: "p1" } }])
    expect(diffCannotApply(diff)).toBe(true)
  })

  it("keeps a malformed edge ENTRY, with its index and its raw value", () => {
    const diff = proposedDiff(
      requestRecord({
        op: "create",
        diff: {
          edges: [
            { rel: "assignee", to: { id: "p1" } },
            { rel: "blockedBy" },
            "junk",
          ],
        },
      })
    )
    expect(diff.edges).toHaveLength(1)
    expect(diff.malformed).toEqual([
      { key: "edges[1]", raw: { rel: "blockedBy" } },
      { key: "edges[2]", raw: "junk" },
    ])
    expect(diffCannotApply(diff)).toBe(true)
  })

  it("a key the strict decode does not carry still reads as refused", () => {
    const diff = proposedDiff(requestRecord({ diff: { saved: true } }))
    expect(diffCannotApply(diff)).toBe(true)
  })
})

// ── value equality, as the apply decides it ─────────────────────────────────

describe("sameApplied", () => {
  it("is ORDER-SENSITIVE for arrays, because the apply is", () => {
    // write.go's `take` compares with jsonEqual (Marshal byte equality), so a
    // reorder IS a change and the accept writes it.
    expect(sameApplied(["a", "b"], ["b", "a"])).toBe(false)
    expect(sameApplied(["a", "b"], ["a", "b"])).toBe(true)
    expect(sameApplied(["a", "a"], ["a"])).toBe(false)
  })

  it("ignores object key order, because Go's encoder sorts map keys", () => {
    expect(sameApplied({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(true)
    expect(sameApplied({ a: 1 }, { a: 2 })).toBe(false)
  })

  it("reaches arrays nested inside a json value", () => {
    expect(sameApplied({ tags: ["a", "b"] }, { tags: ["b", "a"] })).toBe(false)
    expect(sameApplied([{ x: [1, 2] }], [{ x: [1, 2] }])).toBe(true)
    expect(sameApplied([{ x: [1, 2] }], [{ x: [2, 1] }])).toBe(false)
  })

  it("distinguishes absent from empty", () => {
    expect(sameApplied(undefined, "")).toBe(false)
    expect(sameApplied(undefined, undefined)).toBe(true)
    expect(sameApplied(null, undefined)).toBe(false)
  })
})

// ── before / after ──────────────────────────────────────────────────────────

describe("deriveChangeRows", () => {
  it("classifies set, add, clear and unchanged against the live target", () => {
    const target = targetRecord(
      { summary: "Old summary", status: "open", note: "keep" },
      { meta: { summary: "owner" } }
    )
    const rows = deriveChangeRows(
      {
        summary: "New summary",
        status: "open",
        priority: "high",
        note: null,
      },
      target,
      taskKind
    )
    const byKey = Object.fromEntries(rows.map((r) => [r.key, r]))
    expect(byKey.summary).toMatchObject({
      effect: "set",
      before: "Old summary",
      after: "New summary",
      manager: "owner",
      declared: true,
      description: "what the task is",
    })
    expect(byKey.status.effect).toBe("unchanged")
    expect(byKey.priority).toMatchObject({
      effect: "add",
      before: undefined,
      declared: false,
    })
    expect(byKey.note.effect).toBe("clear")
  })

  it("reads a null against an absent key as changing nothing", () => {
    const rows = deriveChangeRows({ note: null }, targetRecord({}), taskKind)
    expect(rows[0].effect).toBe("unchanged")
  })

  it("reads a REORDER as a change, because the accept applies it", () => {
    const target = targetRecord({ tags: ["b", "a"] })
    const rows = deriveChangeRows({ tags: ["a", "b"] }, target, taskKind)
    expect(rows[0].effect).toBe("set")
    // The same values in the same order really are unchanged.
    expect(
      deriveChangeRows({ tags: ["b", "a"] }, target, taskKind)[0].effect
    ).toBe("unchanged")
  })

  it("reads a reorder nested inside a json value as a change too", () => {
    const target = targetRecord({ shape: { tags: ["a", "b"] } })
    const rows = deriveChangeRows(
      { shape: { tags: ["b", "a"] } },
      target,
      taskKind
    )
    expect(rows[0].effect).toBe("set")
  })

  it("ignores object key order, which the apply also ignores", () => {
    const target = targetRecord({ shape: { a: 1, b: 2 } })
    const rows = deriveChangeRows({ shape: { b: 2, a: 1 } }, target, taskKind)
    expect(rows[0].effect).toBe("unchanged")
  })

  it("adds every property of a create, which has no target to compare with", () => {
    const rows = deriveChangeRows(
      { summary: "Write it down", status: "open" },
      undefined,
      taskKind
    )
    expect(rows.map((r) => r.effect)).toEqual(["add", "add"])
    expect(rows.every((r) => r.before === undefined)).toBe(true)
  })

  it("orders what the accept removes or overwrites first, no-ops last", () => {
    const target = targetRecord({
      summary: "old",
      note: "gone",
      status: "open",
    })
    const rows = deriveChangeRows(
      { status: "open", fresh: 1, summary: "new", note: null },
      target,
      taskKind
    )
    expect(rows.map((r) => [r.key, r.effect])).toEqual([
      ["note", "clear"],
      ["summary", "set"],
      ["fresh", "add"],
      ["status", "unchanged"],
    ])
  })

  it("derives without a schema at all, every row undeclared", () => {
    const rows = deriveChangeRows({ summary: "x" }, targetRecord({}))
    expect(rows[0]).toMatchObject({ declared: false, description: undefined })
  })
})

describe("describeProposedEdge", () => {
  it("matches a proposed edge to its declaration", () => {
    expect(
      describeProposedEdge({ rel: "assignee", id: "p1" }, taskKind)
    ).toEqual({ declared: true, description: "who owns it" })
    expect(
      describeProposedEdge({ rel: "mystery", id: "p1" }, taskKind)
    ).toEqual({ declared: false, description: undefined })
    expect(describeProposedEdge({ rel: "assignee", id: "p1" })).toEqual({
      declared: false,
      description: undefined,
    })
  })
})

// ── the stale target ────────────────────────────────────────────────────────

/** The parsed diff a drift check needs; the stamp lives on the request. */
function diffOf(properties: Record<string, unknown>) {
  return proposedDiff(requestRecord(properties))
}

describe("effectiveCAS / targetDrift", () => {
  it("checks against the stamped targetVersion when the diff names none", () => {
    const r = requestRecord({ targetVersion: 3 })
    expect(effectiveCAS(r, diffOf({}))).toEqual({
      version: 3,
      via: "targetVersion",
    })
    expect(
      targetDrift(r, diffOf({}), targetRecord({}, { version: 5 }))
    ).toEqual({ version: 3, via: "targetVersion", current: 5 })
  })

  it("lets the diff's OWN ifVersion override the stamp, as the accept does", () => {
    // write.go's applyPatchRequest falls back to targetVersion only when the
    // decoded diff carries no ifVersion, so the override is the real CAS.
    const r = requestRecord({ targetVersion: 3 })
    const diff = diffOf({
      diff: { properties: { summary: "x" }, ifVersion: 7 },
    })
    expect(effectiveCAS(r, diff)).toEqual({
      version: 7,
      via: "diff.ifVersion",
    })
    // Agreeing with the stamp is NOT the question: 7 against a target at 3 is
    // drift, and 7 against a target at 7 is not.
    expect(targetDrift(r, diff, targetRecord({}, { version: 3 }))).toEqual({
      version: 7,
      via: "diff.ifVersion",
      current: 3,
    })
    expect(
      targetDrift(r, diff, targetRecord({}, { version: 7 }))
    ).toBeUndefined()
  })

  it("is silent when the versions agree, or when neither is known", () => {
    expect(
      targetDrift(
        requestRecord({ targetVersion: 3 }),
        diffOf({}),
        targetRecord({}, { version: 3 })
      )
    ).toBeUndefined()
    expect(
      targetDrift(requestRecord({}), diffOf({}), targetRecord({}))
    ).toBeUndefined()
    expect(
      targetDrift(requestRecord({ targetVersion: 3 }), diffOf({}))
    ).toBeUndefined()
  })
})

describe("appliesNothing", () => {
  const target = targetRecord({ summary: "same" })

  it("is true when every named property already matches and nothing else rides", () => {
    const diff = diffOf({ diff: { properties: { summary: "same" } } })
    const rows = deriveChangeRows(diff.properties, target, taskKind)
    expect(appliesNothing(diff, rows)).toBe(true)
  })

  it("is FALSE for a finalizer-only diff, which applies something", () => {
    const diff = diffOf({ diff: { addFinalizers: ["owner/hold"] } })
    const rows = deriveChangeRows(diff.properties, target, taskKind)
    expect(rows).toEqual([])
    expect(appliesNothing(diff, rows)).toBe(false)
  })

  it("is false when a label or an annotation rides along unchanged properties", () => {
    const withLabel = diffOf({
      diff: { properties: { summary: "same" }, labels: { tier: "a" } },
    })
    expect(
      appliesNothing(
        withLabel,
        deriveChangeRows(withLabel.properties, target, taskKind)
      )
    ).toBe(false)
  })

  it("is false as soon as one property really changes", () => {
    const diff = diffOf({ diff: { properties: { summary: "other" } } })
    expect(
      appliesNothing(diff, deriveChangeRows(diff.properties, target, taskKind))
    ).toBe(false)
  })
})

// ── the decision patch ─────────────────────────────────────────────────────

describe("decisionPatch", () => {
  it("carries the request's own version as the CAS precondition", () => {
    expect(decisionPatch("accepted", 7)).toEqual({
      properties: { decision: "accepted" },
      ifVersion: 7,
    })
    expect(decisionPatch("rejected", 2)).toEqual({
      properties: { decision: "rejected" },
      ifVersion: 2,
    })
  })

  it("rides the note as the owner/note annotation in the same write", () => {
    expect(decisionPatch("accepted", 7, "  checked the diff  ")).toEqual({
      properties: { decision: "accepted" },
      ifVersion: 7,
      annotations: { "owner/note": "checked the diff" },
    })
  })

  it("omits an empty note entirely", () => {
    expect(decisionPatch("rejected", 1, "   ")).toEqual({
      properties: { decision: "rejected" },
      ifVersion: 1,
    })
  })
})

// ── what the server left behind ─────────────────────────────────────────────

describe("applyConflict", () => {
  it("reads the substrate/conflict annotation the refused apply left", () => {
    const r = requestRecord(
      {},
      {
        annotations: {
          "substrate/conflict": {
            reason: "conflict: applyDiff on cr-1: version 3 is stale",
            at: "2026-08-14T10:00:00Z",
          },
        },
      }
    )
    expect(applyConflict(r)).toEqual({
      reason: "conflict: applyDiff on cr-1: version 3 is stale",
      at: "2026-08-14T10:00:00Z",
    })
  })

  it("accepts a bare string, and any namespace's conflict key", () => {
    expect(
      applyConflict(
        requestRecord({}, { annotations: { "app.foo/conflict": "it moved" } })
      )
    ).toEqual({ reason: "it moved" })
  })

  it("keeps an unreadable annotation verbatim rather than dropping it", () => {
    expect(
      applyConflict(
        requestRecord(
          {},
          { annotations: { "substrate/conflict": { code: 9 } } }
        )
      )
    ).toEqual({ reason: '{"code":9}' })
  })

  it("is silent when nothing conflicted", () => {
    expect(applyConflict(requestRecord({}))).toBeUndefined()
    expect(
      applyConflict(requestRecord({}, { annotations: { "owner/note": "hi" } }))
    ).toBeUndefined()
  })
})

// ── who ─────────────────────────────────────────────────────────────────────

describe("decisionOf / decisionNote", () => {
  // Declared here rather than borrowed from the merge request: nothing holds
  // the two kinds' `decision` declarations to the same states.
  it("defaults an unreadable decision to proposed", () => {
    expect(decisionOf(requestRecord({ decision: "accepted" }))).toBe("accepted")
    expect(decisionOf(requestRecord({ decision: "rejected" }))).toBe("rejected")
    expect(decisionOf(requestRecord({}))).toBe("proposed")
    expect(decisionOf(requestRecord({ decision: 3 }))).toBe("proposed")
  })

  it("reads any actor's /note key back, and ignores a non-string", () => {
    expect(
      decisionNote(
        requestRecord({}, { annotations: { "owner/note": "checked" } })
      )
    ).toBe("checked")
    expect(
      decisionNote(
        requestRecord({}, { annotations: { "app.foo/note": "theirs" } })
      )
    ).toBe("theirs")
    expect(
      decisionNote(requestRecord({}, { annotations: { "owner/note": 7 } }))
    ).toBeUndefined()
    expect(decisionNote(requestRecord({}))).toBeUndefined()
  })
})

describe("proposerOf / deciderOf", () => {
  it("reads the proposer off a property only a proposal writes", () => {
    expect(
      proposerOf(requestRecord({}, { meta: { diff: "learner.substrate" } }))
    ).toBe("learner.substrate")
    // No `diff` manager row: the next proposal property answers.
    expect(
      proposerOf(requestRecord({}, { meta: { rationale: "console" } }))
    ).toBe("console")
    expect(proposerOf(requestRecord({}))).toBeUndefined()
  })

  it("reads the decider off the stamped decidedAt", () => {
    expect(
      deciderOf(requestRecord({}, { meta: { decidedAt: "console" } }))
    ).toBe("console")
    expect(deciderOf(requestRecord({}))).toBeUndefined()
  })
})
