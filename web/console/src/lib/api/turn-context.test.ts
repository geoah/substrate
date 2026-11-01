/** The turn context (decision 0196) on the console's side: the chat body
 * carries the browser's zone and the records, and a user row's
 * `contextRecords` reach its turn as records, never as text. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { streamChat } from "./agents"
import { transcriptOf } from "./transcript"
import type { SubstrateRecord } from "./types"

afterEach(() => {
  vi.unstubAllGlobals()
})

function userRow(properties: Record<string, unknown>): SubstrateRecord {
  return {
    kind: "substrate.reamde.dev/llm/message",
    id: "m1",
    properties: { role: "user", turn: 0, ...properties },
    labels: {},
  } as unknown as SubstrateRecord
}

describe("the chat body", () => {
  it("sends the browser's zone and the records the message is about", async () => {
    let body: unknown
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init: RequestInit) => {
        body = JSON.parse(String(init.body))
        return new Response("", {
          status: 200,
          headers: { "Content-Type": "application/x-ndjson" },
        })
      })
    )
    await new Promise<void>((resolve) => {
      streamChat({
        agent: "a",
        message: "what is due?",
        context: ["ada.example.com/tasks/task/q4"],
        onEvent: () => {},
        onDone: resolve,
        onError: () => resolve(),
      })
    })
    expect(body).toEqual({
      thread: "",
      message: "what is due?",
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      context: ["ada.example.com/tasks/task/q4"],
    })
  })
})

describe("a user row's contextRecords", () => {
  it("reach the turn as records, and the block itself never does", () => {
    const [turn] = transcriptOf([
      userRow({
        content: "what is due?",
        context: "<turn_context>\nnow: x\n</turn_context>",
        contextRecords: [
          { kind: "ada.example.com/tasks/task", id: "q4" },
          { kind: "", id: "dropped" },
        ],
      }),
    ])
    expect(turn.content).toBe("what is due?")
    expect(turn.contextRecords).toEqual([
      { kind: "ada.example.com/tasks/task", id: "q4" },
    ])
  })
  it("leave a turn without them unmarked", () => {
    const [turn] = transcriptOf([userRow({ content: "hi" })])
    expect(turn.contextRecords).toBeUndefined()
  })
})
