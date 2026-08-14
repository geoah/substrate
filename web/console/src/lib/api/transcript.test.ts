/** The transcript fold: `llmmessage` rows in, reader-shaped turns out. The
 * rules under test are the ones a live stream and a page reload must agree on
 * — pairing a tool result to the call that asked for it, by id. */

import { describe, expect, it } from "vitest"

import { toolOK, transcriptOf } from "./transcript"
import type { SubstrateRecord } from "./types"

let seq = 0

function row(properties: Record<string, unknown>): SubstrateRecord {
  seq++
  return {
    id: `m${seq}`,
    kind: "core.substrate.reamde.dev/llmmessage",
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-08-13T00:00:00Z",
    updatedAt: "2026-08-13T00:00:00Z",
  }
}

const call = (id: string, name: string, args: string) => ({
  id,
  name,
  arguments: args,
})

describe("transcriptOf", () => {
  it("folds a tool result into the call that dispatched it", () => {
    const turns = transcriptOf([
      row({ role: "user", content: "hi", turn: 0 }),
      row({
        role: "assistant",
        toolCalls: [call("c1", "titler", '{"id":"x"}')],
        turn: 1,
      }),
      row({
        role: "tool",
        content: '{"title":"ok"}',
        toolCallId: "c1",
        tool: "titler",
        turn: 2,
      }),
      row({ role: "assistant", content: "done", turn: 3 }),
    ])

    expect(turns.map((t) => t.role)).toEqual(["user", "assistant", "assistant"])
    expect(turns[1].tools).toHaveLength(1)
    expect(turns[1].tools[0]).toMatchObject({
      id: "c1",
      name: "titler",
      arguments: '{"id":"x"}',
      output: '{"title":"ok"}',
      ok: true,
    })
    // The tool row is consumed by its call, never rendered as its own turn.
    expect(turns).toHaveLength(3)
  })

  it("pairs by id, not by name, when one turn calls a tool twice", () => {
    const turns = transcriptOf([
      row({
        role: "assistant",
        toolCalls: [
          call("c1", "stats", '{"n":1}'),
          call("c2", "stats", '{"n":2}'),
        ],
        turn: 0,
      }),
      // Out of dispatch order on purpose: the ids decide, not the arrival.
      row({ role: "tool", content: "second", toolCallId: "c2", tool: "stats" }),
      row({ role: "tool", content: "first", toolCallId: "c1", tool: "stats" }),
    ])

    expect(turns[0].tools.map((t) => t.output)).toEqual(["first", "second"])
  })

  it("leaves a call with no result unsettled, so it reads as running", () => {
    const turns = transcriptOf([
      row({ role: "assistant", toolCalls: [call("c1", "slow", "{}")] }),
    ])
    expect(turns[0].tools[0].output).toBeUndefined()
    expect(turns[0].tools[0].ok).toBeUndefined()
  })

  it("pairs a result whose call landed in an earlier turn", () => {
    const turns = transcriptOf([
      row({ role: "assistant", toolCalls: [call("c1", "slow", "{}")] }),
      row({ role: "assistant", content: "thinking" }),
      row({ role: "tool", content: "late", toolCallId: "c1", tool: "slow" }),
    ])
    expect(turns[0].tools[0].output).toBe("late")
    expect(turns[1].tools).toHaveLength(0)
  })

  it("gives an orphaned result its own turn rather than another turn's", () => {
    // Attributing the dispatch to a turn that did not make it is worse than
    // showing it unattached: the turn above really did not call this tool.
    const turns = transcriptOf([
      row({ role: "assistant", content: "prose" }),
      row({ role: "tool", content: "out", toolCallId: "gone", tool: "ghost" }),
    ])
    expect(turns).toHaveLength(2)
    expect(turns[0].tools).toEqual([])
    expect(turns[1].tools[0]).toMatchObject({ name: "ghost", output: "out" })
  })

  it("does not let a duplicate result settle a second card", () => {
    const turns = transcriptOf([
      row({ role: "assistant", toolCalls: [call("c1", "t", "{}")] }),
      row({ role: "tool", content: "first", toolCallId: "c1", tool: "t" }),
      row({ role: "tool", content: "again", toolCallId: "c1", tool: "t" }),
    ])
    expect(turns[0].tools[0].output).toBe("first")
    // The second is an orphan, on its own — never overwriting the first.
    expect(turns).toHaveLength(2)
    expect(turns[1].tools[0].output).toBe("again")
  })

  it("drops a malformed call rather than rendering a nameless card", () => {
    const turns = transcriptOf([
      row({ role: "assistant", toolCalls: [{ arguments: "{}" }, "nonsense"] }),
    ])
    expect(turns[0].tools).toEqual([])
  })
})

describe("toolOK", () => {
  it("believes the declared ok property over the payload", () => {
    expect(
      toolOK(row({ role: "tool", ok: false, content: '{"fine":1}' }))
    ).toBe(false)
    expect(
      toolOK(row({ role: "tool", ok: true, content: '{"error":"x"}' }))
    ).toBe(true)
  })

  it("reads the loop's one-key error envelope as a failure", () => {
    expect(toolOK(row({ role: "tool", content: '{"error":"boom"}' }))).toBe(
      false
    )
  })

  it("does not mistake a result that merely carries an error key", () => {
    // A successful call whose payload reports a per-item error is not a
    // failed dispatch — the envelope has exactly one key, this has two.
    expect(
      toolOK(row({ role: "tool", content: '{"error":null,"items":[]}' }))
    ).toBe(true)
  })

  it("treats a non-JSON payload as a success", () => {
    expect(toolOK(row({ role: "tool", content: "plain text" }))).toBe(true)
  })
})
