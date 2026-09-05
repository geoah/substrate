/** What a DECLARATION kind's id is, and what it says. Two rules, and the one
 * exception that keeps them honest: an actor is named by a bare word, so its
 * authority is the only one somebody still has to type. */

import { describe, expect, it } from "vitest"

import {
  ACTOR_KIND,
  AGENT_KIND,
  AUTHORITY_KIND,
  BUNDLE_KIND,
  DECLARATION_KINDS,
  KIND_KIND,
  authorityIsDerived,
  declarationIdShape,
  derivedAuthority,
  derivedPackage,
  isDeclarationKind,
  packageIsDerived,
} from "./declarations"

describe("the declaration kinds", () => {
  it("is the ten the engine routes through admission", () => {
    expect(DECLARATION_KINDS).toHaveLength(10)
    expect(isDeclarationKind(KIND_KIND)).toBe(true)
    expect(isDeclarationKind(AGENT_KIND)).toBe(true)
    expect(isDeclarationKind("substrate.reamde.dev/core/llmprovider")).toBe(
      false
    )
    expect(isDeclarationKind("tasks.example.com/task")).toBe(false)
  })

  it("spells the id shape each kind is addressed by", () => {
    expect(declarationIdShape(KIND_KIND)).toBe("<authority>/<package>/<name>")
    expect(declarationIdShape(ACTOR_KIND)).toBe("<name>")
    expect(declarationIdShape(AUTHORITY_KIND)).toBe("<authority>")
    expect(declarationIdShape(BUNDLE_KIND)).toBe("<authority>/<package>")
  })
})

describe("the authority an id says", () => {
  it("is the label in front of the first slash", () => {
    expect(derivedAuthority(KIND_KIND, "tasks.example.com/tasks/task")).toBe(
      "tasks.example.com"
    )
    expect(derivedAuthority(AGENT_KIND, " crew.test.dev/crew/scout ")).toBe(
      "crew.test.dev"
    )
  })

  it("is the whole id where the id IS the label", () => {
    expect(derivedAuthority(AUTHORITY_KIND, "tasks.example.com")).toBe(
      "tasks.example.com"
    )
    expect(derivedAuthority(BUNDLE_KIND, "tasks.example.com/tasks")).toBe(
      "tasks.example.com"
    )
  })

  it("is nothing an ACTOR's id can say: a bare name names no authority", () => {
    expect(authorityIsDerived(ACTOR_KIND)).toBe(false)
    expect(derivedAuthority(ACTOR_KIND, "console")).toBeUndefined()
  })

  it("is nothing at all for an ordinary kind, or a blank id", () => {
    expect(authorityIsDerived("tasks.example.com/tasks/task")).toBe(false)
    expect(
      derivedAuthority("tasks.example.com/tasks/task", "t-1")
    ).toBeUndefined()
    expect(derivedAuthority(KIND_KIND, "  ")).toBeUndefined()
  })

  it("follows a half-typed id, because it is read as it is typed", () => {
    expect(derivedAuthority(KIND_KIND, "tasks.exa")).toBe("tasks.exa")
    expect(derivedAuthority(KIND_KIND, "tasks.example.com/")).toBe(
      "tasks.example.com"
    )
  })
})

describe("the package an id says", () => {
  it("is the id's second segment", () => {
    expect(derivedPackage(KIND_KIND, "tasks.example.com/tasks/task")).toBe(
      "tasks"
    )
    expect(derivedPackage(BUNDLE_KIND, " tasks.example.com/tasks ")).toBe(
      "tasks"
    )
  })

  it("is nothing an AUTHORITY's id can say: one label names no package", () => {
    expect(packageIsDerived(AUTHORITY_KIND)).toBe(false)
    expect(derivedPackage(AUTHORITY_KIND, "tasks.example.com")).toBeUndefined()
    expect(packageIsDerived(ACTOR_KIND)).toBe(false)
    expect(derivedPackage(ACTOR_KIND, "console")).toBeUndefined()
  })

  it("is nothing while the id has no second segment yet", () => {
    expect(derivedPackage(KIND_KIND, "tasks.example.com")).toBeUndefined()
    expect(derivedPackage(KIND_KIND, "tasks.example.com/")).toBeUndefined()
  })
})
