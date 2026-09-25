/** The record page's pure decisions: which fan-in groups "Connected to"
 * lists and in what order, what a record points to, what a change row says,
 * and who the header says added and last changed it. */

import { describe, expect, it } from "vitest"

import {
  changeSentence,
  CHANGE_REQUEST_KIND,
  MERGE_REQUEST_KIND,
  connectedGroups,
  doneState,
  everydayGroups,
  headerFacts,
  outgoingOf,
  sortConnected,
} from "./record-model"
import type { ReferencingGroup, ReferencingRow } from "@/lib/api/records"
import type { ChangeRow, KindInfo, SubstrateRecord } from "@/lib/api/types"

const PERSON = "ada.example.com/people/person"
const TASK = "ada.example.com/tasks/task"
const CONTACT = "providers.substrate.reamde.dev/google/contact"

const kind = (identity: string, properties: Record<string, unknown>) => {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { properties },
  } satisfies KindInfo
}

const task = kind(TASK, {
  status: { type: "state", states: ["open", "done"], initial: "open" },
  assignee: { type: "reference", kind: PERSON },
  watchers: { type: "reference", kind: PERSON, repeated: true },
})

const rec = (
  id: string,
  properties: Record<string, unknown>,
  k = TASK
): SubstrateRecord => ({
  id,
  kind: k,
  properties,
  labels: {},
  version: 1,
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
})

const grace: SubstrateRecord = {
  ...rec("grace", { name: "Grace" }, PERSON),
  linkedFrom: [
    {
      ref: `${CONTACT}/c1`,
      kind: CONTACT,
      property: "person",
      mapping: "ada.example.com/people/googlecontactperson",
    },
  ],
}

const row = (r: SubstrateRecord, property: string): ReferencingRow => ({
  record: r,
  property,
})

describe("connectedGroups", () => {
  it("drops the mapping slots a record's sources point through", () => {
    const groups: ReferencingGroup[] = [
      { kind: CONTACT, property: "person", rows: [] },
      { kind: TASK, property: "assignee", rows: [] },
    ]
    expect(connectedGroups(groups, grace).map((g) => g.kind)).toEqual([TASK])
  })
})

describe("everydayGroups", () => {
  it("keeps data and the review requests, one group each, and drops machinery", () => {
    const req = (id: string, k: string) => rec(id, {}, k)
    const groups: ReferencingGroup[] = [
      { kind: TASK, property: "assignee", rows: [] },
      {
        kind: MERGE_REQUEST_KIND,
        property: "winner",
        rows: [row(req("m1", MERGE_REQUEST_KIND), "winner")],
      },
      {
        kind: MERGE_REQUEST_KIND,
        property: "loser",
        rows: [
          row(req("m1", MERGE_REQUEST_KIND), "loser"),
          row(req("m2", MERGE_REQUEST_KIND), "loser"),
        ],
      },
      { kind: CHANGE_REQUEST_KIND, property: "target", rows: [] },
      { kind: "substrate.reamde.dev/core/triggerrun", property: "x", rows: [] },
      { kind: "ada.example.com/tasks/cursor", property: "task", rows: [] },
    ]
    const kinds = new Map([
      [
        "ada.example.com/tasks/cursor",
        {
          ...kind("ada.example.com/tasks/cursor", {}),
          definition: { purpose: "internal" },
        },
      ],
    ])
    const out = everydayGroups(groups, kinds)
    expect(out.map((g) => g.kind)).toEqual([
      TASK,
      MERGE_REQUEST_KIND,
      CHANGE_REQUEST_KIND,
    ])
    expect(out[1].rows.map((r) => r.record.id)).toEqual(["m1", "m2"])
  })
})

describe("doneState and sortConnected", () => {
  it("finds a done-like state and orders what is left first, soonest due", () => {
    expect(doneState(task)).toEqual({
      property: "status",
      done: "done",
      initial: "open",
    })
    expect(doneState(kind(PERSON, {}))).toBeUndefined()
    const rows = [
      row(
        rec("a", { status: "done", title: "A", dueAt: "2026-01-01" }),
        "assignee"
      ),
      row(
        rec("b", { status: "open", title: "B", dueAt: "2026-03-01" }),
        "assignee"
      ),
      row(
        rec("c", { status: "open", title: "C", dueAt: "2026-02-01" }),
        "assignee"
      ),
      row(rec("d", { status: "open", title: "D" }), "assignee"),
    ]
    expect(
      sortConnected(rows, doneState(task), "dueAt").map((r) => r.record.id)
    ).toEqual(["c", "b", "d", "a"])
  })
})

describe("outgoingOf", () => {
  it("lists every record a declared reference holds, lists included", () => {
    const t = rec("t1", {
      assignee: { ref: `${PERSON}/grace` },
      watchers: [{ ref: `${PERSON}/ada` }, { ref: `${PERSON}/alan` }],
    })
    expect(outgoingOf(t, task).map((o) => `${o.label}:${o.id}`)).toEqual([
      "Assignee:grace",
      "Watchers:ada",
      "Watchers:alan",
    ])
    expect(outgoingOf(t, undefined)).toEqual([])
  })
})

const change = (over: Partial<ChangeRow>): ChangeRow => ({
  seq: 1,
  ts: "2026-09-01T00:00:00Z",
  actor: "console",
  op: "put",
  recordId: "t1",
  kind: TASK,
  ...over,
})

describe("changeSentence", () => {
  const t = rec("t1", {})
  it("says what one change did", () => {
    expect(changeSentence(change({ payload: { created: true } }), t)).toBe(
      "added this task"
    )
    expect(
      changeSentence(
        change({ op: "patch", payload: { properties: ["priority"] } }),
        t
      )
    ).toBe("changed")
    expect(
      changeSentence(
        change({
          op: "patch",
          payload: { properties: ["completedAt"], states: { status: "done" } },
        }),
        t
      )
    ).toBe("moved")
    expect(
      changeSentence(
        change({ op: "merge", payload: { winner: "t1", loser: "t2" } }),
        t
      )
    ).toBe("combined another task into this one")
  })
})

describe("headerFacts", () => {
  it("names who added it only when the creating row is known", () => {
    const t = { ...rec("t1", {}), updatedAt: "2026-09-02T00:00:00Z" }
    const rows = [
      change({
        seq: 2,
        op: "patch",
        actor: "agent:ada.example.com:llm:helper",
      }),
      change({ seq: 1, payload: { created: true } }),
    ]
    expect(headerFacts(t, rows)).toEqual({
      addedBy: "you",
      changedBy: "Helper",
      changed: true,
    })
    expect(headerFacts(t, rows.slice(0, 1)).addedBy).toBeUndefined()
    expect(headerFacts(rec("t1", {}), rows.slice(1)).changed).toBe(false)
  })
})
