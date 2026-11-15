/** Read state of a chat, from two records: the thread, whose `lastMessage`
 * names the newest message it held when it last settled, and the signed-in
 * person's mark of it (`llm/threadmark`), whose `readThrough` names the
 * newest message they have seen (decision 0313). Nothing here is stored:
 * unread is derived each time the list is drawn.
 *
 * A thread with no `lastMessage` settled on a server from before marks, or
 * holds no message; it is never unread unless its mark says so.
 *
 * The same mark says whether the person pinned or archived the chat
 * (decision 0322). They may do either from the list, to a chat they never
 * opened, so a mark may hold no `readThrough`: that chat is as unread as one
 * with no mark. */

import type { SubstrateRecord } from "@/lib/api/types"
import { refId } from "@/lib/tools"

/** The message a thread's `lastMessage` names. */
export function threadLastMessage(thread: SubstrateRecord): string | undefined {
  return refId(thread.properties.lastMessage)
}

/** The message a mark reads through. */
export function markReadThrough(mark: SubstrateRecord): string | undefined {
  return refId(mark.properties.readThrough)
}

/** The marks keyed by the thread each is of. */
export function marksByThread(
  marks: readonly SubstrateRecord[]
): Map<string, SubstrateRecord> {
  const out = new Map<string, SubstrateRecord>()
  for (const mark of marks) {
    const thread = refId(mark.properties.thread)
    if (thread) out.set(thread, mark)
  }
  return out
}

/** Whether a chat is unread for the person whose mark this is: it has
 * settled, and they marked it unread, or its newest message is not the one
 * they read through, or they have no mark of it. A chat that is still
 * running is working, not unread. */
export function threadUnread(
  thread: SubstrateRecord,
  mark: SubstrateRecord | undefined
): boolean {
  if (thread.properties.status === "running") return false
  if (mark?.properties.markedUnread === true) return true
  const last = threadLastMessage(thread)
  if (!last) return false
  return !mark || markReadThrough(mark) !== last
}

/** What opening a chat should write, if anything: the message to read
 * through. Nothing while the chat runs, while it holds no `lastMessage`, or
 * when the mark already reads through it and is not marked unread. */
export function readThroughToWrite(
  thread: SubstrateRecord | undefined,
  mark: SubstrateRecord | undefined
): string | undefined {
  if (!thread || !threadUnread(thread, mark)) return undefined
  return threadLastMessage(thread)
}

/** How many of the thread's questions, suggested changes and calls needing
 * approval wait for a person (`waitingOnPerson`); 0 where the thread says
 * nothing. */
export function threadWaiting(thread: SubstrateRecord): number {
  const n = thread.properties.waitingOnPerson
  return typeof n === "number" && n > 0 ? n : 0
}

/** Whether the person pinned the chat this mark is of. */
export function markPinned(mark: SubstrateRecord | undefined): boolean {
  return mark?.properties.pinned === true
}

/** Whether the person archived the chat this mark is of. */
export function markArchived(mark: SubstrateRecord | undefined): boolean {
  return mark?.properties.archived === true
}

/** The chat list's three parts, from the rows read and the person's own
 * flags, each in the order the rows came in:
 *
 * - `archived`: what the Archived view lists;
 * - `pinned`: the pinned chats, above the rest; an archived chat is never
 *   here, whatever its pin says, and takes its place again when it is
 *   unarchived;
 * - `listed`: every other chat the list shows. An archived chat is in it
 *   only while it waits on a person: an archive hides a chat, never a
 *   question, a suggested change or a call that needs an answer. */
export function chatSections<Row extends { thread: SubstrateRecord }>(
  rows: readonly Row[],
  flags: { pinned: ReadonlySet<string>; archived: ReadonlySet<string> }
): { pinned: Row[]; listed: Row[]; archived: Row[] } {
  const out = {
    pinned: [] as Row[],
    listed: [] as Row[],
    archived: [] as Row[],
  }
  for (const row of rows) {
    const id = row.thread.id
    if (flags.archived.has(id)) {
      out.archived.push(row)
      if (threadWaiting(row.thread) > 0) out.listed.push(row)
    } else if (flags.pinned.has(id)) out.pinned.push(row)
    else out.listed.push(row)
  }
  return out
}
