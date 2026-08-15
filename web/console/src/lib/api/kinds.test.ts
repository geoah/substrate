import { describe, expect, it } from "vitest"

import { buildKindNav, normalizeKinds } from "./kinds"
import type { KindInfo } from "./types"

function kindInfo(overrides: Partial<KindInfo>): KindInfo {
  return {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
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
          id: "people.substrate.reamde.dev/person",
          kind: "core.substrate.reamde.dev/kind",
          properties: {
            name: "person",
            authority: "people.substrate.reamde.dev",
            plural: "persons",
            source: "builtin",
            definition: {
              authority: "people.substrate.reamde.dev",
              description: "One human, one record.",
            },
          },
        },
      ],
      cursor: undefined,
    }
    expect(normalizeKinds(payload)).toEqual([
      {
        identity: "people.substrate.reamde.dev/person",
        name: "person",
        authority: "people.substrate.reamde.dev",
        version: 0,
        plural: "persons",
        source: "builtin",
        description: "One human, one record.",
        definition: {
          authority: "people.substrate.reamde.dev",
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
          id: "tasks.substrate.reamde.dev/task",
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
      records: [{ id: "tasks.substrate.reamde.dev/task", properties: {} }],
    })
    expect(k).toMatchObject({
      identity: "tasks.substrate.reamde.dev/task",
      name: "task",
      authority: "tasks.substrate.reamde.dev",
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
          { id: "tasks.substrate.reamde.dev/task", properties: { version } },
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
    kindInfo({ identity: "people.substrate.reamde.dev/person" }),
    kindInfo({
      identity: "people.substrate.reamde.dev/organization",
      name: "organization",
      plural: "organizations",
    }),
    kindInfo({
      identity: "tasks.substrate.reamde.dev/task",
      name: "task",
      authority: "tasks.substrate.reamde.dev",
      plural: "tasks",
    }),
    kindInfo({
      identity: "core.substrate.reamde.dev/kind",
      name: "kind",
      authority: "core.substrate.reamde.dev",
      plural: "kinds",
      source: "builtin",
    }),
    kindInfo({
      identity: "google.bundles.substrate.reamde.dev/contact",
      name: "contact",
      authority: "google.bundles.substrate.reamde.dev",
      plural: "contacts",
      source: "installed",
    }),
    kindInfo({
      identity: "google.bundles.substrate.reamde.dev/syncrun",
      name: "syncrun",
      authority: "google.bundles.substrate.reamde.dev",
      plural: "syncruns",
      source: "installed",
    }),
  ]

  it("lists every authority flat at one level — repository authorities first, machinery last", () => {
    const nav = buildKindNav(kinds)
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "people.substrate.reamde.dev",
      "tasks.substrate.reamde.dev",
      "core.substrate.reamde.dev",
      "google.bundles.substrate.reamde.dev",
    ])
  })

  it("keeps core first among machinery authorities even out of alphabet", () => {
    const nav = buildKindNav([
      kindInfo({
        identity: "beeper.bundles.substrate.reamde.dev/beeperuser",
        name: "beeperuser",
        authority: "beeper.bundles.substrate.reamde.dev",
        plural: "beeperusers",
        source: "installed",
      }),
      kindInfo({
        identity: "core.substrate.reamde.dev/kind",
        name: "kind",
        authority: "core.substrate.reamde.dev",
        plural: "kinds",
      }),
    ])
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "core.substrate.reamde.dev",
      "beeper.bundles.substrate.reamde.dev",
    ])
  })

  it("sorts kinds inside an authority by name", () => {
    const nav = buildKindNav(kinds)
    const people = nav.authorities.find(
      (a) => a.authority === "people.substrate.reamde.dev"
    )
    expect(people?.kinds.map((k) => k.name)).toEqual(["organization", "person"])
  })

  it("an authority with any schema-declared kind sorts ahead of machinery", () => {
    const nav = buildKindNav([
      kindInfo({
        identity: "acme.substrate.reamde.dev/custom",
        name: "custom",
        authority: "acme.substrate.reamde.dev",
        plural: "customs",
        source: "schema",
      }),
      kindInfo({
        identity: "acme.substrate.reamde.dev/installedtoo",
        name: "installedtoo",
        authority: "acme.substrate.reamde.dev",
        plural: "installedtoos",
        source: "installed",
      }),
      kindInfo({
        identity: "core.substrate.reamde.dev/kind",
        name: "kind",
        authority: "core.substrate.reamde.dev",
        plural: "kinds",
      }),
    ])
    expect(nav.authorities.map((a) => a.authority)).toEqual([
      "acme.substrate.reamde.dev",
      "core.substrate.reamde.dev",
    ])
  })
})
