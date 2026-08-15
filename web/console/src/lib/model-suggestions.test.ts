import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { pricedModels } from "./model-suggestions"

function provider(pricing: unknown): SubstrateRecord {
  return {
    id: "openai",
    kind: "core.substrate.reamde.dev/llmprovider",
    properties: { pricing },
    labels: {},
    version: 1,
    createdAt: "2026-08-01T10:00:00Z",
    updatedAt: "2026-08-01T10:00:00Z",
  }
}

describe("pricedModels", () => {
  it("offers the models a provider prices, in declared order", () => {
    expect(
      pricedModels(
        provider([
          { model: "gpt-5", inputPer1M: 1 },
          { model: "gpt-4.1-mini", inputPer1M: 0.4 },
        ])
      )
    ).toEqual(["gpt-5", "gpt-4.1-mini"])
  })

  it("offers a duplicate once", () => {
    // The runtime already treats the later row as the winner; two identical
    // suggestions would just be a worse list.
    expect(
      pricedModels(provider([{ model: "gpt-5" }, { model: "gpt-5" }]))
    ).toEqual(["gpt-5"])
  })

  it("skips a row with no model id", () => {
    expect(
      pricedModels(
        provider([{ inputPer1M: 1 }, { model: "  " }, { model: "gpt-5" }])
      )
    ).toEqual(["gpt-5"])
  })

  it("answers nothing when there is no provider, or it prices nothing", () => {
    // Nothing is the right answer, not a guess: with no list the field is
    // plain free text, which is what it was before.
    expect(pricedModels(undefined)).toEqual([])
    expect(pricedModels(provider(undefined))).toEqual([])
    expect(pricedModels(provider([]))).toEqual([])
    expect(pricedModels(provider("not a list"))).toEqual([])
  })
})
