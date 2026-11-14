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
    row("~/recipes", "act-agent-llm-notekeeper"),
    row("~/tasks", "act-person"),
    row("~/people"),
    row("~/training", "act-app-workouts-example-org"),
    row("providers.substrate.reamde.dev/google", "act-pkg-google"),
  ],
})

describe("packages", () => {
  it("reads who declared each package, absent where nothing stamped it", () => {
    expect(rows.get("~/recipes")?.declaredBy).toBe("act-agent-llm-notekeeper")
    expect(rows.get("~/people")).toEqual({
      id: "~/people",
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
        row("~/tasks", "act-person"),
      ],
    })
    const core = labelled.get("substrate.reamde.dev/core")!
    expect(core.stability).toBe("stable")
    expect(packageStability(core)).toBe("stable")
    const tasks = labelled.get("~/tasks")!
    expect(tasks.stability).toBeUndefined()
    expect(packageStability(tasks)).toBe("experimental")
  })

  it("reads the function interface a package names, and 1 where it names none", () => {
    const named = packageRows({
      records: [
        {
          kind: "substrate.reamde.dev/core/package",
          id: "~/garden",
          properties: { interface: 1 },
        } as unknown as SubstrateRecord,
        row("~/tasks", "act-person"),
      ],
    })
    const garden = named.get("~/garden")!
    expect(garden.functionInterface).toBe(1)
    expect(packageInterface(garden)).toBe(1)
    const tasks = named.get("~/tasks")!
    expect(tasks.functionInterface).toBeUndefined()
    expect(packageInterface(tasks)).toBe(1)
  })

  it("names the agent only when an agent declared the package", () => {
    expect(packageAgent(rows, "~/recipes")).toBe("act-agent-llm-notekeeper")
    expect(packageAgent(rows, "~/tasks")).toBeUndefined()
    expect(
      packageAgent(rows, "providers.substrate.reamde.dev/google")
    ).toBeUndefined()
    expect(packageAgent(undefined, "~/recipes")).toBeUndefined()
  })

  it("says an agent's collection is made by that agent", () => {
    expect(originWords(kindOrigin("~/recipes/recipe", rows))).toBe(
      "Made by Notekeeper"
    )
    expect(originWords(kindOrigin("~/tasks/task", rows))).toBe("Yours")
    expect(
      originWords(
        kindOrigin("providers.substrate.reamde.dev/google/contact", rows)
      )
    ).toBe("From Google")
  })

  // Decision 0214: an app that declared a package is its declarer and no
  // more; its collections read as the owner's, and nothing is locked.
  it("reads an app's package as the owner's own", () => {
    expect(originWords(kindOrigin("~/training/set", rows))).toBe("Yours")
    expect(packageAgent(rows, "~/training")).toBeUndefined()
  })
})
