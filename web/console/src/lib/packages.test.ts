import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { originWords } from "@/lib/origin"

import { kindOrigin, packageAgent, packageRows } from "./packages"

function row(id: string, declaredBy?: string): SubstrateRecord {
  return {
    kind: "substrate.reamde.dev/core/package",
    id,
    properties: declaredBy ? { declaredBy } : {},
  } as unknown as SubstrateRecord
}

const rows = packageRows({
  records: [
    row("ada.example.com/recipes", "agent:ada.example.com:llm:notekeeper"),
    row("ada.example.com/tasks", "console"),
    row("ada.example.com/people"),
    row(
      "providers.substrate.reamde.dev/google",
      "bundle:providers.substrate.reamde.dev:google"
    ),
  ],
})

describe("packages", () => {
  it("reads who declared each package, absent where nothing stamped it", () => {
    expect(rows.get("ada.example.com/recipes")?.declaredBy).toBe(
      "agent:ada.example.com:llm:notekeeper"
    )
    expect(rows.get("ada.example.com/people")).toEqual({
      id: "ada.example.com/people",
    })
  })

  it("names the agent only when an agent declared the package", () => {
    expect(packageAgent(rows, "ada.example.com/recipes")).toBe(
      "agent:ada.example.com:llm:notekeeper"
    )
    expect(packageAgent(rows, "ada.example.com/tasks")).toBeUndefined()
    expect(
      packageAgent(rows, "providers.substrate.reamde.dev/google")
    ).toBeUndefined()
    expect(packageAgent(undefined, "ada.example.com/recipes")).toBeUndefined()
  })

  it("says an agent's collection is made by that agent", () => {
    expect(
      originWords(kindOrigin("ada.example.com/recipes/recipe", rows))
    ).toBe("Made by Notekeeper")
    expect(originWords(kindOrigin("ada.example.com/tasks/task", rows))).toBe(
      "Yours"
    )
    expect(
      originWords(
        kindOrigin("providers.substrate.reamde.dev/google/contact", rows)
      )
    ).toBe("From Google")
  })
})
