/** The unread rule: a settled chat is unread when its mark says so, when its
 * newest message is not the one the mark reads through, or when the person
 * has no mark of it; a running chat and one from before marks are not. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import {
  chatSections,
  markArchived,
  markPinned,
  marksByThread,
  readThroughToWrite,
  threadUnread,
  threadWaiting,
} from "./thread-marks"

const MESSAGE = "substrate.reamde.dev/llm/message"
const THREAD = "substrate.reamde.dev/llm/thread"

function record(id: string, properties: Record<string, unknown>) {
  return {
    id,
    kind: THREAD,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-11-14T10:00:00Z",
    updatedAt: "2026-11-14T10:00:00Z",
  } as unknown as SubstrateRecord
}

function thread(last?: string, status = "ok", more = {}) {
  return record("t1", {
    status,
    ...(last ? { lastMessage: { ref: `${MESSAGE}/${last}` } } : {}),
    ...more,
  })
}

function mark(through?: string, markedUnread?: boolean) {
  return record("t1.actor", {
    thread: { ref: `${THREAD}/t1` },
    ...(through ? { readThrough: { ref: `${MESSAGE}/${through}` } } : {}),
    ...(markedUnread === undefined ? {} : { markedUnread }),
  })
}

describe("threadUnread", () => {
  it("is read when the mark reads through the newest message", () => {
    expect(threadUnread(thread("m2"), mark("m2"))).toBe(false)
  })

  it("is unread when the newest message is past the mark", () => {
    expect(threadUnread(thread("m3"), mark("m2"))).toBe(true)
  })

  it("is unread when the person has no mark of it", () => {
    expect(threadUnread(thread("m1"), undefined)).toBe(true)
  })

  it("is unread when marked so, whatever the position", () => {
    expect(threadUnread(thread("m2"), mark("m2", true))).toBe(true)
  })

  it("is working, not unread, while it runs", () => {
    expect(threadUnread(thread("m3", "running"), mark("m2", true))).toBe(false)
  })

  it("stays read with a message of the person's own waiting past the reply they read", () => {
    // A message sent while a turn ran, which the turn never read (decision
    // 0318): the server never names it as `lastMessage`, and the dot reads
    // that property alone, never the rows. So the stopped chat names the
    // reply the mark reads through, and writes nothing more.
    const stopped = thread("m2", "stopped")
    expect(threadUnread(stopped, mark("m2"))).toBe(false)
    expect(readThroughToWrite(stopped, mark("m2"))).toBeUndefined()
  })

  it("is read when the thread names no newest message, as before marks", () => {
    expect(threadUnread(thread(), undefined)).toBe(false)
    expect(threadUnread(thread(), mark(undefined, true))).toBe(true)
  })
})

describe("readThroughToWrite", () => {
  it("names the newest message of an unread, settled chat", () => {
    expect(readThroughToWrite(thread("m3"), mark("m2"))).toBe("m3")
    expect(readThroughToWrite(thread("m2"), mark("m2", true))).toBe("m2")
  })

  it("writes nothing for a read chat, a running one or none", () => {
    expect(readThroughToWrite(thread("m2"), mark("m2"))).toBeUndefined()
    expect(readThroughToWrite(thread("m3", "running"), mark("m2"))).toBe(
      undefined
    )
    expect(readThroughToWrite(undefined, undefined)).toBeUndefined()
  })
})

describe("marksByThread and threadWaiting", () => {
  it("keys each mark by the thread it is of", () => {
    const m = mark("m1")
    expect(marksByThread([m]).get("t1")).toBe(m)
  })

  it("reads the thread's count of what waits, 0 where it says nothing", () => {
    expect(threadWaiting(thread("m1", "ok", { waitingOnPerson: 2 }))).toBe(2)
    expect(threadWaiting(thread("m1"))).toBe(0)
  })
})

describe("a mark made by a pin or an archive, on a chat never opened", () => {
  // The mark holds a flag and no position.
  const pinnedOnly = record("t1.actor", {
    thread: { ref: `${THREAD}/t1` },
    pinned: true,
  })

  it("leaves the chat as unread as one with no mark", () => {
    expect(markPinned(pinnedOnly)).toBe(true)
    expect(markArchived(pinnedOnly)).toBe(false)
    expect(threadUnread(thread("m1"), pinnedOnly)).toBe(true)
    // Opening it reads through its newest message, as for any unread chat.
    expect(readThroughToWrite(thread("m1"), pinnedOnly)).toBe("m1")
  })

  it("is not unread where the thread holds no newest message", () => {
    expect(threadUnread(thread(), pinnedOnly)).toBe(false)
    expect(threadUnread(thread("m1", "running"), pinnedOnly)).toBe(false)
  })

  it("reads a flag as off where the mark does not say, or there is none", () => {
    expect(markPinned(undefined)).toBe(false)
    expect(markArchived(mark("m1"))).toBe(false)
    expect(
      markArchived(record("t1.actor", { archived: false, pinned: false }))
    ).toBe(false)
  })
})

describe("chatSections", () => {
  const row = (id: string, more = {}) => ({
    thread: record(id, { status: "ok", ...more }),
  })
  const rows = [
    row("plain"),
    row("pinned"),
    row("archived"),
    row("both"),
    row("asking", { waitingOnPerson: 1 }),
    row("pinned-asking", { waitingOnPerson: 2 }),
  ]
  const flags = {
    pinned: new Set(["pinned", "both", "pinned-asking"]),
    archived: new Set(["archived", "both", "asking", "pinned-asking"]),
  }
  const ids = (list: { thread: SubstrateRecord }[]) =>
    list.map((r) => r.thread.id)

  it("puts the pinned chats in their own part, and never an archived one", () => {
    expect(ids(chatSections(rows, flags).pinned)).toEqual(["pinned"])
  })

  it("lists an archived chat only while it waits on a person", () => {
    expect(ids(chatSections(rows, flags).listed)).toEqual([
      "plain",
      "asking",
      "pinned-asking",
    ])
  })

  it("keeps every archived chat for the Archived view, waiting or pinned", () => {
    expect(ids(chatSections(rows, flags).archived)).toEqual([
      "archived",
      "both",
      "asking",
      "pinned-asking",
    ])
  })

  it("shows every chat where nothing is pinned or archived", () => {
    const none = { pinned: new Set<string>(), archived: new Set<string>() }
    const parts = chatSections(rows, none)
    expect(parts.listed).toHaveLength(rows.length)
    expect(parts.pinned).toEqual([])
    expect(parts.archived).toEqual([])
  })
})
