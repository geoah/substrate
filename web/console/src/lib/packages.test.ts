import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { originWords } from "@/lib/origin"

import { readOnlyReason } from "@/lib/provenance"

import {
  awaitsPackageRows,
  declarationApp,
  kindOrigin,
  packageAgent,
  packageApp,
  packageRows,
} from "./packages"

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
    row("ada.example.com/training", "app:workouts.example.org"),
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

  // Decision 0205: a package an app declared under its declare: scope is
  // the app's, and its declarations are read only on every edit door.
  it("names the app that manages a package, and only an app", () => {
    expect(packageApp(rows, "ada.example.com/training")).toBe(
      "app:workouts.example.org"
    )
    expect(packageApp(rows, "ada.example.com/recipes")).toBeUndefined()
    expect(packageApp(rows, "ada.example.com/tasks")).toBeUndefined()
    expect(packageApp(undefined, "ada.example.com/training")).toBeUndefined()
    expect(originWords(kindOrigin("ada.example.com/training/set", rows))).toBe(
      "Managed by workouts.example.org"
    )
  })

  it("locks an app's package, bundle and kinds, and none of their records", () => {
    const decl = (kind: string, id: string, properties = {}) =>
      ({
        kind: `substrate.reamde.dev/core/${kind}`,
        id,
        properties,
      }) as unknown as SubstrateRecord
    const app = "app:workouts.example.org"
    const kind = decl("kind", "ada.example.com/training/set")
    const bundle = decl("bundle", "ada.example.com/training")
    const pkg = decl("package", "ada.example.com/training", {
      declaredBy: app,
    })
    for (const record of [kind, bundle, pkg]) {
      expect(declarationApp(record, rows)).toBe(app)
      expect(readOnlyReason(record, rows)).toBe("app")
    }
    // The package row says it itself, without the rows.
    expect(declarationApp(pkg, undefined)).toBe(app)
    const set = {
      kind: "ada.example.com/training/set",
      id: "s1",
      properties: { reps: 8 },
    } as unknown as SubstrateRecord
    expect(readOnlyReason(set, rows)).toBeUndefined()
    expect(
      readOnlyReason(decl("kind", "ada.example.com/tasks/task"), rows)
    ).toBeUndefined()
    expect(
      declarationApp(decl("function", "ada.example.com/training/x"), rows)
    ).toBeUndefined()
  })
})

// The edit doors of a bundle or a kind wait for the package rows, which say
// whether an app manages it; a package row and a record never wait.
describe("awaitsPackageRows", () => {
  const of = (kind: string) => ({ kind })
  it("waits only for a bundle or a kind, only while the rows load", () => {
    expect(awaitsPackageRows(of("substrate.reamde.dev/core/kind"), true)).toBe(
      true
    )
    expect(
      awaitsPackageRows(of("substrate.reamde.dev/core/bundle"), true)
    ).toBe(true)
    expect(
      awaitsPackageRows(of("substrate.reamde.dev/core/package"), true)
    ).toBe(false)
    expect(awaitsPackageRows(of("ada.example.com/training/set"), true)).toBe(
      false
    )
    expect(awaitsPackageRows(of("substrate.reamde.dev/core/kind"), false)).toBe(
      false
    )
  })

  it("reads the client id beside the app's actor", () => {
    const withClient = packageRows({
      records: [
        {
          kind: "substrate.reamde.dev/core/package",
          id: "ada.example.com/training",
          properties: {
            declaredBy: "app:workouts.example.org",
            declaredByClient: "https://workouts.example.org/client.json",
          },
        } as unknown as SubstrateRecord,
      ],
    })
    expect(withClient.get("ada.example.com/training")?.declaredByClient).toBe(
      "https://workouts.example.org/client.json"
    )
  })
})
