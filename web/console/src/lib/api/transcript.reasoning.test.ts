/** The model's thinking on an assistant turn, as the row keeps it and as the
 * live `reasoning` events build it: the two must render the same turn. */

import { describe, expect, it } from "vitest"

import {
  EMPTY_OVERLAY,
  pushDelta,
  pushReasoning,
  pushToolStart,
  settleTool,
  transcriptOf,
} from "./transcript"
import type { SubstrateRecord } from "./types"

function row(id: string, properties: Record<string, unknown>): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-17T00:00:00Z",
    updatedAt: "2026-10-17T00:00:00Z",
  }
}

describe("reasoning on the transcript", () => {
  it("reads the row's reasoning and the time spent", () => {
    const [turn] = transcriptOf([
      row("a", {
        role: "assistant",
        content: "hello",
        reasoning: "think first",
        reasoningMillis: 12000,
      }),
    ])
    expect(turn.reasoning).toBe("think first")
    expect(turn.reasoningMillis).toBe(12000)
  })

  it("leaves a turn without reasoning bare", () => {
    const [turn] = transcriptOf([
      row("a", { role: "assistant", content: "hello", reasoningMillis: 5 }),
    ])
    expect(turn).not.toHaveProperty("reasoning")
    expect(turn).not.toHaveProperty("reasoningMillis")
  })

  it("never reads reasoning off a user row", () => {
    const [turn] = transcriptOf([
      row("u", { role: "user", content: "hi", reasoning: "no" }),
    ])
    expect(turn).not.toHaveProperty("reasoning")
  })
})

describe("reasoning in the live overlay", () => {
  it("counts from the first reasoning event to the first text", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "let me ", 1, 1000)
    live = pushReasoning(live, "think", 2, 1500)
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0].reasoning).toBe("let me think")
    expect(live.turns[0].reasoningStartedAt).toBe(1000)
    live = pushDelta(live, "Hi", 3, 4000)
    live = pushDelta(live, " there", 4, 9000)
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0]).toMatchObject({
      content: "Hi there",
      reasoning: "let me think",
      reasoningMillis: 3000,
    })
    expect(live.turns[0]).not.toHaveProperty("reasoningStartedAt")
  })

  it("stops the clock at a tool call, and opens a new turn after it", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "look it up", 1, 0)
    const call = { id: "c1", name: "peek", arguments: "{}" }
    live = pushToolStart(live, call, 2, 2000)
    expect(live.turns[0].reasoningMillis).toBe(2000)
    live = settleTool(live, call, "{}", true, 3)
    live = pushReasoning(live, "now answer", 4, 5000)
    expect(live.turns).toHaveLength(2)
    expect(live.turns[1]).toMatchObject({
      reasoning: "now answer",
      reasoningStartedAt: 5000,
      content: "",
    })
    expect(live.turns[0].reasoning).toBe("look it up")
  })
})
