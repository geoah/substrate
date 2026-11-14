/** Streamed text written where it belongs. A `delta` and a `reasoning`
 * event carry `at`, the code points of that turn's text that came before
 * the piece, so a piece is placed, not appended: a replay written over what
 * is shown changes nothing, and what the stored rows hold is left to them
 * without emptying the rest. */

import { describe, expect, it } from "vitest"

import {
  EMPTY_OVERLAY,
  liveBeyond,
  pushCompacted,
  pushDelta,
  pushReasoning,
  pushToolStart,
  settleTool,
  type LiveOverlay,
} from "./transcript"
import type { SubstrateRecord } from "./types"

function row(
  id: string,
  turn: number,
  role: string,
  more: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties: { turn, role, ...more },
    labels: {},
    version: 1,
    createdAt: "2026-11-14T10:00:00Z",
    updatedAt: "2026-11-14T10:00:00Z",
  }
}

/** Streams pieces of one turn in order, each at its position. */
function stream(live: LiveOverlay, turn: number, pieces: string[]) {
  let at = 0
  pieces.forEach((piece, i) => {
    live = pushDelta(live, piece, 100 + i, { turn, at })
    at += Array.from(piece).length
  })
  return live
}

describe("text placed by position", () => {
  const pieces = ["The balcony ", "plants 🌿 need ", "water twice a week."]
  const whole = pieces.join("")

  it("reads the same as appending when every piece arrives once, in order", () => {
    const live = stream(EMPTY_OVERLAY, 3, pieces)
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0].content).toBe(whole)
    expect(live.turns[0].live).toEqual({
      turn: 3,
      text: Array.from(whole).length,
      reasoning: 0,
    })
  })

  it("writes a replay over what is shown, changing nothing and keeping the turn's key", () => {
    const shown = stream(EMPTY_OVERLAY, 3, pieces.slice(0, 2))
    // An attach replays the turn's text so far as one piece from 0, then
    // goes on live.
    let live = pushDelta(shown, pieces[0] + pieces[1], 7, { turn: 3, at: 0 })
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0].key).toBe(shown.turns[0].key)
    expect(live.turns[0].content).toBe(pieces[0] + pieces[1])
    live = pushDelta(live, pieces[2], 8, {
      turn: 3,
      at: Array.from(pieces[0] + pieces[1]).length,
    })
    expect(live.turns[0].content).toBe(whole)
    // Applied a second time, whole, it is still the same text.
    const again = pushDelta(live, whole, 9, { turn: 3, at: 0 })
    expect(again.turns[0].content).toBe(whole)
  })

  it("counts code points, so a piece after an emoji lands after it", () => {
    // 🌿 is one code point and two UTF-16 units; a family is five code
    // points and eight units, and may be cut between them.
    const cut = ["One 👩‍", "👩‍👧 and e", "́ done."]
    const live = stream(EMPTY_OVERLAY, 5, cut)
    expect(live.turns[0].content).toBe(cut.join(""))
    // A replay of a shorter span, over the emoji, leaves the rest alone.
    const replayed = pushDelta(live, cut[0] + cut[1], 9, { turn: 5, at: 0 })
    expect(replayed.turns[0].content).toBe(cut.join(""))
    // And a piece replayed from the middle of the text lands in the middle.
    const middle = pushDelta(live, cut[1], 10, {
      turn: 5,
      at: Array.from(cut[0]).length,
    })
    expect(middle.turns[0].content).toBe(cut.join(""))
  })

  it("appends a piece that starts past what it holds, and keeps counting with the server", () => {
    // The replay was truncated: the next live piece starts further on.
    let live = pushDelta(EMPTY_OVERLAY, "Start ", 1, { turn: 2, at: 0 })
    live = pushDelta(live, "later", 2, { turn: 2, at: 40 })
    expect(live.turns[0].content).toBe("Start later")
    live = pushDelta(live, " on.", 3, { turn: 2, at: 45 })
    expect(live.turns[0].content).toBe("Start later on.")
  })

  it("starts each turn at 0, in a turn of its own once the last one's tools settled", () => {
    let live = stream(EMPTY_OVERLAY, 1, ["Looking."])
    const call = { id: "c1", name: "query", arguments: "{}", turn: 2 }
    live = pushToolStart(live, call, 5)
    live = settleTool(live, call, "[]", true, 6)
    live = stream(live, 3, ["Found ", "none."])
    expect(live.turns.map((t) => t.content)).toEqual([
      "Looking.",
      "Found none.",
    ])
    // A replay of the first turn goes to the first turn, though the second
    // is the one in flight.
    live = pushDelta(live, "Looking.", 9, { turn: 1, at: 0 })
    expect(live.turns.map((t) => t.content)).toEqual([
      "Looking.",
      "Found none.",
    ])
  })

  it("gives another row's text a turn of its own, though the last one never closed", () => {
    // A run's final reply leaves its turn open; the run an answer resumes
    // streams under a later ordinal, into the same overlay.
    let live = stream(EMPTY_OVERLAY, 3, ["I asked."])
    live = stream(live, 5, ["Noted: ", "red."])
    expect(live.turns.map((t) => [t.live?.turn, t.content])).toEqual([
      [3, "I asked."],
      [5, "Noted: red."],
    ])
    // So the rows take the first over and leave the second.
    expect(
      liveBeyond(live, [row("a-3", 3, "assistant")]).map((t) => t.content)
    ).toEqual(["Noted: red."])
  })

  it("appends from a server that sends no position", () => {
    let live = pushDelta(EMPTY_OVERLAY, "one ", 1)
    live = pushDelta(live, "two", 2)
    expect(live.turns[0].content).toBe("one two")
    expect(live.turns[0].live).toBeUndefined()
  })

  it("places reasoning apart from the reply, and a replay moves neither its text nor its clock", () => {
    let live = pushReasoning(EMPTY_OVERLAY, "weighing ", 1, 1_000, {
      turn: 4,
      at: 0,
    })
    live = pushReasoning(live, "it 🤔", 2, 3_000, { turn: 4, at: 9 })
    live = pushDelta(live, "The answer.", 3, { turn: 4, at: 0 })
    expect(live.turns[0]).toMatchObject({
      reasoning: "weighing it 🤔",
      content: "The answer.",
      reasoningMillis: 2_000,
      thinking: false,
    })
    // A minute later an attach replays the thinking and the text.
    live = pushReasoning(live, "weighing it 🤔", 4, 63_000, { turn: 4, at: 0 })
    live = pushDelta(live, "The answer.", 5, { turn: 4, at: 0 })
    expect(live.turns).toHaveLength(1)
    expect(live.turns[0]).toMatchObject({
      reasoning: "weighing it 🤔",
      content: "The answer.",
      reasoningMillis: 2_000,
      thinking: false,
    })
  })

  it("leaves a call and a compaction it already shows as they are", () => {
    const call = { id: "c1", name: "query", arguments: "{}", turn: 2 }
    let live = pushToolStart(EMPTY_OVERLAY, call, 1)
    live = settleTool(live, call, "[1]", true, 2)
    const replayed = settleTool(
      pushToolStart(live, call, 3),
      call,
      "[1]",
      true,
      4
    )
    expect(replayed.turns).toHaveLength(1)
    expect(replayed.turns[0].tools).toHaveLength(1)
    let marked = pushCompacted(live, { tokensBefore: 900, covered: 4 }, 5)
    marked = pushCompacted(marked, { tokensBefore: 900, covered: 4 }, 6)
    expect(marked.turns.filter((t) => t.role === "summary")).toHaveLength(1)
  })
})

describe("the live turns the stored rows do not show yet", () => {
  const call = { id: "c1", name: "query", arguments: "{}", turn: 2 }
  function overlay(): LiveOverlay {
    let live: LiveOverlay = {
      turns: [
        {
          key: "live-u1",
          role: "user",
          content: "hi",
          tools: [],
          sentId: "m-1",
        },
      ],
      closed: false,
    }
    live = stream(live, 1, ["Looking."])
    live = settleTool(pushToolStart(live, call, 5), call, "[]", true, 6)
    return stream(live, 3, ["Found "])
  }

  it("keeps everything while the rows hold none of it", () => {
    const live = overlay()
    expect(liveBeyond(live, [row("m-0", 0, "user")])).toEqual(live.turns)
  })

  it("leaves the sent message to its row once the row is stored, by its id", () => {
    const shown = liveBeyond(overlay(), [row("m-1", 0, "user")])
    expect(shown.map((t) => t.role)).toEqual(["assistant", "assistant"])
  })

  it("leaves a stored turn's text and its stored calls to the rows, and keeps the reply still streaming", () => {
    const live = overlay()
    const shown = liveBeyond(live, [
      row("m-1", 0, "user"),
      row("a-1", 1, "assistant"),
      row("t-2", 2, "tool"),
    ])
    expect(shown).toHaveLength(1)
    expect(shown[0].content).toBe("Found ")
    // The same turn object, under the key it had: nothing is rebuilt.
    expect(shown[0]).toBe(live.turns[2])
  })

  it("keeps a call that is still only live when its turn's text is stored", () => {
    const shown = liveBeyond(overlay(), [
      row("m-1", 0, "user"),
      row("a-1", 1, "assistant"),
    ])
    expect(shown).toHaveLength(2)
    expect(shown[0].content).toBe("")
    expect(shown[0].tools.map((c) => c.id)).toEqual(["c1"])
  })

  it("leaves a compaction to its summary row", () => {
    const live = pushCompacted(overlay(), { tokensBefore: 900, covered: 4 }, 9)
    expect(
      liveBeyond(live, [row("s-1", 4, "summary", { tokensBefore: 900 })]).some(
        (t) => t.role === "summary"
      )
    ).toBe(false)
    expect(
      liveBeyond(live, [row("m-0", 0, "user")]).some(
        (t) => t.role === "summary"
      )
    ).toBe(true)
  })
})
