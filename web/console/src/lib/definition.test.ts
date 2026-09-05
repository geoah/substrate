import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  columnProperties,
  declaredProperties,
  declaredReferences,
  describeKey,
  filterableProperties,
  graphqlTypeName,
  propertyTypeLabel,
  kindByCollection,
  resolveReferenceTarget,
  splitKind,
  stateProperties,
  temporalProperties,
} from "./definition"

/** A faithful slice of the live person kind (record 56 shapes). */
const person: KindInfo = {
  identity: "samples.substrate.reamde.dev/people/person",
  name: "person",
  authority: "samples.substrate.reamde.dev",
  package: "people",
  version: 0,
  plural: "people",
  source: "builtin",
  definition: {
    authority: "samples.substrate.reamde.dev",
    package: "people",
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
      memberOf: {
        type: "reference",
        kind: "organization",
        repeated: true,
        mustExist: true,
        description: "the employer or workspace",
        properties: {
          role: { type: "string" },
          since: { type: "date" },
        },
      },
    },
  },
}

const event: KindInfo = {
  identity: "samples.substrate.reamde.dev/calendar/calendarevent",
  name: "calendarevent",
  authority: "samples.substrate.reamde.dev",
  package: "calendar",
  version: 0,
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
      "memberOf",
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
    // A reference earns a column like any other property: its cell names the
    // record it points at.
    expect(cols).toEqual([
      "displayName",
      "emails",
      "memberOf",
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
    identity: "samples.substrate.reamde.dev/people/organization",
    name: "organization",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    version: 0,
    plural: "organizations",
    source: "builtin",
  }
  const kinds = [person, org, event]

  it("splits a kind reference into authority, package and name", () => {
    expect(splitKind("samples.substrate.reamde.dev/people/person")).toEqual({
      authority: "samples.substrate.reamde.dev",
      pkg: "people",
      name: "person",
    })
    expect(splitKind("task")).toEqual({
      authority: "",
      pkg: "",
      name: "task",
    })
  })

  it("routes authority+package+name back to the kind", () => {
    expect(
      kindByCollection(
        kinds,
        "samples.substrate.reamde.dev",
        "people",
        "person"
      )
    ).toBe(person)
    expect(
      kindByCollection(kinds, "samples.substrate.reamde.dev", "people", "nope")
    ).toBeUndefined()
  })

  it("resolves a bare reference pin inside the declaring package first", () => {
    expect(resolveReferenceTarget(kinds, person, "organization")).toBe(org)
    expect(
      resolveReferenceTarget(
        kinds,
        person,
        "samples.substrate.reamde.dev/people/organization"
      )
    ).toBe(org)
    expect(resolveReferenceTarget(kinds, person, "missing")).toBeUndefined()
  })

  it("declaredReferences reads the pin, the container and what it holds", () => {
    const refs = declaredReferences(person)
    expect(refs.map((r) => r.name)).toEqual(["memberOf"])
    expect(refs[0]).toMatchObject({
      to: "organization",
      repeated: true,
      mustExist: true,
      description: "the employer or workspace",
      // The LINK DATA: `memberOf` is the one shipped reference that carries
      // any, so a value there is `{ref, role, since}` and not a bare path.
      linkProperties: ["role", "since"],
    })
  })

  it("leaves a non-reference property out of declaredReferences", () => {
    expect(declaredReferences(person).map((r) => r.name)).not.toContain("name")
  })
})

/** What the Definition tab (and every schema hover) reads off a declaration
 * beyond name and datatype: the presentational hints the substrate stores
 * verbatim — `required`, an enum's admitted set, a pointer's referent. */
describe("declaration detail", () => {
  const config: KindInfo = {
    identity: "providers.substrate.reamde.dev/github/config",
    name: "config",
    authority: "providers.substrate.reamde.dev",
    package: "github",
    version: 0,
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
        owner: {
          type: "reference",
          kind: "samples.substrate.reamde.dev/people/person",
        },
        plain: { type: "string" },
        subject: {
          type: "reference",
          kind: "issue",
          required: true,
          mustExist: true,
          subject: true,
        },
      },
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
    expect(by("owner").to).toBe("samples.substrate.reamde.dev/people/person")
    // A mapping's subject is a reference like any other, marked `subject`.
    expect(by("subject")).toMatchObject({
      required: true,
      mustExist: true,
      subject: true,
    })
    expect(by("owner").mustExist).toBe(false)
    expect(by("owner").subject).toBe(false)
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
    // A repeated reference wears the container marker like any other property.
    expect(propertyTypeLabel(by("memberOf"))).toBe("reference → organization[]")
  })
})

describe("graphqlTypeName", () => {
  it("PascalCases a bare kind's name", () => {
    expect(graphqlTypeName("task")).toBe("Task")
  })

  it("PascalCases a shipped authority's kind name, no prefix", () => {
    expect(graphqlTypeName("samples.substrate.reamde.dev/people/person")).toBe(
      "Person"
    )
  })

  it("prefixes an installed bundle kind with the bundle name", () => {
    expect(
      graphqlTypeName(
        "providers.substrate.reamde.dev/google/person",
        "installed"
      )
    ).toBe("Google_Person")
  })

  it("prefixes a published provider kind too", () => {
    // A provider's declarations are a copy the repository holds, so the server
    // names them like any other non-seed kind (vocabulary GraphQLName).
    expect(
      graphqlTypeName(
        "providers.substrate.reamde.dev/whoop/account",
        "published"
      )
    ).toBe("Whoop_Account")
  })

  it("leaves a seeded kind bare", () => {
    expect(graphqlTypeName("substrate.reamde.dev/core/token", "builtin")).toBe(
      "Token"
    )
  })
})
