import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  columnProperties,
  declaredProperties,
  declaredReferences,
  describeKey,
  expandableReferences,
  filterableProperties,
  propertyTypeLabel,
  kindByCollection,
  kindByIdentity,
  kindPurpose,
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
  source: "builtin",
  description: "",
  definition: {
    authority: "samples.substrate.reamde.dev",
    package: "people",
    names: { singular: "person" },
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
        kind: "samples.substrate.reamde.dev/people/organization",
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
  source: "builtin",
  description: "",
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
    expect(declaredProperties({ ...person, definition: {} })).toEqual([])
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

  // Every shipped declaration names core's trait in full (decision 0101);
  // matching only the bare spelling dropped a task's `dueAt` everywhere.
  it("reads core's fully qualified temporal trait the same as the bare one", () => {
    const traits = (t: string) =>
      temporalProperties({ ...event, definition: { traits: [t] } })
    expect(traits("substrate.reamde.dev/core/temporal(point: dueAt)")).toEqual([
      "dueAt",
    ])
    expect(traits("substrate.reamde.dev/core/temporal(point)")).toEqual(["at"])
    expect(traits("substrate.reamde.dev/core/temporal(range)")).toEqual([
      "at",
      "endsAt",
    ])
    // Another package's `temporal` is not core's.
    expect(traits("ada.example.com/tasks/temporal(point: dueAt)")).toEqual([])
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
    source: "builtin",
    description: "",
    definition: {},
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

  it("resolves a reference pin by its full identity, and a bare word to nothing", () => {
    expect(
      kindByIdentity(kinds, "samples.substrate.reamde.dev/people/organization")
    ).toBe(org)
    // The server refuses a bare pin (records 0098, 0101), so nothing here
    // completes one: not inside the declaring package, not anywhere.
    expect(kindByIdentity(kinds, "organization")).toBeUndefined()
  })

  it("declaredReferences reads the pin, the container and what it holds", () => {
    const refs = declaredReferences(person)
    expect(refs.map((r) => r.name)).toEqual(["memberOf"])
    expect(refs[0]).toMatchObject({
      to: "samples.substrate.reamde.dev/people/organization",
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
    source: "installed",
    description: "",
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
          kind: "providers.substrate.reamde.dev/github/issue",
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
    expect(propertyTypeLabel(by("memberOf"))).toBe(
      "reference → samples.substrate.reamde.dev/people/organization[]"
    )
  })
})

/** What a list read may ask to expand. The server refuses the WHOLE page for
 * a name it cannot expand, so this set must be exactly the one
 * `engine.expandProperties` admits. */
describe("the reference properties a page may expand", () => {
  const kind: KindInfo = {
    identity: "ada.example.com/tasks/task",
    name: "task",
    authority: "ada.example.com",
    package: "tasks",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      properties: {
        name: { type: "string" },
        assignee: { type: "reference", kind: "ada.example.com/people/person" },
        watchers: {
          type: "reference",
          kind: "ada.example.com/people/person",
          repeated: true,
        },
        // A keyed map of pointers is the one reference shape the server does
        // not expand; naming it would 422 the page.
        roles: {
          type: "reference",
          kind: "ada.example.com/people/person",
          keyed: true,
        },
      },
    },
  }

  it("takes a single and a repeated reference, and nothing else", () => {
    expect(expandableReferences(kind)).toEqual(["assignee", "watchers"])
  })

  it("reads the live person kind's own pointer", () => {
    expect(expandableReferences(person)).toEqual(["memberOf"])
  })
})

describe("kindPurpose", () => {
  const withPurpose = (purpose: unknown, authority = "example.com") => ({
    ...person,
    identity: `${authority}/things/thing`,
    authority,
    definition: { ...person.definition, purpose },
  })

  it("reads the declared purpose, absent being primary", () => {
    expect(kindPurpose(person)).toBe("primary")
    expect(kindPurpose(withPurpose("supporting"))).toBe("supporting")
    expect(kindPurpose(withPurpose("internal"))).toBe("internal")
    expect(kindPurpose(withPurpose("decorative"))).toBe("primary")
  })

  it("treats every kind of the substrate's own authority as internal", () => {
    expect(kindPurpose(withPurpose("primary", "substrate.reamde.dev"))).toBe(
      "internal"
    )
    expect(kindPurpose("substrate.reamde.dev/llm/thread")).toBe("internal")
    expect(kindPurpose("samples.substrate.reamde.dev/tasks/task")).toBe(
      "primary"
    )
  })
})
