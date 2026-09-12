import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  columnIdOf,
  defaultHiddenColumns,
  propertyColumnId,
  sortPropertyOf,
} from "./kind-browse-columns"

describe("column id ↔ wire property mapping", () => {
  it("namespaces declared properties apart from system columns", () => {
    // pullrequest declares its own `updatedAt`; the system column must
    // not collide with it (live finding, 2026-08-05).
    expect(propertyColumnId("updatedAt")).toBe("prop:updatedAt")
    expect(sortPropertyOf("prop:number")).toBe("number")
    expect(sortPropertyOf("updatedAt")).toBe("updatedAt")
  })

  it("routes a wire sort property back to the owning column", () => {
    expect(columnIdOf("updatedAt")).toBe("updatedAt")
    expect(columnIdOf("title")).toBe("title")
    expect(columnIdOf("prominence")).toBe("prop:prominence")
  })
})

function kind(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "builtin",
    description: "",
    definition: { properties: {} },
  }
}

describe("the columns a kind opens without", () => {
  // A function's declaration is mostly machinery, and three of its properties
  // are now IN its title (it titles itself with its full reference), so the
  // browse table opened nine columns wide saying the same thing twice.
  it("hides a core function's machinery and keeps what a reader reads", () => {
    const hidden = defaultHiddenColumns(
      kind("substrate.reamde.dev/core/function")
    )
    for (const name of [
      "authority",
      "package",
      "version",
      "source",
      "arguments",
      "effect",
      "confirmation",
    ]) {
      expect(hidden).toContain(propertyColumnId(name))
    }
    for (const name of ["description", "runtime", "timeout"]) {
      expect(hidden).not.toContain(propertyColumnId(name))
    }
  })

  // Hiding a property of a kind this console did not ship would be guessing at
  // somebody else's vocabulary, so every other kind opens with all of them.
  it("hides nothing on a kind it does not ship", () => {
    expect(defaultHiddenColumns(kind("ada.example.com/tasks/task"))).toEqual([])
  })
})
