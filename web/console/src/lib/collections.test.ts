import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  collectionGroups,
  collectionSource,
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
  kind("substrate.reamde.dev/core/kind"),
]

describe("collectionGroups", () => {
  const groups = collectionGroups(KINDS, "ada.example.com")

  it("reads yours, then each provider by name, then the substrate", () => {
    expect(groups.map((g) => g.label)).toEqual([
      "Your data",
      "From Google",
      "From Linear",
      "Substrate",
    ])
  })

  it("keeps every non-provider, non-core authority in Your data, home first", () => {
    const yours = groups[0]
    expect(yours.authorities.map((a) => a.authority)).toEqual([
      "ada.example.com",
      "other.example.com",
    ])
    expect(yours.primary.map((k) => k.identity)).toEqual([
      "other.example.com/notes/note",
      "ada.example.com/people/person",
      "ada.example.com/tasks/task",
    ])
    expect(yours.hidden.map((k) => k.identity)).toEqual([
      "ada.example.com/tasks/tasklog",
    ])
  })

  it("lists only primary kinds as primary and core as hidden machinery", () => {
    const google = groups[1]
    expect(google.provider?.name).toBe("Google")
    expect(google.primary.map((k) => k.identity)).toEqual([
      "providers.substrate.reamde.dev/google/contact",
    ])
    expect(google.hidden).toHaveLength(3)
    expect(groups[3].primary).toEqual([])
  })

  it("names a few hidden kinds for the note under a group", () => {
    expect(hiddenExamples(groups[1])).toBe("email addresses and Gmail labels")
  })
})

describe("group folding", () => {
  it("starts provider groups folded and every other group open", () => {
    const yours = { id: "group:yours", type: "yours" as const }
    const google = { id: "group:provider:google", type: "provider" as const }
    expect(isGroupOpen(yours, [])).toBe(true)
    expect(isGroupOpen(google, [])).toBe(false)
    expect(isGroupOpen(yours, [groupToggleKey(yours)])).toBe(false)
    expect(isGroupOpen(google, [groupToggleKey(google)])).toBe(true)
  })
})

describe("collectionSource", () => {
  it("says where a kind comes from", () => {
    expect(collectionSource("ada.example.com/tasks/task")).toBe("Your data")
    expect(
      collectionSource("providers.substrate.reamde.dev/google/contact")
    ).toBe("From Google")
    expect(collectionSource("substrate.reamde.dev/core/kind")).toBe("Substrate")
  })
})
