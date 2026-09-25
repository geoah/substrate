import { describe, expect, it } from "vitest"

import { stateTone, stateWord } from "./state-words"

describe("state words", () => {
  it("says a state in everyday words", () => {
    expect(stateWord("proposed")).toBe("Suggested")
    expect(stateWord("abandoned")).toBe("Dropped")
    expect(stateWord("open")).toBe("Open")
    expect(stateWord("in_review")).toBe("In review")
  })

  it("colours a state by what it means", () => {
    expect(stateTone("open")).toBe("active")
    expect(stateTone("done")).toBe("ok")
    expect(stateTone("proposed")).toBe("pending")
    expect(stateTone("abandoned")).toBe("stopped")
    expect(stateTone("failed")).toBe("bad")
  })

  it("reads an unknown state by whether the machine has moved", () => {
    expect(stateTone("triage", "triage")).toBe("stopped")
    expect(stateTone("shipping", "triage")).toBe("active")
  })
})
