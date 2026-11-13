import { beforeEach, describe, expect, it } from "vitest"

import {
  actorIdentity,
  agentIdentity,
  functionIdentity,
  providerInfo,
  providerOfKind,
} from "./actor-identity"
import { ACTORS, seedActors } from "./actors.test-fixtures"

beforeEach(() => seedActors())

describe("actorIdentity", () => {
  it("names the person You", () => {
    expect(actorIdentity(ACTORS.person.id)).toMatchObject({
      actor: ACTORS.person.id,
      cls: "you",
      name: "You",
    })
  })

  it("names an app by its publisher, never as You", () => {
    expect(actorIdentity(ACTORS.notesApp.id)).toMatchObject({
      cls: "app",
      name: "notes.example.org",
    })
  })

  it("names an app on this device as one, never by a host", () => {
    expect(actorIdentity(ACTORS.localApp.id)).toMatchObject({
      cls: "app",
      name: "An app on this device",
    })
  })

  it("names the engine", () => {
    expect(actorIdentity(ACTORS.system.id)).toMatchObject({
      cls: "engine",
      name: "Substrate",
    })
  })

  it("names an agent by its own name and links its declaration", () => {
    expect(actorIdentity(ACTORS.substrateAgent.id)).toMatchObject({
      cls: "agent",
      name: "Substrate",
      record: {
        kind: "substrate.reamde.dev/core/agent",
        id: "ada.localhost/llm/substrate",
      },
    })
  })

  it.each([
    [ACTORS.googleContactsSync.id, "Google Contacts sync"],
    [ACTORS.linearSync.id, "Linear sync"],
    [ACTORS.beeperMessagesSync.id, "Beeper Messages sync"],
    [ACTORS.githubSync.id, "GitHub sync"],
  ])("names the provider function of %s as %s", (actor, name) => {
    const identity = actorIdentity(actor)
    expect(identity.cls).toBe("function")
    expect(identity.name).toBe(name)
    expect(identity.provider?.letter).toBe(name[0])
  })

  it("names a repository's own function by its name", () => {
    expect(actorIdentity(ACTORS.notesSave.id)).toMatchObject({
      cls: "function",
      name: "save",
      provider: undefined,
    })
  })

  it("names a provider's package by the provider", () => {
    expect(actorIdentity(ACTORS.googlePackage.id)).toMatchObject({
      cls: "bundle",
      name: "Google",
    })
  })

  it("names any other package by its display name, and core as the substrate", () => {
    expect(actorIdentity(ACTORS.notesPackage.id).name).toBe("Notes")
    expect(actorIdentity(ACTORS.readingListPackage.id).name).toBe(
      "Reading list"
    )
    expect(actorIdentity(ACTORS.corePackage.id)).toMatchObject({
      cls: "bundle",
      name: "Substrate",
    })
  })

  it("keeps an id it has not read, and never calls it You", () => {
    const unknown = actorIdentity("act-unread")
    expect(unknown.actor).toBe("act-unread")
    expect(unknown.cls).not.toBe("you")
  })

  it("marks an actor whose code is gone", () => {
    seedActors({ ...ACTORS.notesSave, id: "act-gone", disabled: true })
    expect(actorIdentity("act-gone").disabled).toBe(true)
  })
})

describe("an identity from a callable's own identity", () => {
  it("names an agent and a function without an actor record", () => {
    expect(agentIdentity("ada.localhost/llm/substrateEditor").name).toBe(
      "Substrate editor"
    )
    expect(
      functionIdentity("providers.substrate.reamde.dev/google/synccontacts")
        .name
    ).toBe("Google Contacts sync")
  })
})

describe("providers", () => {
  it("spells a provider's name and letter", () => {
    expect(providerInfo("github")).toMatchObject({
      name: "GitHub",
      letter: "G",
    })
    expect(providerInfo("acme")).toMatchObject({ name: "Acme", letter: "A" })
    expect(providerInfo("firecrawl")).toMatchObject({
      name: "Firecrawl",
      letter: "F",
      color: "#FA5D19",
    })
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
