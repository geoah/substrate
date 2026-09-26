import { describe, expect, it } from "vitest"

import { collectionSamples } from "./sample-collections"
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
    expect(out[0].kinds).toEqual([
      `${HOME}/tasks/task`,
      `${HOME}/tasks/project`,
    ])
  })

  it("leaves out samples that ship tools or agents, and ones with no kind", () => {
    const out = collectionSamples(
      [
        sample("firecrawl", {
          kinds: [`${HOME}/firecrawl/webdocument`],
          functions: [`${HOME}/firecrawl/scrape`],
        }),
        sample("llm", {
          kinds: [`${HOME}/llm/scratchpad`],
          agents: [`${HOME}/llm/assistant`],
        }),
        sample("scheduling", { traits: [`${HOME}/scheduling/occurrence`] }),
        sample("people", { kinds: [`${HOME}/people/person`] }),
      ],
      []
    )
    expect(out.map((s) => s.row.package)).toEqual(["people"])
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
