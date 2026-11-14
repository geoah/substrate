/** The signed-in person's thread marks (decision 0313): how far they have
 * read each chat. One list read serves a page of threads, filtered to the
 * caller's own marks by `readBy: me`, so another person's marks of the same
 * threads never come back. A write names the thread alone: the server builds
 * the mark's id from the thread and the person, fills in `readBy`, and keeps
 * the position from moving back, so a late write is answered with what is
 * stored and is no failure. */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, LLM_PACKAGE, LLM_PACKAGE_NAME } from "./http"
import { createRecord, recordsQueryOptions } from "./records"
import type { SubstrateRecord } from "./types"

export const THREAD_MARK_KIND = `${LLM_PACKAGE}/threadmark`

/** The key every read of marks starts with, so a change to any mark
 * re-reads them all. */
export const THREAD_MARKS_KEY = ["records", [THREAD_MARK_KIND]] as const

/** The caller's own marks of these threads. A server from before marks does
 * not know the kind and answers 404: the read is not retried, and the list
 * shows no chat as unread, as it did before. */
export function myThreadMarksQueryOptions(threadIds: string[]) {
  return queryOptions({
    ...recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: LLM_PACKAGE_NAME,
      name: "threadmark",
      first: Math.max(threadIds.length, 1),
      filter: {
        properties: { thread: { in: threadIds }, readBy: { eq: "me" } },
      },
    }),
    enabled: threadIds.length > 0,
    retry: false,
  })
}

/** Write the caller's mark of a thread: read it through a message, set or
 * clear Mark as Unread, or both. The answer is the mark as stored, which for
 * a late write is the position another device already moved it to. */
export function writeThreadMark(
  thread: string,
  change: { readThrough?: string; markedUnread?: boolean }
): Promise<SubstrateRecord> {
  return createRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "threadmark", {
    properties: { thread, ...change },
  })
}
