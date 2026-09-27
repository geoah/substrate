import { describe, expect, it } from "vitest"

import {
  costWords,
  isConversation,
  policySentence,
  runStartWords,
  runStats,
  tokenWords,
} from "./agent-runs"
import type { SubstrateRecord } from "@/lib/api/types"

function rec(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "x",
    kind: "",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("agent runs", () => {
  it("sums what the runs spent and counts the ones that didn't finish", () => {
    expect(
      runStats([
        rec({ status: "ok", totalTokens: 1000, costUSD: 0.01 }),
        rec({ status: "error", totalTokens: 200 }),
        rec({ status: "overbudget", costUSD: 0.02 }),
        rec({ status: "running" }),
      ])
    ).toEqual({ runs: 4, failed: 2, tokens: 1200, cost: 0.03, priced: true })
    // No run carried a cost: the page says so rather than "$0".
    expect(runStats([rec({ status: "ok" })]).priced).toBe(false)
  })

  it("says what started a run, and which ones are conversations", () => {
    expect(runStartWords(rec({ mode: "schedule" }))).toBe("On a schedule")
    expect(runStartWords(rec({}))).toBe("A run")
    expect(isConversation(rec({ mode: "chat" }))).toBe(true)
    expect(isConversation(rec({ mode: "judge" }))).toBe(false)
  })

  it("reads tokens and dollars the way a person does", () => {
    expect(tokenWords(840)).toBe("840")
    expect(tokenWords(12_345)).toBe("12.3k")
    expect(tokenWords(250_000)).toBe("250k")
    expect(tokenWords(1_200_000)).toBe("1.2M")
    expect(costWords(0)).toBe("$0")
    expect(costWords(0.0004)).toBe("under $0.001")
    expect(costWords(0.0213)).toBe("$0.021")
    expect(costWords(3.456)).toBe("$3.46")
  })

  it("says a policy as a sentence about the agent", () => {
    const TASK = "samples.substrate.reamde.dev/tasks/task"
    expect(
      policySentence(rec({ action: "gate", selector: { kinds: [TASK] } }))
    ).toEqual({
      text: "Asks you before it can add, change and delete tasks",
      tone: "warn",
      everyone: true,
    })
    expect(
      policySentence(
        rec({
          action: "refuse",
          selector: { kinds: [TASK], ops: ["delete"], agents: ["a/b/c"] },
        })
      )
    ).toMatchObject({
      text: "Can’t delete tasks",
      tone: "bad",
      everyone: false,
    })
    expect(policySentence(rec({ action: "allow", selector: {} })).text).toBe(
      "Can add, change and delete anything in your data without asking"
    )
  })
})
