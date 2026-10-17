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
  it("counts from the first piece of reasoning to the latest", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "let me ", 1, 1000)
    live = pushReasoning(live, "think", 2, 1500)
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0]).toMatchObject({
      reasoning: "let me think",
      reasoningStartedAt: 1000,
      reasoningMillis: 500,
      thinking: true,
    })
    live = pushDelta(live, "Hi", 3)
    live = pushDelta(live, " there", 4)
    expect(live.turns).toHaveLength(1)
    // The text ends the thinking; the time stays the reasoning's own span,
    // as the row measures it, not the wait for the first text.
    expect(live.turns[0]).toMatchObject({
      content: "Hi there",
      reasoning: "let me think",
      reasoningMillis: 500,
      thinking: false,
    })
  })

  it("keeps reasoning, text, reasoning, text from one completion in one turn", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "first, ", 1, 0)
    live = pushDelta(live, "Partly ", 2)
    live = pushReasoning(live, "then more", 3, 3000)
    expect(live.turns[0].thinking).toBe(true)
    live = pushDelta(live, "done.", 4)
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0]).toMatchObject({
      content: "Partly done.",
      reasoning: "first, then more",
      reasoningMillis: 3000,
      thinking: false,
    })
  })

  it("opens a new turn only after the tool round closes", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "look it up", 1, 0)
    const call = { id: "c1", name: "peek", arguments: "{}" }
    live = pushToolStart(live, call, 2)
    expect(live.turns[0].thinking).toBe(false)
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
