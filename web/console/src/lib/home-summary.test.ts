import { describe, expect, it } from "vitest"

import type { BundleStatus, KindInfo } from "@/lib/api/types"
import { collectionGroups } from "@/lib/collections"
import {
  dataSummary,
  homeSections,
  namesSummary,
  providersSummary,
  providerState,
} from "./home-summary"

function status(over: Partial<BundleStatus>): BundleStatus {
  return {
    id: "providers.substrate.reamde.dev/google",
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 4,
    kinds: 14,
    liveRecords: 0,
    ...over,
  }
}

describe("providerState", () => {
  it("says where a provider stands", () => {
    expect(
      providerState(
        status({ setup: [{ code: "missing", message: "no config" }] })
      )
    ).toBe("Google not set up yet")
    expect(providerState(status({ accounts: 1 }))).toBe("Google connected")
    expect(providerState(status({}))).toBe("Google needs an account")
    expect(providerState(status({ enabled: false }))).toBe("Google turned off")
  })
})

describe("providersSummary", () => {
  it("counts providers and leaves samples out", () => {
    const summary = providersSummary([
      status({ accounts: 1 }),
      status({
        id: "ada.example.com/tasks",
        authority: "ada.example.com",
        package: "tasks",
      }),
    ])
    expect(summary).toEqual({ big: "1 added", sub: "Google connected" })
  })

  it("invites a first provider when there is none", () => {
    expect(providersSummary([]).big).toBe("None added")
  })
})

describe("dataSummary and namesSummary", () => {
  it("reads counts in words", () => {
    expect(dataSummary(12, 2)).toEqual({
      big: "12 collections",
      sub: "yours and from 2 providers",
    })
    expect(dataSummary(1, 0)).toEqual({ big: "1 collection", sub: "all yours" })
    expect(namesSummary(["a", "b", "c"])).toBe("a, b and c")
    expect(namesSummary(["a", "b", "c", "d", "e"])).toBe("a, b and 3 more")
  })
})

function kind(identity: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return { identity, authority, package: pkg, name } as unknown as KindInfo
}

describe("homeSections", () => {
  const HOME = "ada.example.com"
  const yours = ["task", "project", "person", "note"].map((n) =>
    kind(`${HOME}/things/${n}`)
  )
  const google = ["calendarevent", "contact", "gmailthread"].map((n) =>
    kind(`providers.substrate.reamde.dev/google/${n}`)
  )

  it("groups yours first, then each provider under its own heading", () => {
    const sections = homeSections(
      collectionGroups([...yours, ...google], HOME),
      () => true
    )
    expect(sections.map((s) => s.label)).toEqual(["Your data", "From Google"])
    expect(sections[1].provider?.key).toBe("google")
    expect(sections[1].kinds).toHaveLength(3)
  })

  it("leads with the collections that hold something", () => {
    const sections = homeSections(
      collectionGroups(yours, HOME),
      (k) => k.name === "task"
    )
    expect(sections[0].kinds[0].name).toBe("task")
  })

  it("keeps to the cap, yours taking their share first", () => {
    const sections = homeSections(
      collectionGroups([...yours, ...google], HOME),
      () => true,
      5,
      4
    )
    expect(sections.map((s) => s.kinds.length)).toEqual([4, 1])
  })
})
