/** The suggested-mapping fold (decision record 0049): what a row says a
 * mapping is doing, and what it tells the reader to do about it.
 *
 * The state is the MAPPING RECORD's, never the provider's. `ready` is the one
 * that pays for this whole surface: the provider is installed, the mapping
 * fits it, and nothing is projecting until the sample is imported AGAIN, which
 * is a replacement rather than a merge. */

import { describe, expect, it } from "vitest"

import type { CatalogItem } from "@/lib/api/catalog"
import type { SuggestedMappingState } from "@/lib/api/types"
import {
  readySuggestedMappings,
  REIMPORT_WARNING,
  suggestedMappingHint,
  suggestedMappingsOf,
  type SuggestedMappingRow,
} from "./bundles"

/** One suggested-mapping row per (state, provider), as the fold builds them
 * off a catalog entry the server served. */
function rows(
  entries: [SuggestedMappingState, string][]
): SuggestedMappingRow[] {
  const catalog = {
    id: "samples.substrate.reamde.dev/people",
    suggestedMappings: entries.map(([state, pkg], i) => ({
      id: `ada.example.com/people/m${i}`,
      from: `${pkg}/user`,
      to: "ada.example.com/people/person",
      package: pkg,
      state,
      problems:
        state === "blocked"
          ? [`${pkg}/user declares no property "person"`]
          : undefined,
    })),
  } as CatalogItem
  return suggestedMappingsOf({
    id: "ada.example.com/people",
    name: "people",
    authority: "ada.example.com",
    package: "people",
    installed: true,
    requires: [],
    catalog,
  })
}

const github = "providers.substrate.reamde.dev/github"
const linear = "providers.substrate.reamde.dev/linear"

describe("suggestedMappingsOf", () => {
  it("carries the state through, and lands only in `landed`", () => {
    const got = rows([
      ["landed", github],
      ["ready", linear],
    ])
    expect(got.map((r) => r.state)).toEqual(["landed", "ready"])
    expect(got.map((r) => r.landed)).toEqual([true, false])
    // `ready` is NOT running: the provider is here, the declaration is not.
    expect(got[1].title).toContain("import people again")
    expect(got[1].title).toContain(REIMPORT_WARNING)
  })

  it("names the label from both kinds' own names", () => {
    expect(rows([["landed", github]])[0].label).toBe("user → person")
  })

  it("says what to do in each state's own words", () => {
    expect(rows([["landed", github]])[0].title).toContain("landed.")
    expect(rows([["waiting", linear]])[0].title).toContain("install linear")
    const blocked = rows([["blocked", linear]])[0]
    expect(blocked.title).toContain("upgrade linear first")
    // The loader's own words ride along, so "blocked" is never a dead end.
    expect(blocked.title).toContain('declares no property "person"')
  })
})

describe("readySuggestedMappings", () => {
  it("is the set a re-import would land, and nothing else", () => {
    const got = readySuggestedMappings(
      rows([
        ["landed", github],
        ["ready", linear],
        ["waiting", github],
        ["blocked", linear],
      ])
    )
    expect(got.map((r) => r.state)).toEqual(["ready"])
  })
})

describe("suggestedMappingHint", () => {
  it("says nothing when every mapping landed", () => {
    expect(suggestedMappingHint(rows([["landed", github]]))).toBe("")
  })

  it("asks for the import alone when the mapping is ready", () => {
    expect(suggestedMappingHint(rows([["ready", linear]]))).toBe(
      `To enable this mapping, import people again. ${REIMPORT_WARNING}`
    )
  })

  it("names the provider to install first, then the import", () => {
    expect(suggestedMappingHint(rows([["waiting", linear]]))).toBe(
      `To enable this mapping, install linear, then import people again. ${REIMPORT_WARNING}`
    )
  })

  it("asks for an upgrade where the provider is too old", () => {
    expect(suggestedMappingHint(rows([["blocked", linear]]))).toBe(
      `To enable this mapping, upgrade linear, then import people again. ${REIMPORT_WARNING}`
    )
  })

  it("counts several and names each provider once, install before upgrade", () => {
    expect(
      suggestedMappingHint(
        rows([
          ["waiting", linear],
          ["waiting", linear],
          ["blocked", github],
          ["landed", github],
        ])
      )
    ).toBe(
      `To enable these 3 mappings, install linear, then upgrade github, then import people again. ${REIMPORT_WARNING}`
    )
  })

  it("does not warn about a replacement for a sample nobody has imported", () => {
    const hint = suggestedMappingHint(rows([["waiting", linear]]), false)
    expect(hint).toBe(
      "To enable this mapping, install linear, then import people."
    )
    expect(hint).not.toContain(REIMPORT_WARNING)
  })
})
