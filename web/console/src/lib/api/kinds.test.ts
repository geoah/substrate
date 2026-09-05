import { describe, expect, it } from "vitest"

import { buildKindNav, normalizeKinds } from "./kinds"
import type { KindInfo } from "./types"

function kindInfo(overrides: Partial<KindInfo>): KindInfo {
  return {
    identity: "samples.substrate.reamde.dev/people/person",
    name: "person",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    version: 0,
    plural: "persons",
    source: "builtin",
    ...overrides,
  }
}

describe("normalizeKinds", () => {
  it("reads the kind envelope the live registry serves", () => {
    const payload = {
      records: [
        {
          id: "samples.substrate.reamde.dev/people/person",
          kind: "substrate.reamde.dev/core/kind",
          properties: {
            name: "person",
            authority: "samples.substrate.reamde.dev",
            package: "people",
            plural: "persons",
            source: "builtin",
            definition: {
              authority: "samples.substrate.reamde.dev",
              package: "people",
              description: "One human, one record.",
            },
          },
        },
      ],
      cursor: undefined,
    }
    expect(normalizeKinds(payload)).toEqual([
      {
        identity: "samples.substrate.reamde.dev/people/person",
        name: "person",
        authority: "samples.substrate.reamde.dev",
        package: "people",
        version: 0,
        plural: "persons",
        source: "builtin",
        description: "One human, one record.",
        definition: {
          authority: "samples.substrate.reamde.dev",
          package: "people",
          description: "One human, one record.",
        },
      },
    ])
  })

  // The kind's description is the DECLARATION's — core's `kind` carries no
  // description column for it to be projected onto.
  it("reads the description off the stored declaration", () => {
    const [k] = normalizeKinds({
      records: [
        {
          id: "samples.substrate.reamde.dev/tasks/task",
          properties: { definition: { description: "Something to do." } },
        },
      ],
    })
    expect(k?.description).toBe("Something to do.")
  })

  it("accepts a bare KindInfo list as the same registry", () => {
    const flat = [kindInfo({})]
    expect(normalizeKinds(flat)).toEqual(flat)
  })

  it("derives name and authority from the id when properties are sparse", () => {
    const [k] = normalizeKinds({
      records: [
        { id: "samples.substrate.reamde.dev/tasks/task", properties: {} },
      ],
    })
    expect(k).toMatchObject({
      identity: "samples.substrate.reamde.dev/tasks/task",
      name: "task",
      authority: "samples.substrate.reamde.dev",
      package: "tasks",
      plural: "task",
    })
  })

  it("reads a bare (repository-local) kind's id as an authority-less reference", () => {
    const [k] = normalizeKinds({ records: [{ id: "task", properties: {} }] })
    expect(k).toMatchObject({ identity: "task", name: "task", authority: "" })
  })

  it("skips rows without an id", () => {
    expect(normalizeKinds({ records: [{ properties: {} }] })).toEqual([])
  })

  it("reads the declaration version as a number: 0 when absent or not numeric", () => {
    const read = (version?: unknown) =>
      normalizeKinds({
        records: [
          {
            id: "samples.substrate.reamde.dev/tasks/task",
            properties: { version },
          },
        ],
      })[0]?.version
    expect(read(3)).toBe(3)
    expect(read("3")).toBe(3)
    expect(read(undefined)).toBe(0)
    // A pre-rename declaration still holding a string version reads as absent.
    expect(read("v1alpha1")).toBe(0)
  })
})

describe("buildKindNav", () => {
  const kinds: KindInfo[] = [
    kindInfo({ identity: "samples.substrate.reamde.dev/people/person" }),
    kindInfo({
      identity: "samples.substrate.reamde.dev/people/organization",
      name: "organization",
      plural: "organizations",
    }),
    kindInfo({
      identity: "samples.substrate.reamde.dev/tasks/task",
      name: "task",
      authority: "samples.substrate.reamde.dev",
      package: "tasks",
      plural: "tasks",
    }),
    kindInfo({
      identity: "substrate.reamde.dev/core/kind",
      name: "kind",
      authority: "substrate.reamde.dev",
      package: "core",
      plural: "kinds",
      source: "builtin",
    }),
    kindInfo({
      identity: "providers.substrate.reamde.dev/google/contact",
      name: "contact",
      authority: "providers.substrate.reamde.dev",
      package: "google",
      plural: "contacts",
      source: "installed",
    }),
    kindInfo({
      identity: "providers.substrate.reamde.dev/google/syncrun",
      name: "syncrun",
      authority: "providers.substrate.reamde.dev",
      package: "google",
      plural: "syncruns",
      source: "installed",
    }),
  ]

  it("lists every authority flat at one level — repository authorities first, machinery last", () => {
    const nav = buildKindNav(kinds)
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "samples.substrate.reamde.dev",
      "substrate.reamde.dev",
      "providers.substrate.reamde.dev",
    ])
  })

  it("holds each authority's packages, package-name sorted", () => {
    const nav = buildKindNav(kinds)
    const samples = nav.authorities.find(
      (a) => a.authority === "samples.substrate.reamde.dev"
    )
    expect(samples?.packages.map((p) => p.package)).toEqual(["people", "tasks"])
    expect(samples?.packages.map((p) => p.identity)).toEqual([
      "samples.substrate.reamde.dev/people",
      "samples.substrate.reamde.dev/tasks",
    ])
  })

  it("keeps core first among machinery authorities even out of alphabet", () => {
    const nav = buildKindNav([
      kindInfo({
        identity: "providers.substrate.reamde.dev/beeper/beeperuser",
        name: "beeperuser",
        authority: "providers.substrate.reamde.dev",
        package: "beeper",
        plural: "beeperusers",
        source: "installed",
      }),
      kindInfo({
        identity: "substrate.reamde.dev/core/kind",
        name: "kind",
        authority: "substrate.reamde.dev",
        package: "core",
        plural: "kinds",
      }),
    ])
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "substrate.reamde.dev",
      "providers.substrate.reamde.dev",
    ])
  })

  it("sorts kinds inside a package by name", () => {
    const nav = buildKindNav(kinds)
    const people = nav.authorities
      .find((a) => a.authority === "samples.substrate.reamde.dev")
      ?.packages.find((p) => p.package === "people")
    expect(people?.kinds.map((k) => k.name)).toEqual(["organization", "person"])
  })

  it("an authority with any schema-declared kind sorts ahead of machinery", () => {
    const nav = buildKindNav([
      kindInfo({
        identity: "acme.substrate.reamde.dev/acme/custom",
        name: "custom",
        authority: "acme.substrate.reamde.dev",
        package: "acme",
        plural: "customs",
        source: "schema",
      }),
      kindInfo({
        identity: "acme.substrate.reamde.dev/acme/installedtoo",
        name: "installedtoo",
        authority: "acme.substrate.reamde.dev",
        package: "acme",
        plural: "installedtoos",
        source: "installed",
      }),
      kindInfo({
        identity: "substrate.reamde.dev/core/kind",
        name: "kind",
        authority: "substrate.reamde.dev",
        package: "core",
        plural: "kinds",
      }),
    ])
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "acme.substrate.reamde.dev",
      "substrate.reamde.dev",
    ])
  })
})
