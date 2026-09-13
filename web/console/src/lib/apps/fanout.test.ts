/** A read over several kinds: the query rewritten onto each kind's temporal
 * point, the pages merged and sorted by that point, `incomplete` when a
 * cursor remained, and a single read passed through with its cursor. */

import { describe, expect, it } from "vitest"

import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { mergePages, readFor, runImplements } from "./fanout"
import { boundPoint, pointOrder, rewriteForKind } from "./temporal"

function kind(identity: string, traits: string[] = []): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { traits, properties: {} },
  }
}

const task = kind("ada.example.com/tasks/task", ["temporal(point: dueAt)"])
const event = kind("ada.example.com/calendar/calendarevent", [
  "temporal(range)",
])
const person = kind("ada.example.com/people/person")

function record(
  k: KindInfo,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: k.identity,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const page = (records: SubstrateRecord[], cursor?: string): Page => ({
  records,
  cursor,
  head: 7,
  generation: "g",
})

describe("temporal", () => {
  it("finds the bound point: a remap, a range's start, or at", () => {
    expect(boundPoint(task)).toBe("dueAt")
    expect(boundPoint(event)).toBe("at")
    expect(boundPoint(person)).toBe("at")
  })

  it("moves at onto the point in the filter and the order, and nothing else", () => {
    const q = {
      filter: {
        properties: {
          at: { gte: "2026-09-01T00:00:00Z" },
          status: { eq: "open" },
        },
      },
      orderBy: "at:desc",
    }
    expect(rewriteForKind(q, task)).toEqual({
      filter: {
        properties: {
          status: { eq: "open" },
          dueAt: { gte: "2026-09-01T00:00:00Z" },
        },
      },
      orderBy: "dueAt:desc",
    })
    expect(rewriteForKind(q, event)).toBe(q)
    expect(rewriteForKind({ orderBy: "name:asc" }, task)).toEqual({
      filter: undefined,
      orderBy: "name:asc",
    })
    expect(pointOrder("at:desc")).toBe("desc")
    expect(pointOrder("dueAt:desc")).toBe("asc")
    expect(pointOrder(undefined)).toBe("asc")
  })
})

describe("readFor", () => {
  it("addresses the kind's collection with the rewritten query", () => {
    const read = readFor(
      {
        implements: "t",
        first: 3,
        orderBy: "at:asc",
        filter: { properties: { at: { lt: "x" } } },
      },
      task
    )
    expect(read.params).toEqual({
      authority: "ada.example.com",
      package: "tasks",
      name: "task",
      first: 3,
      after: undefined,
      filter: { properties: { dueAt: { lt: "x" } } },
      orderBy: "dueAt:asc",
    })
  })
})

describe("mergePages", () => {
  const t1 = record(task, "t1", { dueAt: "2026-09-03T10:00:00Z" })
  const t2 = record(task, "t2", { dueAt: "2026-09-01T10:00:00Z" })
  const e1 = record(event, "e1", { at: "2026-09-02T10:00:00Z" })
  const e2 = record(event, "e2", {})

  it("passes a single read through, cursor and all", () => {
    const reads = [readFor({ kind: task.identity }, task)]
    expect(mergePages(reads, [page([t1, t2], "c1")])).toEqual({
      records: [t1, t2],
      cursor: "c1",
      head: 7,
    })
  })

  it("merges several by each record's own point, undated last, and says incomplete", () => {
    const q = { implements: "t", orderBy: "at:asc" }
    const reads = [readFor(q, task), readFor(q, event)]
    const merged = mergePages(
      reads,
      [page([t1, t2], "more"), page([e1, e2])],
      q.orderBy
    )
    expect(merged.records.map((r) => r.id)).toEqual(["t2", "e1", "t1", "e2"])
    expect(merged.incomplete).toBe(true)
    expect(merged.cursor).toBeUndefined()
    expect(merged.head).toBe(7)
    const desc = mergePages(reads, [page([t1, t2]), page([e1])], "at:desc")
    expect(desc.records.map((r) => r.id)).toEqual(["t1", "e1", "t2"])
    expect(desc.incomplete).toBeUndefined()
  })
})

describe("runImplements", () => {
  it("lists each kind in parallel with its own rewritten query", async () => {
    const asked: string[] = []
    const result = await runImplements(
      {
        implements: "t",
        filter: { properties: { at: { gte: "2026-09-01T00:00:00Z" } } },
        orderBy: "at:asc",
        first: 10,
      },
      [task, event],
      async (params) => {
        asked.push(
          `${params.name} ${params.orderBy} ${JSON.stringify(params.filter)}`
        )
        return params.name === "task"
          ? page([record(task, "t", { dueAt: "2026-09-05T00:00:00Z" })])
          : page([record(event, "e", { at: "2026-09-04T00:00:00Z" })])
      }
    )
    expect(asked).toEqual([
      'task dueAt:asc {"properties":{"dueAt":{"gte":"2026-09-01T00:00:00Z"}}}',
      'calendarevent at:asc {"properties":{"at":{"gte":"2026-09-01T00:00:00Z"}}}',
    ])
    expect(result.records.map((r) => r.id)).toEqual(["e", "t"])
  })
})
