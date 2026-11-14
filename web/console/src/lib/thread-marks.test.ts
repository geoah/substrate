/** The unread rule: a settled chat is unread when its mark says so, when its
 * newest message is not the one the mark reads through, or when the person
 * has no mark of it; a running chat and one from before marks are not. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import {
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
