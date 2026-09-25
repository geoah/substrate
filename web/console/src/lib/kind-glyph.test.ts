import { describe, expect, it } from "vitest"

import { KIND_HUES, hashHue, kindGlyph } from "./kind-glyph"

describe("kindGlyph", () => {
  it.each([
    ["samples.substrate.reamde.dev/tasks/task", "circle-check", "blue"],
    ["samples.substrate.reamde.dev/tasks/project", "folder", "orange"],
    ["samples.substrate.reamde.dev/people/person", "user", "teal"],
    ["providers.substrate.reamde.dev/google/contact", "at-sign", "teal"],
    ["samples.substrate.reamde.dev/people/organization", "building", "brown"],
    ["samples.substrate.reamde.dev/people/team", "users", "green"],
    ["samples.substrate.reamde.dev/calendar/calendarevent", "calendar", "red"],
    [
      "samples.substrate.reamde.dev/calendar/calendareventseries",
      "repeat",
      "gray",
    ],
    ["providers.substrate.reamde.dev/google/gmailthread", "mail", "red"],
    ["samples.substrate.reamde.dev/messaging/emailthread", "mail", "purple"],
    [
      "samples.substrate.reamde.dev/messaging/conversationmessage",
      "message-square",
      "purple",
    ],
    ["providers.substrate.reamde.dev/google/drivefile", "file-text", "yellow"],
    ["providers.substrate.reamde.dev/google/gmaillabel", "tag", "gray"],
    ["providers.substrate.reamde.dev/google/calendarsync", "settings", "gray"],
    ["samples.substrate.reamde.dev/tasks/tasklog", "history", "gray"],
  ])("%s → %s / %s", (reference, icon, hue) => {
    const glyph = kindGlyph(reference)
    expect(glyph.iconName).toBe(icon)
    expect(glyph.hue).toBe(hue)
  })

  it("falls back to a box with a hue hashed from the full reference", () => {
    const a = kindGlyph("example.com/widgets/frobnicator")
    expect(a.iconName).toBe("box")
    expect(KIND_HUES).toContain(a.hue)
    expect(kindGlyph("example.com/widgets/frobnicator").hue).toBe(a.hue)
    expect(hashHue("example.com/widgets/frobnicator")).toBe(a.hue)
  })

  it("spreads unknown kinds across the hues", () => {
    const hues = new Set(
      Array.from({ length: 40 }, (_, i) => hashHue(`example.com/p/k${i}`))
    )
    expect(hues.size).toBeGreaterThan(5)
  })

  it("reads a registry entry by its reference", () => {
    expect(
      kindGlyph({
        identity: "example.com/recipes/recipe",
        name: "recipe",
        authority: "example.com",
        package: "recipes",
        version: 1,
        source: "installed",
        description: "",
        definition: {},
      })
    ).toMatchObject({ iconName: "book-open", hue: "green" })
  })
})
