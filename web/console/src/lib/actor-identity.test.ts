import { describe, expect, it } from "vitest"

import { actorIdentity, providerInfo, providerOfKind } from "./actor-identity"

describe("actorIdentity", () => {
  it("names a request-asserted actor You, with the door it came through", () => {
    expect(actorIdentity("console")).toMatchObject({
      cls: "you",
      name: "You",
      via: "this console",
    })
    expect(actorIdentity("substratectl").via).toBe("the command line")
    expect(actorIdentity("nightly-script")).toMatchObject({
      cls: "you",
      via: "nightly-script",
    })
  })

  it("names the engine", () => {
    expect(actorIdentity("substrate")).toMatchObject({
      cls: "engine",
      name: "Substrate",
    })
  })

  it("names an agent by its own name and links its declaration", () => {
    expect(actorIdentity("agent:ada.localhost:llm:substrate")).toMatchObject({
      cls: "agent",
      name: "Substrate",
      record: {
        kind: "substrate.reamde.dev/core/agent",
        id: "ada.localhost/llm/substrate",
      },
    })
  })

  it.each([
    [
      "function:providers.substrate.reamde.dev:google:synccontacts",
      "Google Contacts sync",
    ],
    [
      "function:providers.substrate.reamde.dev:linear:linearsync",
      "Linear sync",
    ],
    [
      "function:providers.substrate.reamde.dev:beeper:messagessync",
      "Beeper Messages sync",
    ],
    [
      "function:providers.substrate.reamde.dev:github:githubsync",
      "GitHub sync",
    ],
  ])("names a provider function %s as %s", (actor, name) => {
    const identity = actorIdentity(actor)
    expect(identity.cls).toBe("function")
    expect(identity.name).toBe(name)
    expect(identity.provider?.letter).toBe(name[0])
  })

  it("names a repository's own function by its name", () => {
    expect(actorIdentity("function:ada.localhost:notes:save")).toMatchObject({
      cls: "function",
      name: "save",
      provider: undefined,
    })
  })

  it("names a provider bundle by the provider", () => {
    expect(
      actorIdentity("bundle:providers.substrate.reamde.dev:google")
    ).toMatchObject({ cls: "bundle", name: "Google" })
  })

  it("names any other bundle by its package's display name", () => {
    expect(actorIdentity("bundle:ada.localhost:notes").name).toBe("Notes")
    expect(actorIdentity("bundle:ada.localhost:readinglist").name).toBe(
      "Reading list"
    )
  })
})

it("names an authority-shaped actor as the bundle behind it, never You", () => {
  expect(actorIdentity("providers.substrate.reamde.dev/github")).toMatchObject({
    cls: "bundle",
    name: "GitHub",
  })
  expect(actorIdentity("samples.substrate.reamde.dev")).toMatchObject({
    cls: "bundle",
    name: "samples.substrate.reamde.dev",
  })
})

describe("providers", () => {
  it("spells a provider's name and letter", () => {
    expect(providerInfo("github")).toMatchObject({
      name: "GitHub",
      letter: "G",
    })
    expect(providerInfo("acme")).toMatchObject({ name: "Acme", letter: "A" })
  })

  it("finds the provider of a provider kind only", () => {
    expect(
      providerOfKind("providers.substrate.reamde.dev/google/contact")?.name
    ).toBe("Google")
    expect(providerOfKind("samples.substrate.reamde.dev/people/person")).toBe(
      undefined
    )
  })
})

describe("a seeded package's bundle actor", () => {
  it("reads bundle:core as the substrate, never as you", () => {
    const core = actorIdentity("bundle:core")
    expect(core.cls).toBe("bundle")
    expect(core.name).toBe("Substrate")
  })
})
