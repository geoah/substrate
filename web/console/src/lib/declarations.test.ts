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
  isDeclarationKind,
} from "./declarations"

describe("the declaration kinds", () => {
  it("is the nine the engine routes through admission", () => {
    expect(DECLARATION_KINDS).toHaveLength(9)
    expect(isDeclarationKind(KIND_KIND)).toBe(true)
    expect(isDeclarationKind(AGENT_KIND)).toBe(true)
    expect(isDeclarationKind("core.substrate.reamde.dev/llmprovider")).toBe(
      false
    )
    expect(isDeclarationKind("tasks.example.com/task")).toBe(false)
  })

  it("spells the id shape each kind is addressed by", () => {
    expect(declarationIdShape(KIND_KIND)).toBe("<authority>/<name>")
    expect(declarationIdShape(ACTOR_KIND)).toBe("<name>")
    expect(declarationIdShape(AUTHORITY_KIND)).toBe("<authority>")
    expect(declarationIdShape(BUNDLE_KIND)).toBe("<authority>")
  })
})

describe("the authority an id says", () => {
  it("is the label in front of the slash", () => {
    expect(derivedAuthority(KIND_KIND, "tasks.example.com/task")).toBe(
      "tasks.example.com"
    )
    expect(derivedAuthority(AGENT_KIND, " crew.test.dev/scout ")).toBe(
      "crew.test.dev"
    )
  })

  it("is the whole id where the id IS the label", () => {
    expect(derivedAuthority(AUTHORITY_KIND, "tasks.example.com")).toBe(
      "tasks.example.com"
    )
    expect(derivedAuthority(BUNDLE_KIND, "tasks.example.com")).toBe(
      "tasks.example.com"
    )
  })

  it("is nothing an ACTOR's id can say: a bare name names no authority", () => {
    expect(authorityIsDerived(ACTOR_KIND)).toBe(false)
    expect(derivedAuthority(ACTOR_KIND, "console")).toBeUndefined()
  })

  it("is nothing at all for an ordinary kind, or a blank id", () => {
    expect(authorityIsDerived("tasks.example.com/task")).toBe(false)
    expect(derivedAuthority("tasks.example.com/task", "t-1")).toBeUndefined()
    expect(derivedAuthority(KIND_KIND, "  ")).toBeUndefined()
  })

  it("follows a half-typed id, because it is read as it is typed", () => {
    expect(derivedAuthority(KIND_KIND, "tasks.exa")).toBe("tasks.exa")
    expect(derivedAuthority(KIND_KIND, "tasks.example.com/")).toBe(
      "tasks.example.com"
    )
  })
})
