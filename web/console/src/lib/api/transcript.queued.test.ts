/** A message sent while a turn runs (decision 0318), in the live overlay:
 * it waits at the bottom until the model is handed it, what the run does
 * meanwhile is drawn above it, and once taken it keeps its place with the
 * reply that reads it under it. */

import { describe, expect, it } from "vitest"

import {
  dropQueued,
  EMPTY_OVERLAY,
  hasWaiting,
  liveBeyond,
  pushCompacted,
  pushDelta,
  pushQueued,
  pushReasoning,
  pushToolStart,
  queuedIds,
  settleMentions,
  settleTool,
  takeQueued,
  type LiveOverlay,
} from "./transcript"
import type { SubstrateRecord } from "./types"

function row(id: string, turn: number, role: string): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/llm/message",
    properties: { turn, role, content: id },
    labels: {},
    version: 1,
    createdAt: "2026-11-14T10:00:00Z",
    updatedAt: "2026-11-14T10:00:00Z",
  }
}

/** The overlay as `role:content`, a waiting message marked `?`. */
function drawn(live: LiveOverlay): string[] {
  return live.turns.map((t) => {
    const calls = t.tools.map((c) => `[${c.id}]`).join("")
    const mark = t.queued === "waiting" ? "?" : ""
    return `${t.role}${mark}:${t.content}${calls}`
  })
}

/** A turn whose reply is streaming, with one message sent meanwhile. */
function streamingWithOneQueued(): LiveOverlay {
  let live = pushDelta(EMPTY_OVERLAY, "Looking", 1, { turn: 3, at: 0 })
  live = pushQueued(live, { id: "q1", content: "only the open ones" }, 2)
  return live
}

describe("a message sent while a turn runs", () => {
  it("waits at the bottom, under what the run does before it is read", () => {
    let live = streamingWithOneQueued()
    expect(drawn(live)).toEqual([
      "assistant:Looking",
      "user?:only the open ones",
    ])
    expect(hasWaiting(live)).toBe(true)
    expect(queuedIds(live)).toEqual(new Set(["q1"]))

    // The rest of the reply that was streaming, and the call it makes.
    live = pushDelta(live, " them up.", 3, { turn: 3, at: 7 })
    live = pushToolStart(
      live,
      { id: "c0", name: "query", arguments: "{}", turn: 5 },
      4
    )
    live = settleTool(
      live,
      { id: "c0", name: "query", arguments: "{}", turn: 5 },
      "{}",
      true,
      5
    )
    expect(drawn(live)).toEqual([
      "assistant:Looking them up.[c0]",
      "user?:only the open ones",
    ])
    // A reply that had not said a word when the message was sent, too: its
    // ordinal is below the message's.
    let early = pushQueued(
      EMPTY_OVERLAY,
      { id: "q1", content: "wait", turn: 4 },
      1
    )
    early = pushReasoning(early, "hm", 2, 0, { turn: 3, at: 0 })
    early = pushDelta(early, "First.", 3, { turn: 3, at: 0 })
    expect(drawn(early)).toEqual(["assistant:First.", "user?:wait"])
  })

  it("keeps its place once taken, with the reply that reads it under it", () => {
    let live = takeQueued(streamingWithOneQueued(), "q1")
    expect(hasWaiting(live)).toBe(false)
    live = pushDelta(live, "Two are open.", 6, { turn: 6, at: 0 })
    expect(drawn(live)).toEqual([
      "assistant:Looking",
      "user:only the open ones",
      "assistant:Two are open.",
    ])
    // Still one of the messages the overlay stands in for.
    expect(queuedIds(live)).toEqual(new Set(["q1"]))
  })

  it("takes every message sent before the one named: the model reads them in order", () => {
    let live = streamingWithOneQueued()
    live = pushQueued(live, { id: "q2", content: "and sort them" }, 3)
    live = pushQueued(live, { id: "q3", content: "newest first" }, 4)
    live = takeQueued(live, "q2")
    expect(drawn(live)).toEqual([
      "assistant:Looking",
      "user:only the open ones",
      "user:and sort them",
      "user?:newest first",
    ])
  })

  it("reads a reply past a message's ordinal as the model having read it", () => {
    let live = pushQueued(
      streamingWithOneQueued(),
      { id: "q1", content: "", turn: 4 },
      3
    )
    // The ordinal arrived for a message the overlay already held.
    expect(live.turns[1].sentTurn).toBe(4)
    expect(live.turns[1].content).toBe("only the open ones")
    // A tool event past it says nothing: that call was already under way.
    live = pushToolStart(
      live,
      { id: "c0", name: "query", arguments: "{}", turn: 5 },
      4
    )
    expect(hasWaiting(live)).toBe(true)
    live = pushReasoning(live, "so", 5, 0, { turn: 6, at: 0 })
    expect(hasWaiting(live)).toBe(false)
    expect(drawn(live)).toEqual([
      "assistant:Looking[c0]",
      "user:only the open ones",
      "assistant:",
    ])
  })

  it("draws a compaction and the reply's links above a waiting message", () => {
    let live = pushCompacted(
      streamingWithOneQueued(),
      { tokensBefore: 900, covered: 4 },
      3
    )
    expect(drawn(live).at(-1)).toBe("user?:only the open ones")
    live = settleMentions(streamingWithOneQueued(), [
      { kind: "~/tasks/task", id: "t1", text: "Tax" },
    ])
    expect(live.turns[0].mentions).toHaveLength(1)
  })

  it("holds a message once, and lets one the server never took go", () => {
    let live = streamingWithOneQueued()
    live = pushQueued(live, { id: "q1", content: "only the open ones" }, 3)
    expect(live.turns).toHaveLength(2)
    live = dropQueued(live, "q1")
    expect(drawn(live)).toEqual(["assistant:Looking"])
    expect(dropQueued(live, "q1")).toBe(live)
    expect(takeQueued(live, "nobody")).toBe(live)
  })

  it("is shown by the overlay, not by its stored row, while the overlay is up", () => {
    const live = takeQueued(streamingWithOneQueued(), "q1")
    const stored = [row("m0", 0, "user"), row("q1", 4, "user")]
    const rows = stored.filter((r) => !queuedIds(live).has(r.id))
    // Its row is left out of what the rows draw, so it is drawn once, where
    // the run read it: under the reply that was streaming.
    expect(liveBeyond(live, rows).map((t) => t.content)).toEqual([
      "Looking",
      "only the open ones",
    ])
  })
})
