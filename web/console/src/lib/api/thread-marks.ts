/** The signed-in person's thread marks (decisions 0313 and 0322): how far
 * they have read each chat, and whether they pinned or archived it. One list
 * read serves a page of threads, filtered to the caller's own marks by
 * `readBy: me`, so another person's marks of the same threads never come
 * back. A write names the thread alone: the server builds the mark's id from
 * the thread and the person, fills in `readBy`, and keeps the position from
 * moving back, so a late write is answered with what is stored and is no
 * failure.
 *
 * The chat list is read in parts, each a list of threads the server narrows
 * by the caller's own marks (`referencedBy`): the chats they have not
 * archived, the ones they pinned however old, and the archived ones. */

import { queryOptions } from "@tanstack/react-query"

import {
  CHAT_LIST_WINDOW,
  CONVERSATION_MODES,
  conversationsQueryOptions,
} from "./agents"
import { CORE_AUTHORITY, LLM_PACKAGE, LLM_PACKAGE_NAME } from "./http"
import { createRecord, recordsQueryOptions } from "./records"
import type { ReferencedBy, SubstrateRecord } from "./types"

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

/** The revision of `records` from which a mark carries `pinned` and
 * `archived` and a list of threads is narrowed by the reader's own marks.
 * Below it the server refuses the two properties and the `referencedBy`
 * filter arm, so the page offers neither action and reads one list. */
export const PIN_ARCHIVE_REVISION = 9

/** The threads the caller's own mark flags (`exists`), or the threads no
 * mark of theirs flags: a thread they never opened has no mark, and is
 * neither pinned nor archived. */
export function byMyMark(
  flag: "pinned" | "archived",
  exists: boolean
): ReferencedBy {
  return {
    kind: THREAD_MARK_KIND,
    property: "thread",
    properties: { readBy: { eq: "me" }, [flag]: { eq: true } },
    ...(!exists && { exists: false }),
  }
}

/** The parts the chat list is read in. `all` is the one list a server from
 * before pin and archive serves. `archivedWaiting` is the archived chats
 * that wait on a person, which the list shows all the same: an archive
 * hides no question. */
export type ChatListPart =
  "all" | "unarchived" | "pinned" | "archived" | "archivedWaiting"

/** One part of the chat list, most recently active first. The pinned part
 * holds a chat the person also archived; the mark says so, and the list
 * keeps it with the archived ones. */
export function chatListQueryOptions(
  part: ChatListPart,
  first = CHAT_LIST_WINDOW
) {
  if (part === "all") return conversationsQueryOptions(first)
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: LLM_PACKAGE_NAME,
    name: "thread",
    first,
    filter: {
      properties: {
        mode: { in: CONVERSATION_MODES },
        ...(part === "archivedWaiting" && { waitingOnPerson: { gt: 0 } }),
      },
      referencedBy:
        part === "unarchived"
          ? byMyMark("archived", false)
          : byMyMark(part === "pinned" ? "pinned" : "archived", true),
    },
    orderBy: "updatedAt:desc",
  })
}

/** Write the caller's mark of a thread: read it through a message, set or
 * clear Mark as Unread, pin it, archive it, or several at once. A flag
 * written on a chat never opened makes the mark, with no position. The
 * answer is the mark as stored, which for a late write is the position
 * another device already moved it to. */
export function writeThreadMark(
  thread: string,
  change: {
    readThrough?: string
    markedUnread?: boolean
    pinned?: boolean
    archived?: boolean
  }
): Promise<SubstrateRecord> {
  return createRecord(CORE_AUTHORITY, LLM_PACKAGE_NAME, "threadmark", {
    properties: { thread, ...change },
  })
}
