import { describe, expect, it } from "vitest"

import { outcomeUnknown, stateTone, stateWord } from "./state-words"

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
    // A request whose outcome is not known needs the owner as a failure
    // does; one that ran out of time stopped as a cancelled one did.
    expect(stateTone("uncertain", "queued")).toBe("bad")
    expect(stateTone("expired", "queued")).toBe("stopped")
    expect(stateTone("sending", "queued")).toBe("active")
  })

  it("reads an unknown state by whether the machine has moved", () => {
    expect(stateTone("triage", "triage")).toBe("stopped")
    expect(stateTone("shipping", "triage")).toBe("active")
  })
})

describe("outcomeUnknown", () => {
  it("knows a state that says an attempt may or may not have happened", () => {
    expect(outcomeUnknown("uncertain")).toBe(true)
    expect(outcomeUnknown("Unknown")).toBe(true)
    expect(outcomeUnknown("failed")).toBe(false)
    expect(outcomeUnknown(undefined)).toBe(false)
  })
})
