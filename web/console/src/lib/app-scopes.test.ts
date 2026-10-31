import { describe, expect, it } from "vitest"

import { overAgents, scopeWords } from "./app-scopes"

describe("scopeWords", () => {
  it("names what each verb does over its pattern", () => {
    expect(scopeWords("read:ada.example.com/tasks/*")).toEqual({
      verb: "read",
      action: "Read records of",
      pattern: "ada.example.com/tasks/*",
    })
    expect(scopeWords("write:ada.example.com/tasks/task").action).toBe(
      "Read and write records of"
    )
    expect(scopeWords("agents:ada.example.com/llm/*")).toEqual({
      verb: "agents",
      action: "Talk to the agents matching",
      pattern: "ada.example.com/llm/*",
    })
    expect(scopeWords("decide:*").pattern).toBe("*")
  })

  it("keeps a scope it does not know as written", () => {
    expect(scopeWords("admin:*")).toEqual({
      verb: undefined,
      action: "",
      pattern: "admin:*",
    })
  })
})

describe("overAgents", () => {
  it("is true for the two agent verbs alone", () => {
    expect(overAgents("agents")).toBe(true)
    expect(overAgents("decide")).toBe(true)
    expect(overAgents("read")).toBe(false)
    expect(overAgents("write")).toBe(false)
  })
})
