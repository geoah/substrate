import { describe, expect, it } from "vitest"
import { scopeWords, traitOf } from "./words"

describe("scopeWords", () => {
  it("says a trait scope by the trait's name, as the Swift kit does", () => {
    expect(scopeWords("write:implements(substrate.reamde.dev/core/lifecycle)")).toBe(
      "See, add, change and delete your records of every kind with the lifecycle trait, including kinds that take it on later"
    )
    expect(scopeWords("read:implements(ada.example.com/work/dueDate)")).toBe(
      "See your records of every kind with the due date trait, including kinds that take it on later"
    )
  })

  it("keeps a kind pattern's words", () => {
    expect(scopeWords("write:substrate.reamde.dev/tasks/*")).toBe(
      "See, add, change and delete your records of the tasks package, including kinds added later"
    )
  })
})

describe("traitOf", () => {
  it("reads the trait out of implements(…) and nothing else", () => {
    expect(traitOf("implements(substrate.reamde.dev/core/lifecycle)")).toBe("substrate.reamde.dev/core/lifecycle")
    expect(traitOf("implements()")).toBeUndefined()
    expect(traitOf("substrate.reamde.dev/tasks/*")).toBeUndefined()
  })
})
