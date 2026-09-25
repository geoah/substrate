/** The shell's crumbs: every page the router serves reads as where it sits,
 * and a parent crumb links back to a route that exists. */

import { describe, expect, it } from "vitest"

import { crumbsFor } from "./app-shell"

describe("crumbsFor", () => {
  it("reads the Connections list and one connection under it", () => {
    expect(crumbsFor("/connections")).toEqual([{ label: "Connections" }])
    expect(
      crumbsFor("/connections/providers.substrate.reamde.dev/google/account/a1")
    ).toEqual([
      { label: "Connections", to: "/connections" },
      { label: "a1", mono: true },
    ])
  })

  it("reads Settings and one bundle's settings by its whole bundle id", () => {
    expect(crumbsFor("/settings")).toEqual([{ label: "Settings" }])
    expect(
      crumbsFor("/settings/providers.substrate.reamde.dev%2Fgoogle")
    ).toEqual([
      { label: "Settings", to: "/settings" },
      { label: "providers.substrate.reamde.dev/google", mono: true },
    ])
  })

  it("reads Agents and one agent under it", () => {
    expect(crumbsFor("/agents")).toEqual([{ label: "Agents" }])
    expect(crumbsFor("/agents/helper")).toEqual([
      { label: "Agents", to: "/agents" },
      { label: "helper", mono: true },
    ])
  })

  it("reads Account and its Tokens page", () => {
    expect(crumbsFor("/account")).toEqual([{ label: "Account" }])
    expect(crumbsFor("/account/tokens")).toEqual([
      { label: "Account", to: "/account" },
      { label: "Tokens" },
    ])
  })

  // Connections moved out of the registry; no crumb may link back to the
  // route that no longer exists.
  it("never links to the retired /registry/connections route", () => {
    for (const path of [
      "/connections",
      "/connections/a.example.com/p/n/x",
      "/registry",
      "/registry/providers.substrate.reamde.dev%2Fgoogle",
    ]) {
      for (const crumb of crumbsFor(path)) {
        expect(crumb.to ?? "").not.toContain("/registry/connections")
      }
    }
  })

  it("keeps the registry and data crumbs", () => {
    expect(crumbsFor("/registry")).toEqual([{ label: "Registry" }])
    expect(crumbsFor("/data/a.example.com/tasks/task/t1")).toEqual([
      { label: "Data" },
      { label: "a.example.com", to: "/data/a.example.com", mono: true },
      { label: "tasks", to: "/data/a.example.com/tasks", mono: true },
      { label: "task", to: "/data/a.example.com/tasks/task", mono: true },
      { label: "t1", mono: true },
    ])
  })
})
