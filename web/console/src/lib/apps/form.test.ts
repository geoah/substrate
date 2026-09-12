/** The create sheet asks for exactly the prompt, hot columns included, and
 * requires what the declaration marks required plus the one heading
 * alternative the prompt carries. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { headingAlternatives, promptFields, requiredHeading } from "./form"

const person: KindInfo = {
  identity: "ada.example.com/people/person",
  name: "person",
  authority: "ada.example.com",
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    displayTemplate: "{displayName|name}",
    traits: ["temporal(point: dueAt)"],
    properties: {
      displayName: { type: "string" },
      name: { type: "string" },
      emails: { type: "string", repeated: true },
      org: { type: "reference", kind: "organization", required: true },
      login: { type: "string", writer: "connector" },
    },
  },
}

describe("promptFields", () => {
  it("reads the heading alternatives in order", () => {
    expect(headingAlternatives(person)).toEqual(["displayName", "name"])
    expect(requiredHeading(person, ["name", "emails"])).toBe("name")
    expect(requiredHeading(person, ["emails"])).toBeUndefined()
  })
  it("asks for the prompt only, requiring the heading and the declared", () => {
    const fields = promptFields(person, ["name", "emails", "org", "dueAt"])
    expect(fields.map((f) => [f.name, f.required])).toEqual([
      ["name", true],
      ["emails", false],
      ["org", true],
      ["dueAt", false],
    ])
  })
  it("never offers a property the owner may not write", () => {
    expect(promptFields(person, ["login", "nope"])).toEqual([])
  })
  it("resolves a bare reference pin inside the declaring package", () => {
    const organization: KindInfo = {
      ...person,
      identity: "ada.example.com/people/organization",
      name: "organization",
      definition: { properties: { name: { type: "string" } } },
    }
    const [org] = promptFields(person, ["org"], [person, organization])
    expect(org.spec.to).toBe("ada.example.com/people/organization")
    // Without the registry the pin stands as written.
    expect(promptFields(person, ["org"])[0].spec.to).toBe("organization")
  })
})
