/** The signed-in person's read state over the chats a page lists (decision
 * 0311): which are unread, the two actions a person takes on one, and the
 * write that follows reading the chat that is open.
 *
 * The open chat is read when it has settled and its window has been in
 * front for a moment; the write waits a little longer, so a run of replies
 * is one write, and leaving the chat sends it at once. A mark another
 * device wrote arrives on the change feed: the marks and the thread rows
 * are read again together, since a mark that reads through a reply this
 * page has not heard of yet would otherwise look unread.
 *
 * A server from before marks does not know the kind: nothing is unread,
 * nothing is written, and no stream is opened. */

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { useLiveRecords } from "@/hooks/use-live-records"
import { THREAD_LISTS_KEY } from "@/lib/api/agents"
import {
  myThreadMarksQueryOptions,
  THREAD_MARK_KIND,
  THREAD_MARKS_KEY,
  writeThreadMark,
} from "@/lib/api/thread-marks"
import type { Page, SubstrateRecord } from "@/lib/api/types"
import {
  marksByThread,
  readThroughToWrite,
  threadLastMessage,
  threadUnread,
} from "@/lib/thread-marks"

/** How long a settled chat is in front before it counts as read. */
export const SEEN_MS = 1000
/** How long the write of a read waits for a newer one to replace it. */
export const WRITE_MS = 2000

function subscribeVisibility(notify: () => void) {
  document.addEventListener("visibilitychange", notify)
  return () => document.removeEventListener("visibilitychange", notify)
}

/** Whether this window is the one being looked at. */
function useInFront(): boolean {
  return useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState === "visible",
    () => true
  )
}

export interface ThreadMarks {
  /** The ids of the listed chats that are unread. */
  unread: ReadonlySet<string>
  /** Whether the server keeps marks; false hides the two actions. */
  supported: boolean
  markUnread: (thread: string) => void
  markRead: (thread: string) => void
}

export function useThreadMarks(
  threads: readonly SubstrateRecord[],
  /** The chat being read, when one is. */
  open: SubstrateRecord | undefined
): ThreadMarks {
  const queryClient = useQueryClient()
  const ids = useMemo(() => {
    const out = threads.map((t) => t.id)
    if (open && !out.includes(open.id)) out.push(open.id)
    return out
  }, [threads, open])
  const query = useQuery(myThreadMarksQueryOptions(ids))
  const supported = query.isSuccess
  useLiveRecords(
    supported ? [THREAD_MARK_KIND] : [],
    [[...THREAD_MARKS_KEY], [...THREAD_LISTS_KEY]],
    undefined,
    query.data
  )
  const marks = useMemo(
    () => marksByThread(query.data?.records ?? []),
    [query.data]
  )
  const unread = useMemo(() => {
    const out = new Set<string>()
    if (!supported) return out
    for (const thread of threads)
      if (threadUnread(thread, marks.get(thread.id))) out.add(thread.id)
    return out
  }, [threads, marks, supported])

  /** The stored mark a write answered, put into every cached read of marks
   * so the list follows before the feed says so. */
  const keep = useCallback(
    (mark: SubstrateRecord) => {
      queryClient.setQueriesData<Page>(
        { queryKey: [...THREAD_MARKS_KEY] },
        (page) =>
          page && {
            ...page,
            records: [
              ...(page.records ?? []).filter((r) => r.id !== mark.id),
              mark,
            ],
          }
      )
    },
    [queryClient]
  )
  const write = useCallback(
    (thread: string, change: Parameters<typeof writeThreadMark>[1]) => {
      // A read that fails is tried again the next time the chat is opened;
      // it is nothing the person can act on.
      void writeThreadMark(thread, change).then(keep, () => {})
    },
    [keep]
  )

  // Mark as Unread on the chat that is open would be undone by reading it a
  // moment later: the chat stays unread until it is left or says more.
  const [kept, setKept] = useState<string>()
  const openKey = open ? `${open.id}:${threadLastMessage(open) ?? ""}` : ""
  const inFront = useInFront()
  const want =
    supported && open && kept !== openKey
      ? readThroughToWrite(open, marks.get(open.id))
      : undefined
  const openId = open?.id

  const queued = useRef<{
    thread: string
    message: string
    timer: ReturnType<typeof setTimeout>
  }>(undefined)
  const flush = useCallback(() => {
    const q = queued.current
    if (!q) return
    clearTimeout(q.timer)
    queued.current = undefined
    write(q.thread, { readThrough: q.message })
  }, [write])
  useEffect(() => {
    if (!openId || !want || !inFront) return
    const seen = setTimeout(() => {
      flush()
      queued.current = {
        thread: openId,
        message: want,
        timer: setTimeout(flush, WRITE_MS),
      }
    }, SEEN_MS)
    // Leaving the chat, the window going behind another, or a newer reply:
    // what was seen is written now.
    return () => {
      clearTimeout(seen)
      flush()
    }
  }, [openId, want, inFront, flush])
  useEffect(() => {
    window.addEventListener("pagehide", flush)
    return () => window.removeEventListener("pagehide", flush)
  }, [flush])
  // Another chat is open: the one marked unread may be read again later.
  const [keptFor, setKeptFor] = useState(openId)
  if (keptFor !== openId) {
    setKeptFor(openId)
    setKept(undefined)
  }

  const markUnread = useCallback(
    (thread: string) => {
      if (thread === openId) {
        // A read of it still waiting to be written is dropped with it.
        if (queued.current?.thread === thread) {
          clearTimeout(queued.current.timer)
          queued.current = undefined
        }
        setKept(openKey)
      }
      write(thread, { markedUnread: true })
    },
    [openId, openKey, write]
  )
  const markRead = useCallback(
    (thread: string) => {
      const listed =
        threads.find((t) => t.id === thread) ??
        (open?.id === thread ? open : undefined)
      const last = listed && threadLastMessage(listed)
      write(thread, last ? { readThrough: last } : { markedUnread: false })
    },
    [threads, open, write]
  )
  return { unread, supported, markUnread, markRead }
}
