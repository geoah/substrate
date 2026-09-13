/** The grant expanded against the registry: kinds pass through, traits
 * become their implementors, and what resolves to nothing is named with the
 * path it sits at. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { expandGrant, implementsTrait, traitNameOf } from "./grant"

const TEMPORAL = "substrate.reamde.dev/core/temporal"

function kind(
  identity: string,
  traits: string[] = [],
  over: Partial<KindInfo> = {}
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
    definition: { traits, properties: {} },
    ...over,
  }
}

const task = kind("ada.example.com/tasks/task", ["temporal(point: dueAt)"])
const event = kind("ada.example.com/calendar/calendarevent", [
  "temporal(range)",
])
const person = kind("ada.example.com/people/person")
const kinds = [task, event, person]

describe("implementsTrait", () => {
  it("strips the binding's arguments", () => {
    expect(traitNameOf("temporal(point: dueAt)")).toBe("temporal")
    expect(traitNameOf("recurring")).toBe("recurring")
  })

  it("matches a bare binding against a core trait and the kind's own package", () => {
    expect(implementsTrait(task, TEMPORAL)).toBe(true)
    expect(implementsTrait(task, "temporal")).toBe(true)
    expect(implementsTrait(person, TEMPORAL)).toBe(false)
    expect(implementsTrait(task, "ada.example.com/tasks/temporal")).toBe(true)
    // A bare binding never answers for a foreign package's trait.
    expect(implementsTrait(task, "other.example.com/x/temporal")).toBe(false)
    const explicit = kind("ada.example.com/tasks/note", [
      "other.example.com/x/temporal(point)",
    ])
    expect(implementsTrait(explicit, "other.example.com/x/temporal")).toBe(true)
    expect(implementsTrait(explicit, TEMPORAL)).toBe(false)
  })
})

describe("expandGrant", () => {
  it("unions the kinds and the traits' implementors, in order, once", () => {
    const grant = expandGrant(
      {
        permissions: {
          reads: { kinds: [task.identity], traits: [TEMPORAL] },
          writes: [task.identity],
          call: ["ada.example.com/tasks/summarize"],
          agents: [],
        },
      },
      kinds
    )
    expect(grant.reads).toEqual([task.identity, event.identity])
    expect(grant.readKinds.map((k) => k.name)).toEqual([
      "task",
      "calendarevent",
    ])
    expect(grant.writes).toEqual([task.identity])
    expect(grant.call).toEqual(["ada.example.com/tasks/summarize"])
    expect(grant.unresolved).toEqual([])
  })

  it("names every reference that resolves to nothing, at its path", () => {
    const grant = expandGrant(
      {
        permissions: {
          reads: {
            kinds: [
              task.identity,
              "providers.substrate.reamde.dev/github/pullrequest",
            ],
            traits: ["substrate.reamde.dev/core/nothing"],
          },
          writes: ["ada.example.com/tasks/gone"],
          call: [],
          agents: [],
        },
      },
      kinds
    )
    expect(grant.reads).toEqual([task.identity])
    expect(grant.unresolved).toEqual([
      {
        path: "permissions.reads.kinds[1]",
        identity: "providers.substrate.reamde.dev/github/pullrequest",
      },
      {
        path: "permissions.reads.traits[0]",
        identity: "substrate.reamde.dev/core/nothing",
      },
      { path: "permissions.writes[0]", identity: "ada.example.com/tasks/gone" },
    ])
  })
})
