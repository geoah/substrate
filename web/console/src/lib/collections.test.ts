import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  collectionGroups,
  authorityTitle,
  collectionSource,
  packageTitle,
  groupToggleKey,
  hiddenExamples,
  isGroupOpen,
} from "./collections"

function kind(identity: string, purpose?: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: purpose ? { purpose } : {},
  }
}

const KINDS = [
  kind("ada.example.com/tasks/task"),
  kind("ada.example.com/tasks/tasklog", "supporting"),
  kind("ada.example.com/people/person"),
  kind("other.example.com/notes/note"),
  kind("providers.substrate.reamde.dev/google/contact"),
  kind("providers.substrate.reamde.dev/google/emailaddress", "supporting"),
  kind("providers.substrate.reamde.dev/google/gmaillabel", "supporting"),
  kind("providers.substrate.reamde.dev/google/account", "internal"),
  kind("providers.substrate.reamde.dev/linear/issue"),
  kind("substrate.reamde.dev/core/kind", "internal"),
  kind("substrate.reamde.dev/llm/thread", "internal"),
  kind("substrate.reamde.dev/console/recordview", "internal"),
  kind("substrate.reamde.dev/labs/labreport", "primary"),
  kind("substrate.reamde.dev/labs/labresult", "supporting"),
  kind("substrate.reamde.dev/observations/observation", "supporting"),
]

describe("collectionGroups", () => {
  const groups = collectionGroups(KINDS, "ada.example.com")

  it("reads each package by label, then each provider by name, then the substrate", () => {
    expect(groups.map((g) => g.label)).toEqual([
      "Labs",
      "Notes",
      "Observations",
      "People",
      "Tasks",
      "From Google",
      "From Linear",
      "Substrate",
    ])
    expect(groups.map((g) => g.type)).toEqual([
      "package",
      "package",
      "package",
      "package",
      "package",
      "provider",
      "provider",
      "system",
    ])
  })

  it("gives every package but the providers' and the machinery its own group, keyed by its reference", () => {
    const tasks = groups.find((g) => g.label === "Tasks")
    expect(tasks?.id).toBe("group:package:ada.example.com/tasks")
    expect(tasks?.primary.map((k) => k.identity)).toEqual([
      "ada.example.com/tasks/task",
    ])
    expect(tasks?.hidden.map((k) => k.identity)).toEqual([
      "ada.example.com/tasks/tasklog",
    ])
    const labs = groups.find((g) => g.label === "Labs")
    expect(labs?.authorities.map((a) => a.authority)).toEqual([
      "substrate.reamde.dev",
    ])
    expect(labs?.kinds.map((k) => k.identity)).toEqual([
      "substrate.reamde.dev/labs/labreport",
      "substrate.reamde.dev/labs/labresult",
    ])
  })

  it("keeps a package with no primary kind, for technical mode", () => {
    const observations = groups.find((g) => g.label === "Observations")
    expect(observations?.primary).toEqual([])
    expect(observations?.hidden).toHaveLength(1)
  })

  it("names the publisher where two packages share a word, home first", () => {
    const shared = collectionGroups(
      [
        kind("substrate.reamde.dev/training/session"),
        kind("zed.example.com/training/session"),
        kind("zed.example.com/tasks/task"),
      ],
      "zed.example.com"
    )
    expect(shared.map((g) => g.label)).toEqual([
      "Tasks",
      "Training (zed.example.com)",
      "Training (substrate.reamde.dev)",
    ])
    expect(shared.map((g) => g.id)).toEqual([
      "group:package:zed.example.com/tasks",
      "group:package:zed.example.com/training",
      "group:package:substrate.reamde.dev/training",
    ])
  })

  it("files only the core, llm and console packages under Substrate", () => {
    expect(
      groups
        .at(-1)
        ?.kinds.map((k) => k.identity)
        .sort()
    ).toEqual([
      "substrate.reamde.dev/console/recordview",
      "substrate.reamde.dev/core/kind",
      "substrate.reamde.dev/llm/thread",
    ])
  })

  it("lists only primary kinds as primary and core as hidden machinery", () => {
    const google = groups.find((g) => g.type === "provider")
    expect(google?.provider?.name).toBe("Google")
    expect(google?.primary.map((k) => k.identity)).toEqual([
      "providers.substrate.reamde.dev/google/contact",
    ])
    expect(google?.hidden).toHaveLength(3)
    expect(groups.at(-1)?.primary).toEqual([])
  })

  it("names a few hidden kinds for the note under a group", () => {
    const google = groups.find((g) => g.label === "From Google")
    expect(google && hiddenExamples(google)).toBe(
      "email addresses and Gmail labels"
    )
  })
})

describe("the owner's data inside the machinery packages", () => {
  const groups = collectionGroups(
    [
      kind("substrate.reamde.dev/core/kind", "internal"),
      kind("substrate.reamde.dev/core/contact", "primary"),
      kind("substrate.reamde.dev/llm/memory", "primary"),
      kind("substrate.reamde.dev/llm/thread", "internal"),
    ],
    "ada.example.com"
  )

  it("gives each owner-data kind a group of its own, named by its plural, before the substrate", () => {
    expect(groups.map((g) => [g.id, g.label, g.type])).toEqual([
      [
        "group:package:substrate.reamde.dev/core/contact",
        "Contacts",
        "package",
      ],
      ["group:package:substrate.reamde.dev/llm/memory", "Memories", "package"],
      ["group:system", "Substrate", "system"],
    ])
    expect(groups[0].primary.map((k) => k.identity)).toEqual([
      "substrate.reamde.dev/core/contact",
    ])
    expect(
      groups
        .at(-1)
        ?.kinds.map((k) => k.identity)
        .sort()
    ).toEqual([
      "substrate.reamde.dev/core/kind",
      "substrate.reamde.dev/llm/thread",
    ])
  })

  it("says an owner-data kind sits in its own group", () => {
    expect(collectionSource("substrate.reamde.dev/core/contact")).toBe(
      "Contacts"
    )
  })
})

describe("group folding", () => {
  it("starts provider groups folded and every other group open", () => {
    const tasks = {
      id: "group:package:ada.example.com/tasks",
      type: "package" as const,
    }
    const google = { id: "group:provider:google", type: "provider" as const }
    expect(isGroupOpen(tasks, [])).toBe(true)
    expect(isGroupOpen(google, [])).toBe(false)
    expect(isGroupOpen(tasks, [groupToggleKey(tasks)])).toBe(false)
    expect(isGroupOpen(google, [groupToggleKey(google)])).toBe(true)
  })
})

describe("authority and package titles", () => {
  it("names an authority where its collections sit", () => {
    expect(authorityTitle("ada.example.com")).toBe("ada.example.com")
    expect(authorityTitle("providers.substrate.reamde.dev")).toBe(
      "From providers"
    )
    expect(authorityTitle("substrate.reamde.dev")).toBe("Substrate")
  })

  it("names a package by its word, and a provider by its name", () => {
    expect(packageTitle("ada.example.com", "tasks")).toBe("Tasks package")
    expect(packageTitle("substrate.reamde.dev", "llm")).toBe("LLM package")
    expect(packageTitle("providers.substrate.reamde.dev", "google")).toBe(
      "Google"
    )
  })
})

describe("collectionSource", () => {
  it("says where a kind comes from: its package, a provider or the substrate", () => {
    expect(collectionSource("ada.example.com/tasks/task")).toBe("Tasks")
    expect(
      collectionSource("providers.substrate.reamde.dev/google/contact")
    ).toBe("From Google")
    expect(collectionSource("substrate.reamde.dev/core/kind")).toBe("Substrate")
    expect(collectionSource("substrate.reamde.dev/llm/thread")).toBe(
      "Substrate"
    )
    expect(collectionSource("substrate.reamde.dev/labs/labreport")).toBe("Labs")
    expect(collectionSource("substrate.reamde.dev/clinical/encounter")).toBe(
      "Clinical"
    )
  })
})
