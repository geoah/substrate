import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  columnProperties,
  declaredEdges,
  declaredProperties,
  describeKey,
  edgeTypeLabel,
  filterableProperties,
  graphqlTypeName,
  propertyTypeLabel,
  kindByCollection,
  resolveEdgeTarget,
  splitKind,
  stateProperties,
  temporalProperties,
} from "./definition"

/** A faithful slice of the live person kind (record 56 shapes). */
const person: KindInfo = {
  identity: "people.substrate.reamde.dev/person",
  name: "person",
  authority: "people.substrate.reamde.dev",
  version: "",
  plural: "people",
  source: "builtin",
  definition: {
    authority: "people.substrate.reamde.dev",
    names: { singular: "person", plural: "people" },
    displayTemplate: "{displayName|name}",
    properties: {
      name: { type: "string", description: "the full name, one string" },
      displayName: { type: "string" },
      emails: { type: "email", repeated: true, description: "every address" },
      phones: { type: "string", repeated: true },
      raw: { type: "json" },
      apiKey: { type: "secret" },
      bio: { type: "text" },
      prominence: {
        type: "state",
        states: ["utility", "known"],
        initial: "utility",
        description: "utility until promoted",
      },
    },
    edges: {
      memberOf: {
        to: "organization",
        many: true,
        description: "the employer or workspace",
      },
    },
  },
}

const event: KindInfo = {
  identity: "calendar.substrate.reamde.dev/calendarevent",
  name: "calendarevent",
  authority: "calendar.substrate.reamde.dev",
  version: "",
  plural: "calendarevents",
  source: "builtin",
  definition: { traits: ["temporal(range)"], properties: {} },
}

describe("declaredProperties", () => {
  it("reads name/kind/description/repeated/states in schema casing", () => {
    const props = declaredProperties(person)
    expect(props.map((p) => p.name)).toEqual([
      "apiKey",
      "bio",
      "displayName",
      "emails",
      "name",
      "phones",
      "prominence",
      "raw",
    ])
    const prominence = props.find((p) => p.name === "prominence")!
    expect(prominence.kind).toBe("state")
    expect(prominence.states).toEqual(["utility", "known"])
    expect(prominence.initial).toBe("utility")
  })

  it("handles a kind with no definition at all", () => {
    expect(declaredProperties({ ...person, definition: undefined })).toEqual([])
  })
})

describe("column and filter derivation", () => {
  it("columns drop opaque and long kinds; filters drop only opaque", () => {
    const cols = columnProperties(person).map((p) => p.name)
    expect(cols).toEqual([
      "displayName",
      "emails",
      "name",
      "phones",
      "prominence",
    ])
    const filters = filterableProperties(person).map((p) => p.name)
    expect(filters).toContain("bio")
    expect(filters).not.toContain("raw")
    expect(filters).not.toContain("apiKey")
  })

  it("keeps hot columns out of the declared list — they render as temporal", () => {
    const withHot: KindInfo = {
      ...event,
      definition: {
        traits: ["temporal(point: dueAt)"],
        properties: { dueAt: { type: "datetime" } },
      },
    }
    expect(columnProperties(withHot).map((p) => p.name)).toEqual([])
    expect(temporalProperties(withHot)).toEqual(["dueAt"])
  })
})

describe("temporalProperties", () => {
  it("binds range to at+endsAt, point to at, remap to the named column", () => {
    expect(temporalProperties(event)).toEqual(["at", "endsAt"])
    expect(
      temporalProperties({
        ...event,
        definition: { traits: ["temporal(point)"] },
      })
    ).toEqual(["at"])
    expect(temporalProperties({ ...event, definition: {} })).toEqual([])
  })
})

describe("state and descriptions", () => {
  it("finds the machines and the record-56 one-liners", () => {
    expect(stateProperties(person).map((p) => p.name)).toEqual(["prominence"])
    expect(describeKey(person, "name")).toBe("the full name, one string")
    expect(describeKey(person, "memberOf")).toBe("the employer or workspace")
    expect(describeKey(person, "nothere")).toBeUndefined()
  })
})

describe("kind resolution", () => {
  const org: KindInfo = {
    identity: "people.substrate.reamde.dev/organization",
    name: "organization",
    authority: "people.substrate.reamde.dev",
    version: "",
    plural: "organizations",
    source: "builtin",
  }
  const kinds = [person, org, event]

  it("splits a kind reference at the slash — authority first", () => {
    expect(splitKind("people.substrate.reamde.dev/person")).toEqual({
      authority: "people.substrate.reamde.dev",
      name: "person",
    })
    expect(splitKind("task")).toEqual({ authority: "", name: "task" })
  })

  it("routes authority+plural back to the kind", () => {
    expect(
      kindByCollection(kinds, "people.substrate.reamde.dev", "people")
    ).toBe(person)
    expect(
      kindByCollection(kinds, "people.substrate.reamde.dev", "nope")
    ).toBeUndefined()
  })

  it("resolves a bare edge target inside the declaring authority first", () => {
    expect(resolveEdgeTarget(kinds, person, "organization")).toBe(org)
    expect(
      resolveEdgeTarget(
        kinds,
        person,
        "people.substrate.reamde.dev/organization"
      )
    ).toBe(org)
    expect(resolveEdgeTarget(kinds, person, "missing")).toBeUndefined()
  })

  it("declaredEdges reads rel/to/many/required", () => {
    expect(declaredEdges(person)).toEqual([
      {
        rel: "memberOf",
        to: "organization",
        many: true,
        description: "the employer or workspace",
        required: false,
      },
    ])
  })
})

/** What the Definition tab (and every schema hover) reads off a declaration
 * beyond name and datatype: the presentational hints the substrate stores
 * verbatim — `required`, an enum's admitted set, a pointer's referent. */
describe("declaration detail", () => {
  const config: KindInfo = {
    identity: "github.bundles.substrate.reamde.dev/config",
    name: "config",
    authority: "github.bundles.substrate.reamde.dev",
    version: "",
    plural: "configs",
    source: "installed",
    definition: {
      properties: {
        token: { type: "secret", required: true },
        cadence: {
          type: "enum",
          values: [
            { value: "hourly", label: "Hourly" },
            { value: "daily", label: "" },
          ],
        },
        owner: { type: "reference", to: "people.substrate.reamde.dev/person" },
        plain: { type: "string" },
      },
      edges: { subject: { to: "issue", required: true } },
    },
  }

  it("reads required, enum values and a pointer's referent", () => {
    const props = declaredProperties(config)
    const by = (name: string) => props.find((p) => p.name === name)!
    expect(by("token").required).toBe(true)
    expect(by("plain").required).toBe(false)
    expect(by("cadence").values).toEqual([
      { value: "hourly", label: "Hourly" },
      { value: "daily", label: "" },
    ])
    expect(by("plain").values).toBeUndefined()
    expect(by("owner").to).toBe("people.substrate.reamde.dev/person")
    expect(declaredEdges(config)[0].required).toBe(true)
  })
})

describe("type labels", () => {
  it("spells a property's datatype, repeated marked, pointers aimed", () => {
    const props = declaredProperties(person)
    const by = (name: string) => props.find((p) => p.name === name)!
    expect(propertyTypeLabel(by("name"))).toBe("string")
    expect(propertyTypeLabel(by("emails"))).toBe("email[]")
    expect(propertyTypeLabel(by("prominence"))).toBe("state")
    expect(
      propertyTypeLabel({
        name: "owner",
        kind: "reference",
        to: "person",
        repeated: false,
      })
    ).toBe("reference → person")
  })

  it("spells an edge as an arrow at its target", () => {
    expect(edgeTypeLabel(declaredEdges(person)[0])).toBe("→ organization[]")
    expect(edgeTypeLabel({ rel: "owner", to: "person", many: false })).toBe(
      "→ person"
    )
    expect(edgeTypeLabel({ rel: "loose", to: "", many: false })).toBe("edge")
  })
})

describe("graphqlTypeName", () => {
  it("PascalCases a bare kind's name", () => {
    expect(graphqlTypeName("task")).toBe("Task")
  })

  it("PascalCases a shipped authority's kind name, no prefix", () => {
    expect(graphqlTypeName("people.substrate.reamde.dev/person")).toBe("Person")
  })

  it("prefixes an installed bundle kind with the bundle name", () => {
    expect(graphqlTypeName("google.bundles.substrate.reamde.dev/person")).toBe(
      "Google_Person"
    )
  })
})
