import { describe, expect, it } from "vitest"

import type { BundleStatus } from "@/lib/api/types"
import {
  dataSummary,
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
