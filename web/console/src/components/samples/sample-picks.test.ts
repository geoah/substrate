import { describe, expect, it } from "vitest"

import { collectionSamples } from "./sample-picks"
import type { BundleClosure, CatalogItem, KindInfo } from "@/lib/api/types"
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
    tier: "sample",
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
    id: `${HOME}/${pkg}`,
    name: pkg,
    authority: HOME,
    package: pkg,
    catalog,
    installed,
    tier: "sample",
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
        sample("firecrawl", {
          kinds: [`${HOME}/firecrawl/webdocument`],
          kindPurposes: { [`${HOME}/firecrawl/webdocument`]: "supporting" },
          functions: [`${HOME}/firecrawl/scrape`],
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
      tier: "provider",
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
