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
  kind("~/tasks/task"),
  kind("~/tasks/tasklog", "supporting"),
  kind("~/people/person"),
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
  const groups = collectionGroups(KINDS)

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
    expect(tasks?.id).toBe("group:package:~/tasks")
    expect(tasks?.primary.map((k) => k.identity)).toEqual(["~/tasks/task"])
    expect(tasks?.hidden.map((k) => k.identity)).toEqual(["~/tasks/tasklog"])
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

  it("names the publisher where two packages share a word, the owner's own first", () => {
    const shared = collectionGroups([
      kind("substrate.reamde.dev/training/session"),
      kind("aaa.example.com/training/session"),
      kind("~/training/session"),
      kind("~/tasks/task"),
    ])
    expect(shared.map((g) => g.label)).toEqual([
      "Tasks",
      "Training (yours)",
      "Training (aaa.example.com)",
      "Training (substrate.reamde.dev)",
    ])
    expect(shared.map((g) => g.id)).toEqual([
      "group:package:~/tasks",
      "group:package:~/training",
      "group:package:aaa.example.com/training",
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
  const groups = collectionGroups([
    kind("substrate.reamde.dev/core/kind", "internal"),
    kind("substrate.reamde.dev/core/contact", "primary"),
    kind("substrate.reamde.dev/llm/memory", "primary"),
    kind("substrate.reamde.dev/llm/instruction", "primary"),
    kind("substrate.reamde.dev/llm/thread", "internal"),
  ])

  it("gives an owner-data kind a group of its own, and the assistant's memories and instructions one together, before the substrate", () => {
    expect(groups.map((g) => [g.id, g.label, g.type])).toEqual([
      ["group:package:substrate.reamde.dev/llm", "Assistant", "package"],
      [
        "group:package:substrate.reamde.dev/core/contact",
        "Contacts",
        "package",
      ],
      ["group:system", "Substrate", "system"],
    ])
    expect(groups[0].primary.map((k) => k.identity)).toEqual([
      "substrate.reamde.dev/llm/instruction",
      "substrate.reamde.dev/llm/memory",
    ])
    expect(groups[1].primary.map((k) => k.identity)).toEqual([
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

  it("says an owner-data kind sits in its own group, or the group it shares", () => {
    expect(collectionSource("substrate.reamde.dev/core/contact")).toBe(
      "Contacts"
    )
    expect(collectionSource("substrate.reamde.dev/llm/memory")).toBe(
      "Assistant"
    )
  })
})

describe("group folding", () => {
  it("starts provider groups folded and every other group open", () => {
    const tasks = {
      id: "group:package:~/tasks",
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
    expect(authorityTitle("~")).toBe("Yours")
    expect(authorityTitle("ada.example.com")).toBe("ada.example.com")
    expect(authorityTitle("providers.substrate.reamde.dev")).toBe(
      "From providers"
    )
    expect(authorityTitle("substrate.reamde.dev")).toBe("Substrate")
  })

  it("names a package by its word, and a provider by its name", () => {
    expect(packageTitle("~", "tasks")).toBe("Tasks package")
    expect(packageTitle("substrate.reamde.dev", "llm")).toBe("LLM package")
    expect(packageTitle("providers.substrate.reamde.dev", "google")).toBe(
      "Google"
    )
  })
})

describe("collectionSource", () => {
  it("says where a kind comes from: its package, a provider or the substrate", () => {
    expect(collectionSource("~/tasks/task")).toBe("Tasks")
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
