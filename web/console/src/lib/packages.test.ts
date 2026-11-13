import { describe, expect, it, beforeEach } from "vitest"
import { seedActors } from "@/lib/actors.test-fixtures"

import type { SubstrateRecord } from "@/lib/api/types"
import { originWords } from "@/lib/origin"

import {
  kindOrigin,
  packageAgent,
  packageRows,
  packageInterface,
  packageStability,
} from "./packages"

beforeEach(() => seedActors())

function row(id: string, declaredBy?: string): SubstrateRecord {
  return {
    kind: "substrate.reamde.dev/core/package",
    id,
    properties: declaredBy
      ? { declaredBy: { ref: `substrate.reamde.dev/core/actor/${declaredBy}` } }
      : {},
  } as unknown as SubstrateRecord
}

const rows = packageRows({
  records: [
    row("ada.example.com/recipes", "act-agent-llm-notekeeper"),
    row("ada.example.com/tasks", "act-person"),
    row("ada.example.com/people"),
    row("ada.example.com/training", "act-app-workouts-example-org"),
    row("providers.substrate.reamde.dev/google", "act-pkg-google"),
  ],
})

describe("packages", () => {
  it("reads who declared each package, absent where nothing stamped it", () => {
    expect(rows.get("ada.example.com/recipes")?.declaredBy).toBe(
      "act-agent-llm-notekeeper"
    )
    expect(rows.get("ada.example.com/people")).toEqual({
      id: "ada.example.com/people",
    })
  })

  it("reads the stability a package states, and experimental where it states none", () => {
    const labelled = packageRows({
      records: [
        {
          kind: "substrate.reamde.dev/core/package",
          id: "substrate.reamde.dev/core",
          properties: { stability: "stable" },
        } as unknown as SubstrateRecord,
        row("ada.example.com/tasks", "act-person"),
      ],
    })
    const core = labelled.get("substrate.reamde.dev/core")!
    expect(core.stability).toBe("stable")
    expect(packageStability(core)).toBe("stable")
    const tasks = labelled.get("ada.example.com/tasks")!
    expect(tasks.stability).toBeUndefined()
    expect(packageStability(tasks)).toBe("experimental")
  })

  it("reads the function interface a package names, and 1 where it names none", () => {
    const named = packageRows({
      records: [
        {
          kind: "substrate.reamde.dev/core/package",
          id: "ada.example.com/garden",
          properties: { interface: 1 },
        } as unknown as SubstrateRecord,
        row("ada.example.com/tasks", "act-person"),
      ],
    })
    const garden = named.get("ada.example.com/garden")!
    expect(garden.functionInterface).toBe(1)
    expect(packageInterface(garden)).toBe(1)
    const tasks = named.get("ada.example.com/tasks")!
    expect(tasks.functionInterface).toBeUndefined()
    expect(packageInterface(tasks)).toBe(1)
  })

  it("names the agent only when an agent declared the package", () => {
    expect(packageAgent(rows, "ada.example.com/recipes")).toBe(
      "act-agent-llm-notekeeper"
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

  // Decision 0214: an app that declared a package is its declarer and no
  // more; its collections read as the owner's, and nothing is locked.
  it("reads an app's package as the owner's own", () => {
    expect(originWords(kindOrigin("ada.example.com/training/set", rows))).toBe(
      "Yours"
    )
    expect(packageAgent(rows, "ada.example.com/training")).toBeUndefined()
  })
})
