import { describe, expect, it } from "vitest"

import {
  callableSamples,
  collectionSamples,
  standardCollections,
} from "./sample-picks"
import type {
  BundleClosure,
  CatalogBrings,
  CatalogItem,
  KindInfo,
} from "@/lib/api/types"
import type { BundleRow } from "@/lib/bundles"

const HOME = "ada.example.com"

function sample(
  pkg: string,
  closure: Partial<BundleClosure>,
  installed = false
): BundleRow {
  const catalog = {
    id: `samples.substrate.reamde.dev/${pkg}`,
    name: pkg,
    authority: HOME,
    package: pkg,
    description: "",
    version: 1,
    keptBy: "repository",
    installed,
    closure: {
      kinds: null,
      traits: null,
      functions: null,
      agents: null,
      mappings: null,
      triggers: null,
      records: null,
      ...closure,
    },
  } as CatalogItem
  return {
    key: catalog.id,
    id: `${HOME}/${pkg}`,
    name: pkg,
    authority: HOME,
    package: pkg,
    catalog,
    installed,
    keptBy: "repository",
    requires: [],
  }
}

function kind(identity: string, purpose?: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    authority,
    package: pkg,
    name,
    definition: purpose ? { purpose } : {},
  } as unknown as KindInfo
}

describe("collectionSamples", () => {
  it("offers a data sample by its primary kinds, the one it is named for first", () => {
    const tasks = sample(
      "tasks",
      {
        kinds: [
          `${HOME}/tasks/project`,
          `${HOME}/tasks/task`,
          `${HOME}/tasks/tasklog`,
        ],
      },
      true
    )
    const out = collectionSamples(
      [tasks],
      [
        kind(`${HOME}/tasks/project`),
        kind(`${HOME}/tasks/task`),
        kind(`${HOME}/tasks/tasklog`, "supporting"),
      ]
    )
    expect(out).toHaveLength(1)
    expect(out[0].members).toEqual([
      `${HOME}/tasks/task`,
      `${HOME}/tasks/project`,
    ])
  })

  it("offers a sample by its primary kinds whatever else it ships", () => {
    const out = collectionSamples(
      [
        sample("pebble", {
          kinds: [`${HOME}/pebble/instruction`],
          kindPurposes: { [`${HOME}/pebble/instruction`]: "supporting" },
          functions: [`${HOME}/pebble/ingest`],
        }),
        sample("notes", {
          kinds: [`${HOME}/notes/note`],
          functions: [`${HOME}/notes/savenote`],
          agents: [`${HOME}/notes/titler`],
        }),
        sample("scheduling", { traits: [`${HOME}/scheduling/occurrence`] }),
        sample("people", { kinds: [`${HOME}/people/person`] }),
      ],
      []
    )
    expect(out.map((s) => s.row.package)).toEqual(["notes", "people"])
  })

  it("reads the shipped purpose where the held copy declares none", () => {
    const scratchpad = `${HOME}/llm/scratchpad`
    const llm = sample(
      "llm",
      {
        kinds: [scratchpad],
        kindPurposes: { [scratchpad]: "supporting" },
        agents: [`${HOME}/llm/substrate`],
      },
      true
    )
    expect(collectionSamples([llm], [kind(scratchpad)])).toEqual([])
    // A copy its owner classified keeps its own word.
    expect(
      collectionSamples([llm], [kind(scratchpad, "primary")]).map(
        (s) => s.members
      )
    ).toEqual([[scratchpad]])
  })

  it("leaves out a sample whose kinds all support another", () => {
    const out = collectionSamples(
      [sample("notes", { kinds: [`${HOME}/notes/tag`] }, true)],
      [kind(`${HOME}/notes/tag`, "supporting")]
    )
    expect(out).toEqual([])
  })

  it("lists them by name, providers left out", () => {
    const provider: BundleRow = {
      ...sample("google", { kinds: ["providers.example/google/contact"] }),
      keptBy: "publisher",
    }
    const out = collectionSamples(
      [
        sample("tasks", { kinds: [`${HOME}/tasks/task`] }),
        provider,
        sample("calendar", { kinds: [`${HOME}/calendar/calendarevent`] }),
      ],
      []
    )
    expect(out.map((s) => s.row.package)).toEqual(["calendar", "tasks"])
  })
})

describe("callableSamples", () => {
  const rows = [
    sample("notes", {
      kinds: [`${HOME}/notes/note`],
      functions: [`${HOME}/notes/savenote`],
      agents: [`${HOME}/notes/titler`],
    }),
    sample("readinglist", {
      functions: [`${HOME}/readinglist/fetchpage`],
    }),
    sample("llm", { agents: [`${HOME}/llm/substrate`] }),
    sample("people", { kinds: [`${HOME}/people/person`] }),
  ]

  it("offers the samples that ship tools, by name, with their tools", () => {
    const out = callableSamples(rows, "functions")
    expect(out.map((s) => s.row.package)).toEqual(["notes", "readinglist"])
    expect(out[0].members).toEqual([`${HOME}/notes/savenote`])
  })

  it("offers the samples that ship agents", () => {
    expect(callableSamples(rows, "agents").map((s) => s.row.package)).toEqual([
      "llm",
      "notes",
    ])
  })

  it("leaves providers out", () => {
    const provider: BundleRow = {
      ...sample("google", { functions: ["providers.example/google/sync"] }),
      keptBy: "publisher",
    }
    expect(callableSamples([provider], "functions")).toEqual([])
  })
})

/** A published package as the catalog serves it: what it is for comes with
 * it (decision record 0238). */
function published(
  authority: string,
  pkg: string,
  over: {
    kinds?: string[]
    brings?: CatalogBrings[]
    provider?: boolean
    addonFor?: string
  }
): BundleRow {
  const id = `${authority}/${pkg}`
  const catalog = {
    id,
    name: pkg,
    authority,
    package: pkg,
    description: `${pkg}: what it keeps. More prose.`,
    version: 1,
    keptBy: "publisher",
    provider: over.provider,
    brings: over.brings,
    addonFor: over.addonFor,
    installed: false,
    closure: {
      kinds: over.kinds ?? null,
      traits: null,
      functions: null,
      agents: null,
      mappings: null,
      triggers: null,
      records: null,
    },
  } as CatalogItem
  return {
    key: id,
    id,
    name: pkg,
    authority,
    package: pkg,
    catalog,
    installed: false,
    keptBy: "publisher",
    requires: [],
  }
}

describe("standardCollections", () => {
  const SEED = "substrate.reamde.dev"
  const PROV = "providers.substrate.reamde.dev"
  const rows = [
    published(SEED, "clinical", {
      kinds: [`${SEED}/clinical/allergy`, `${SEED}/clinical/patient`],
      brings: ["collections"],
    }),
    published(SEED, "environment", { brings: ["metrics"] }),
    published(PROV, "google", {
      kinds: [`${PROV}/google/contact`],
      brings: ["account", "collections"],
      provider: true,
    }),
    published(PROV, "slacklive", {
      brings: [],
      provider: true,
      addonFor: `${PROV}/slack`,
    }),
    published("apps.example", "coach", { brings: ["agents"] }),
    sample("tasks", { kinds: [`${HOME}/tasks/task`] }),
  ]

  it("offers the published packages that are no provider and add collections or metrics", () => {
    expect(standardCollections(rows, []).map((s) => s.row.package)).toEqual([
      "clinical",
      "environment",
    ])
  })

  it("names each by its primary kinds, and one that adds metrics alone by none", () => {
    const out = standardCollections(rows, [
      kind(`${SEED}/clinical/allergy`, "primary"),
      kind(`${SEED}/clinical/patient`, "supporting"),
    ])
    expect(out.map((s) => s.members)).toEqual([
      [`${SEED}/clinical/allergy`],
      [],
    ])
  })
})
