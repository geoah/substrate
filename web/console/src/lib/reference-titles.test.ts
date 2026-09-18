/** Resolving what a pointer is CALLED: reading a page's `included` sidecar,
 * reading a batch of referents, finding the paths one record points at, and
 * turning those paths into the one list read that answers them. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  referencePathsOf,
  titleReadScope,
  titlesFromIncluded,
  titlesFromRecords,
} from "./reference-titles"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  formerIds?: string[]
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-18T00:00:00Z",
    updatedAt: "2026-09-18T00:00:00Z",
    formerIds,
  }
}

const PERSON = "ada.example.com/people/person"
const TASK = "ada.example.com/tasks/task"

describe("titles from a list page's included sidecar", () => {
  it("keys them by the path the pointing rows wrote", () => {
    const titles = titlesFromIncluded({
      [`${PERSON}/p1`]: record(PERSON, "p1", { title: "Ada Lovelace" }),
    })
    expect(titles.get(`${PERSON}/p1`)).toBe("Ada Lovelace")
  })

  // Absence IS the id fallback. Mapping a titleless referent to its id would
  // make it indistinguishable from one the page never expanded.
  it("leaves out a referent with no title of its own", () => {
    const titles = titlesFromIncluded({
      [`${PERSON}/p1`]: record(PERSON, "p1", {}),
    })
    expect(titles.has(`${PERSON}/p1`)).toBe(false)
  })

  it("is empty when the page carried no expansion at all", () => {
    expect(titlesFromIncluded(undefined).size).toBe(0)
  })
})

describe("titles from a batch of referents", () => {
  it("answers the record's own path", () => {
    const titles = titlesFromRecords([
      record(PERSON, "p1", { title: "Ada Lovelace" }),
    ])
    expect(titles.get(`${PERSON}/p1`)).toBe("Ada Lovelace")
  })

  // A pointer written before a merge still names the loser's id, and the
  // batch answers under the winner's; the former ids are how the two meet,
  // exactly as the server's own expansion resolves them.
  it("answers every former id the winner absorbed", () => {
    const titles = titlesFromRecords([
      record(PERSON, "p1", { title: "Ada Lovelace" }, ["p0"]),
    ])
    expect(titles.get(`${PERSON}/p0`)).toBe("Ada Lovelace")
  })
})

describe("the paths one record points at", () => {
  it("finds a single reference, a repeated one, and a keyed map of them", () => {
    const paths = referencePathsOf(
      record(TASK, "t1", {
        assignee: { ref: `${PERSON}/p1` },
        watchers: [{ ref: `${PERSON}/p2` }, { ref: `${PERSON}/p3` }],
        roles: { reviewer: { ref: `${PERSON}/p4` } },
      })
    )
    expect(paths.sort()).toEqual([
      `${PERSON}/p1`,
      `${PERSON}/p2`,
      `${PERSON}/p3`,
      `${PERSON}/p4`,
    ])
  })

  it("reaches a pointer nested inside a declared object", () => {
    expect(
      referencePathsOf(
        record(TASK, "t1", { origin: { via: { ref: `${PERSON}/p1` } } })
      )
    ).toEqual([`${PERSON}/p1`])
  })

  it("reads the authored string shorthand, the same value the pill renders", () => {
    expect(
      referencePathsOf(record(TASK, "t1", { assignee: `${PERSON}/p1` }))
    ).toEqual([`${PERSON}/p1`])
  })

  it("says nothing about a string that names no record", () => {
    expect(
      referencePathsOf(record(TASK, "t1", { title: "Buy milk", done: false }))
    ).toEqual([])
  })

  it("names each referent once, however many times it is pointed at", () => {
    expect(
      referencePathsOf(
        record(TASK, "t1", {
          assignee: { ref: `${PERSON}/p1` },
          reviewer: { ref: `${PERSON}/p1` },
        })
      )
    ).toEqual([`${PERSON}/p1`])
  })
})

describe("the scope of the one batched read", () => {
  const known = new Set([PERSON, TASK])

  it("groups the paths into the kinds they name and the ids within them", () => {
    expect(
      titleReadScope([`${PERSON}/p1`, `${TASK}/t9`, `${PERSON}/p2`], known)
    ).toEqual({ kinds: [PERSON, TASK], ids: ["p1", "p2", "t9"] })
  })

  // A kind the repository never declared is `404` for the WHOLE read, and
  // such a reference renders as inert text anyway: it never goes on the wire.
  it("drops a path whose kind nobody installed", () => {
    expect(
      titleReadScope([`${PERSON}/p1`, "ada.example.com/crm/lead/7"], known)
    ).toEqual({ kinds: [PERSON], ids: ["p1"] })
  })

  it("asks for nothing when nothing is resolvable", () => {
    expect(titleReadScope(["not-a-path"], known)).toEqual({
      kinds: [],
      ids: [],
    })
  })
})
