import { describe, expect, it, beforeEach } from "vitest"
import { seedActors } from "@/lib/actors.test-fixtures"

import { providerInfo } from "@/lib/actor-identity"
import { originOfActor, originOfKind, originWords } from "@/lib/origin"

beforeEach(() => seedActors())

describe("originWords", () => {
  it("says every origin in the one set of words", () => {
    expect(originWords({ kind: "yours" })).toBe("Yours")
    expect(
      originWords({ kind: "provider", provider: providerInfo("google") })
    ).toBe("From Google")
    expect(originWords({ kind: "core" })).toBe("Built into substrate")
    expect(originWords({ kind: "other", authority: "example.com" })).toBe(
      "From example.com"
    )
    expect(originWords(originOfActor("act-agent-notes-notekeeper"))).toMatch(
      /^Made by /
    )
  })

  it("names the origin alone in a chip", () => {
    expect(originWords({ kind: "yours" }, true)).toBe("You")
    expect(
      originWords({ kind: "provider", provider: providerInfo("google") }, true)
    ).toBe("Google")
  })
})

describe("originOfActor", () => {
  it("reads the person, a provider's function and the engine", () => {
    expect(originOfActor("act-person").kind).toBe("yours")
    const google = originOfActor("act-fn-google-synccontacts")
    expect(google.kind).toBe("provider")
    expect(originOfActor("act-system").kind).toBe("core")
  })
})

describe("originOfKind", () => {
  it("reads a provider's copy, substrate's machinery, and yours", () => {
    expect(
      originOfKind("providers.substrate.reamde.dev/google/contact").kind
    ).toBe("provider")
    expect(originOfKind("substrate.reamde.dev/core/kind").kind).toBe("core")
    expect(originOfKind("substrate.reamde.dev/core/contact").kind).toBe("yours")
    expect(originOfKind("ada.localhost/tasks/task").kind).toBe("yours")
  })
})
