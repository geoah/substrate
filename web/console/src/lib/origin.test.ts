import { describe, expect, it } from "vitest"

import { providerInfo } from "@/lib/actor-identity"
import { originOfActor, originOfKind, originWords } from "@/lib/origin"

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
    expect(
      originWords(originOfActor("agent:ada.localhost:notes:notekeeper"))
    ).toMatch(/^Made by /)
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
    expect(originOfActor("console").kind).toBe("yours")
    const google = originOfActor(
      "function:providers.substrate.reamde.dev:google:synccontacts"
    )
    expect(google.kind).toBe("provider")
    expect(originOfActor("substrate").kind).toBe("core")
  })
})

describe("originOfKind", () => {
  it("reads a provider's copy, substrate's machinery, and yours", () => {
    expect(
      originOfKind("providers.substrate.reamde.dev/google/contact").kind
    ).toBe("provider")
    expect(originOfKind("substrate.reamde.dev/core/kind").kind).toBe("core")
    expect(originOfKind("ada.localhost/tasks/task").kind).toBe("yours")
  })
})
