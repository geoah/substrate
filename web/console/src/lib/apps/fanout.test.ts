/** A read over several kinds: the query rewritten onto each kind's temporal
 * point where it names `at`, the pages merged under the order the query
 * asked for (the engine's rules: nulls last, the `(kind, id)` tiebreak,
 * `createdAt` newest-first by default), a key the kinds compare differently
 * refused before a request is made, `incomplete` when a cursor remained, and
 * a single read passed through with its cursor. */

import { describe, expect, it } from "vitest"

import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { ERROR, RpcError } from "./bridge/protocol"
import { mergePages, readFor, readsFor, runImplements } from "./fanout"
import {
  boundPoint,
  parseOrderBy,
  renderOrderBy,
  rewriteForKind,
} from "./temporal"

function kind(
  identity: string,
  traits: string[] = [],
  properties: Record<string, Record<string, unknown>> = {}
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { traits, properties },
  }
}

const task = kind("ada.example.com/tasks/task", ["temporal(point: dueAt)"], {
  priority: { type: "int" },
  name: { type: "string" },
  tags: { type: "string", repeated: true },
  owner: { type: "reference", kind: "ada.example.com/people/person" },
})
const event = kind("ada.example.com/calendar/calendarevent", [
  "temporal(range)",
])
const person = kind("ada.example.com/people/person", [], {
  priority: { type: "string" },
  name: { type: "string" },
  secret: { type: "secret" },
})
const bug = kind("ada.example.com/tasks/bug", [], {
  priority: { type: "int" },
  owner: { type: "reference", kind: "ada.example.com/people/person" },
})

function record(
  k: KindInfo,
  id: string,
  properties: Record<string, unknown>,
  stamps: Partial<Pick<SubstrateRecord, "createdAt" | "updatedAt">> = {}
): SubstrateRecord {
  return {
    id,
    kind: k.identity,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
    ...stamps,
  }
}

const page = (records: SubstrateRecord[], cursor?: string): Page => ({
  records,
  cursor,
  head: 7,
  generation: "g",
})

const ids = (result: { records: SubstrateRecord[] }) =>
  result.records.map((r) => r.id)

describe("temporal", () => {
  it("finds the bound point: a remap, a range's start, or at", () => {
    expect(boundPoint(task)).toBe("dueAt")
    expect(boundPoint(event)).toBe("at")
    expect(boundPoint(person)).toBe("at")
  })

  it("parses the wire's order spelling and refuses a direction the server would", () => {
    expect(parseOrderBy("at:desc, name")).toEqual([
      { property: "at", desc: true },
      { property: "name", desc: false },
    ])
    expect(parseOrderBy(undefined)).toEqual([])
    expect(renderOrderBy(parseOrderBy("dueAt:ASC,name:desc"))).toBe(
      "dueAt:asc,name:desc"
    )
    expect(() => parseOrderBy("name:sideways")).toThrow(
      "orderBy: direction must be asc or desc"
    )
    expect(() => parseOrderBy(":asc")).toThrow(RpcError)
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
    // Every key of a several-key order is kept, only `at` moves.
    expect(rewriteForKind({ orderBy: "at:desc,name" }, task).orderBy).toBe(
      "dueAt:desc,name:asc"
    )
    // An order that never names `at` keeps its spelling, bare or not.
    expect(rewriteForKind({ orderBy: "name:asc" }, task)).toEqual({
      filter: undefined,
      orderBy: "name:asc",
    })
    expect(rewriteForKind({ orderBy: "name" }, task).orderBy).toBe("name")
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

describe("readsFor", () => {
  const invalidParams = (fn: () => unknown, message: string) => {
    try {
      fn()
    } catch (err) {
      expect(err).toBeInstanceOf(RpcError)
      expect((err as RpcError).code).toBe(ERROR.invalidParams)
      expect((err as RpcError).message).toContain(message)
      return
    }
    throw new Error("expected a refusal")
  }

  it("refuses a key the kinds compare differently, a repeated one and a sensitive one", () => {
    invalidParams(
      () =>
        readsFor({ kinds: ["x", "y"], orderBy: "priority" }, [task, person]),
      "priority is a number on ada.example.com/tasks/task and text on ada.example.com/people/person"
    )
    invalidParams(
      () => readsFor({ kinds: ["x", "y"], orderBy: "tags:asc" }, [task, bug]),
      "tags is repeated on ada.example.com/tasks/task"
    )
    invalidParams(
      () =>
        readsFor({ kinds: ["x", "y"], orderBy: "secret:asc" }, [person, task]),
      "secret is sensitive on ada.example.com/people/person"
    )
    invalidParams(
      () => readsFor({ kinds: ["x", "y"], orderBy: "name:up" }, [task, person]),
      "direction must be asc or desc"
    )
  })

  it("admits a key the kinds agree on, and never judges a single read", () => {
    expect(
      readsFor({ kinds: ["x", "y"], orderBy: "priority:desc" }, [task, bug])
    ).toHaveLength(2)
    expect(
      readsFor({ kinds: ["x", "y"], orderBy: "at:asc,updatedAt:desc" }, [
        task,
        event,
      ])
    ).toHaveLength(2)
    // A property one kind does not declare orders as text, null there.
    expect(
      readsFor({ kinds: ["x", "y"], orderBy: "name" }, [task, bug])
    ).toHaveLength(2)
    // One kind is one request: the server judges its own order.
    expect(readsFor({ kind: "x", orderBy: "tags" }, [task])).toHaveLength(1)
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

  it("merges a temporal query by each record's own point, undated last either way, and says incomplete", () => {
    const q = { implements: "t", orderBy: "at:asc" }
    const reads = [readFor(q, task), readFor(q, event)]
    const merged = mergePages(
      reads,
      [page([t1, t2], "more"), page([e1, e2])],
      q.orderBy
    )
    expect(ids(merged)).toEqual(["t2", "e1", "t1", "e2"])
    expect(merged.incomplete).toBe(true)
    expect(merged.cursor).toBeUndefined()
    expect(merged.head).toBe(7)
    const desc = mergePages(reads, [page([t1, t2]), page([e1, e2])], "at:desc")
    expect(ids(desc)).toEqual(["t1", "e1", "t2", "e2"])
    expect(desc.incomplete).toBeUndefined()
  })

  it("orders a general query by the property it names, across kinds", () => {
    const q = { kinds: [task.identity, person.identity], orderBy: "name:asc" }
    const reads = readsFor(q, [task, person])
    const zed = record(task, "t9", { name: "Zed" })
    const bob = record(task, "t8", { name: "bob" })
    const alice = record(person, "p1", { name: "alice" })
    const nameless = record(person, "p2", {})
    const merged = mergePages(
      reads,
      [page([bob, zed]), page([alice, nameless])],
      q.orderBy
    )
    expect(ids(merged)).toEqual(["p1", "t8", "t9", "p2"])
    expect(
      ids(
        mergePages(
          reads,
          [page([bob, zed]), page([alice, nameless])],
          "name:desc"
        )
      )
    ).toEqual(["t9", "t8", "p1", "p2"])
  })

  it("orders by an envelope field, and by createdAt newest-first when nothing is named", () => {
    const reads = readsFor({ kinds: [task.identity, person.identity] }, [
      task,
      person,
    ])
    const older = record(task, "t1", {}, { createdAt: "2026-09-01T00:00:00Z" })
    const newer = record(
      person,
      "p1",
      {},
      { createdAt: "2026-09-02T00:00:00Z" }
    )
    const same = record(person, "p0", {}, { createdAt: "2026-09-01T00:00:00Z" })
    // Equal instants fall to the (kind, id) tiebreak in the leading key's
    // direction: descending, so `tasks/task` before `people/person`, as the
    // server pages it.
    expect(
      ids(mergePages(reads, [page([older]), page([newer, same])]))
    ).toEqual(["p1", "t1", "p0"])
    const touched = record(
      task,
      "t1",
      {},
      { updatedAt: "2026-09-05T00:00:00Z" }
    )
    const untouched = record(
      person,
      "p1",
      {},
      { updatedAt: "2026-09-03T00:00:00Z" }
    )
    expect(
      ids(
        mergePages(
          reads,
          [page([touched]), page([untouched])],
          "updatedAt:desc"
        )
      )
    ).toEqual(["t1", "p1"])
    expect(
      ids(mergePages(reads, [page([touched]), page([untouched])], "updatedAt"))
    ).toEqual(["p1", "t1"])
  })

  it("compares a number as a number and a reference by its path", () => {
    const reads = readsFor({ kinds: [task.identity, bug.identity] }, [
      task,
      bug,
    ])
    const ten = record(task, "t10", { priority: 10 })
    const nine = record(bug, "b9", { priority: 9 })
    const two = record(bug, "b2", { priority: 2 })
    expect(
      ids(mergePages(reads, [page([ten]), page([nine, two])], "priority:asc"))
    ).toEqual(["b2", "b9", "t10"])
    const ada = record(task, "t1", {
      owner: { ref: "ada.example.com/people/person/ada" },
    })
    const bo = record(bug, "b1", {
      owner: { ref: "ada.example.com/people/person/bo" },
    })
    expect(
      ids(mergePages(reads, [page([ada]), page([bo])], "owner:desc"))
    ).toEqual(["b1", "t1"])
  })

  it("orders by several keys in turn", () => {
    const reads = readsFor({ kinds: [task.identity, bug.identity] }, [
      task,
      bug,
    ])
    const a = record(task, "t1", { priority: 1, name: "b" })
    const b = record(task, "t2", { priority: 1, name: "a" })
    const c = record(bug, "b1", { priority: 2 })
    expect(
      ids(
        mergePages(reads, [page([a, b]), page([c])], "priority:desc,name:asc")
      )
    ).toEqual(["b1", "t2", "t1"])
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
    expect(ids(result)).toEqual(["e", "t"])
  })

  it("refuses an order it cannot merge before asking anyone", async () => {
    let asked = 0
    await expect(
      runImplements(
        { kinds: [task.identity, person.identity], orderBy: "priority" },
        [task, person],
        async () => {
          asked++
          return page([])
        }
      )
    ).rejects.toBeInstanceOf(RpcError)
    expect(asked).toBe(0)
  })
})
