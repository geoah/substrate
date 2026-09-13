/** The package rows as the floor check reads them: the identity is the id,
 * the version is the `version` PROPERTY the engine maintains, and an unread
 * page stays unread rather than becoming "no packages". */

import { describe, expect, it } from "vitest"

import { installedVersions, packagesQueryOptions } from "./apps"
import type { SubstrateRecord } from "./types"

function pkg(id: string, properties: Record<string, unknown>): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/package",
    properties,
    labels: {},
    version: 7,
    createdAt: "",
    updatedAt: "",
  }
}

describe("installedVersions", () => {
  it("maps identity to the closure's version, not the row's", () => {
    expect(
      installedVersions([
        pkg("ada.example.com/tasks", { version: 3 }),
        pkg("ada.example.com/people", { version: 1 }),
      ])
    ).toEqual({ "ada.example.com/tasks": 3, "ada.example.com/people": 1 })
  })
  it("skips a row without a numeric version and keeps unread unread", () => {
    expect(installedVersions([pkg("x.example.com/y", {})])).toEqual({})
    expect(installedVersions(undefined)).toBeUndefined()
  })
})

describe("packagesQueryOptions", () => {
  it("reads the core package collection in one page", () => {
    expect(packagesQueryOptions().queryKey).toEqual([
      "records",
      "substrate.reamde.dev",
      "core",
      "package",
      { first: 200, after: null, filter: null, orderBy: null },
    ])
  })
})
