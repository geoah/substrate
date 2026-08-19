/** The transcript fold: `llmmessage` rows in, reader-shaped turns out. The
 * rules under test are the ones a live stream and a page reload must agree on
 * — pairing a tool result to the call that asked for it, by id. */

import { describe, expect, it } from "vitest"

import {
  decisionNoticeOf,
  interactionIdOf,
  interactionNoticeOf,
  proposedRequestId,
  requestIdOf,
  toolOK,
  transcriptOf,
  type ToolCallView,
  type TurnView,
} from "./transcript"
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
        name: "titler",
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
      row({ role: "tool", content: "second", toolCallId: "c2", name: "stats" }),
      row({ role: "tool", content: "first", toolCallId: "c1", name: "stats" }),
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
      row({ role: "tool", content: "late", toolCallId: "c1", name: "slow" }),
    ])
    expect(turns[0].tools[0].output).toBe("late")
    expect(turns[1].tools).toHaveLength(0)
  })

  it("gives an orphaned result its own turn rather than another turn's", () => {
    // Attributing the dispatch to a turn that did not make it is worse than
    // showing it unattached: the turn above really did not call this tool.
    const turns = transcriptOf([
      row({ role: "assistant", content: "prose" }),
      row({ role: "tool", content: "out", toolCallId: "gone", name: "ghost" }),
    ])
    expect(turns).toHaveLength(2)
    expect(turns[0].tools).toEqual([])
    expect(turns[1].tools[0]).toMatchObject({ name: "ghost", output: "out" })
  })

  it("does not let a duplicate result settle a second card", () => {
    const turns = transcriptOf([
      row({ role: "assistant", toolCalls: [call("c1", "t", "{}")] }),
      row({ role: "tool", content: "first", toolCallId: "c1", name: "t" }),
      row({ role: "tool", content: "again", toolCallId: "c1", name: "t" }),
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

/** A settled propose names the row it landed, and only then: the card links to
 * a change request that exists, never to a call that failed or is still out. */
describe("proposedRequestId", () => {
  const settled = (over: Record<string, unknown> = {}) => ({
    id: "c1",
    name: "propose",
    arguments: "{}",
    output: '{"id":"cr7abc4def6k"}',
    ok: true,
    ...over,
  })

  it("names the request a successful propose landed", () => {
    expect(proposedRequestId(settled())).toBe("cr7abc4def6k")
  })

  it("says nothing about a call still running, or one that failed", () => {
    expect(proposedRequestId(settled({ ok: undefined }))).toBeUndefined()
    expect(
      proposedRequestId(settled({ ok: false, output: '{"error":"nope"}' }))
    ).toBeUndefined()
  })

  it("says nothing about another tool that happens to answer with an id", () => {
    expect(proposedRequestId(settled({ name: "query" }))).toBeUndefined()
  })

  it("says nothing when the payload carries no id", () => {
    expect(proposedRequestId(settled({ output: "{}" }))).toBeUndefined()
    expect(
      proposedRequestId(settled({ output: '{"id":"  "}' }))
    ).toBeUndefined()
    expect(proposedRequestId(settled({ output: "landed" }))).toBeUndefined()
    expect(proposedRequestId(settled({ output: "{oops" }))).toBeUndefined()
  })
})

describe("system turns and engine-stamped changes", () => {
  const stamp = (seq: number, op: string, kind: string, id: string) => ({
    seq,
    op,
    kind,
    id,
  })

  it("gives a system row its own turn, carrying its changes", () => {
    const turns = transcriptOf([
      row({ role: "user", content: "hi", turn: 0 }),
      row({
        role: "system",
        content: '{"event":"proposalDecision","decision":"accepted"}',
        turn: 1,
        changes: [
          stamp(
            7,
            "patch",
            "core.substrate.reamde.dev/recordpatchrequest",
            "r1"
          ),
          stamp(8, "patch", "people.substrate.reamde.dev/person", "p1"),
        ],
      }),
    ])
    expect(turns.map((t) => t.role)).toEqual(["user", "system"])
    expect(turns[1].changes).toHaveLength(2)
    expect(turns[1].changes?.[1]).toMatchObject({ seq: 8, id: "p1" })
  })

  it("attaches a tool row's changes to the call it settles", () => {
    const turns = transcriptOf([
      row({
        role: "assistant",
        toolCalls: [call("c1", "mutate", "{}")],
        turn: 0,
      }),
      row({
        role: "tool",
        content: "{}",
        toolCallId: "c1",
        name: "mutate",
        ok: true,
        turn: 1,
        changes: [stamp(3, "put", "crew.test.dev/widget", "w1")],
      }),
    ])
    expect(turns[0].tools[0].changes).toEqual([
      { seq: 3, op: "put", kind: "crew.test.dev/widget", id: "w1" },
    ])
  })

  it("drops malformed change entries rather than throwing", () => {
    const turns = transcriptOf([
      row({
        role: "system",
        content: "x",
        turn: 0,
        changes: [{ seq: "nope" }, "junk", stamp(1, "put", "a.dev/b", "c")],
      }),
    ])
    expect(turns[0].changes).toEqual([
      { seq: 1, op: "put", kind: "a.dev/b", id: "c" },
    ])
  })
})

describe("requestIdOf", () => {
  const settled = (over: Partial<ToolCallView>): ToolCallView => ({
    id: "c1",
    name: "propose",
    arguments: "{}",
    output: '{"id":"abcdefghijkl"}',
    ok: true,
    ...over,
  })

  it("prefers the engine-stamped request entry over the payload sniff", () => {
    const found = requestIdOf(
      settled({
        name: "file",
        output: "created it",
        changes: [
          {
            seq: 4,
            op: "put",
            kind: "core.substrate.reamde.dev/recordpatchrequest",
            id: "r-stamped",
          },
        ],
      })
    )
    expect(found).toBe("r-stamped")
  })

  it("falls back to the payload sniff on rows without a stamp", () => {
    expect(requestIdOf(settled({}))).toBe("abcdefghijkl")
  })

  it("does not read another kind's stamp as a proposal", () => {
    const found = requestIdOf(
      settled({
        name: "mutate",
        output: "{}",
        changes: [
          { seq: 4, op: "put", kind: "crew.test.dev/widget", id: "w1" },
        ],
      })
    )
    expect(found).toBeUndefined()
  })
})

describe("decisionNoticeOf", () => {
  const system = (content: string): TurnView => ({
    key: "k",
    role: "system",
    content,
    tools: [],
  })

  it("decodes the engine's decision envelope", () => {
    const notice = decisionNoticeOf(
      system(
        JSON.stringify({
          event: "proposalDecision",
          request: "core.substrate.reamde.dev/recordpatchrequest/r1",
          decision: "accepted",
          op: "patch",
          target: "people.substrate.reamde.dev/person/p1",
          version: 4,
        })
      )
    )
    expect(notice).toEqual({
      requestId: "r1",
      decision: "accepted",
      op: "patch",
      target: "people.substrate.reamde.dev/person/p1",
      version: 4,
      deleted: undefined,
    })
  })

  it("says nothing about a system row that is not a decision", () => {
    expect(decisionNoticeOf(system("plain text"))).toBeUndefined()
    expect(decisionNoticeOf(system('{"event":"other"}'))).toBeUndefined()
  })
})

describe("interactions", () => {
  it("names the interaction a settled ask landed, from the stamp", () => {
    const call: ToolCallView = {
      id: "c1",
      name: "ask",
      arguments: "{}",
      output: '{"id":"iabcdefghijk"}',
      ok: true,
      changes: [
        {
          seq: 9,
          op: "put",
          kind: "core.substrate.reamde.dev/llminteraction",
          id: "iabcdefghijk",
        },
      ],
    }
    expect(interactionIdOf(call)).toBe("iabcdefghijk")
    expect(requestIdOf(call)).toBeUndefined()
  })

  it("decodes the answered envelope, answers and all", () => {
    const notice = interactionNoticeOf({
      key: "k",
      role: "system",
      tools: [],
      content: JSON.stringify({
        event: "interactionAnswered",
        interaction: "core.substrate.reamde.dev/llminteraction/iabcdefghijk",
        answers: [{ question: "color", selected: ["red"] }],
      }),
    })
    expect(notice).toEqual({
      interactionId: "iabcdefghijk",
      event: "interactionAnswered",
      answers: [{ question: "color", selected: ["red"] }],
    })
  })

  it("decodes a dismissal, and ignores other system rows", () => {
    const dismissed = interactionNoticeOf({
      key: "k",
      role: "system",
      tools: [],
      content: JSON.stringify({
        event: "interactionDismissed",
        interaction: "core.substrate.reamde.dev/llminteraction/iabcdefghijk",
      }),
    })
    expect(dismissed?.event).toBe("interactionDismissed")
    expect(
      interactionNoticeOf({
        key: "k",
        role: "system",
        tools: [],
        content: '{"event":"proposalDecision"}',
      })
    ).toBeUndefined()
  })
})
