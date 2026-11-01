/** The records a reply links, as the row keeps them and as the done event
 * carries them: the two must give the reply the same mentions. */

import { describe, expect, it } from "vitest"

import {
  EMPTY_OVERLAY,
  pushDelta,
  settleMentions,
  transcriptOf,
} from "./transcript"
import type { SubstrateRecord } from "./types"

const kind = "ada.example.com/tasks/task"

function row(id: string, properties: Record<string, unknown>): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
  }
}

describe("mentions on the transcript", () => {
  it("reads the row's mentions and drops a malformed entry", () => {
    const [turn] = transcriptOf([
      row("a", {
        role: "assistant",
        content: "see it",
        mentions: [
          { kind, id: "q4", text: "Q4", unread: true },
          { kind, id: "ghost", text: "Ghost", missing: true },
          { kind },
          "nonsense",
        ],
      }),
    ])
    expect(turn.mentions).toEqual([
      { kind, id: "q4", text: "Q4", unread: true },
      { kind, id: "ghost", text: "Ghost", missing: true },
    ])
  })

  it("leaves a turn without mentions bare", () => {
    const [turn] = transcriptOf([
      row("a", { role: "assistant", content: "hi" }),
    ])
    expect(turn).not.toHaveProperty("mentions")
  })

  it("puts the done event's mentions on the live reply", () => {
    const live = pushDelta(EMPTY_OVERLAY, "see it", 0)
    const mentions = [{ kind, id: "q4", text: "Q4" }]
    expect(settleMentions(live, mentions).turns[0].mentions).toEqual(mentions)
    expect(settleMentions(live, undefined)).toBe(live)
    expect(settleMentions(EMPTY_OVERLAY, mentions)).toBe(EMPTY_OVERLAY)
  })
})
