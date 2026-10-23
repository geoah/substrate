/** Chat messages whose send started no turn, kept per conversation for the
 * page's life. The agents page remounts the conversation on every switch,
 * so a message held in the component would be gone by the time somebody
 * came back to copy it. Memory only: nothing is written to storage and
 * nothing here is ever sent. The key carries the repository, so a sign-in
 * as somebody else in the same tab never shows another repository's text. */

import { useSyncExternalStore } from "react"

import { getRepository } from "@/lib/api/session"

/** What became of one send that started no turn. */
export interface Unsent {
  id: number
  /** `refused`: the server's own 4xx refusal, so nothing ran. `checking`
   * and `unknown`: anything else (a connection failure, a gateway's answer,
   * a 5xx), so the message may or may not have arrived. */
  kind: "refused" | "checking" | "unknown"
  /** The server's words, or what failed. */
  reason: string
  message: string
  /** The message was also put back into the composer. Its notice shows the
   * saved copy either way, because the composer may be cleared since. */
  restored: boolean
}

const EMPTY: Unsent[] = []
const lists = new Map<string, Unsent[]>()
const listeners = new Set<() => void>()
let seq = 0

/** The store key for a conversation: its thread, or `new:<agent>` for a new
 * chat whose thread is not known. */
export function unsentKey(thread: string, agentId?: string): string {
  const conversation = thread || (agentId ? `new:${agentId}` : "")
  return conversation ? `${getRepository() ?? ""}|${conversation}` : ""
}

function write(key: string, list: Unsent[]) {
  if (list.length) lists.set(key, list)
  else lists.delete(key)
  for (const listener of listeners) listener()
}

/** One conversation's unsent messages, oldest first. */
export function useUnsent(key: string): Unsent[] {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    () => lists.get(key) ?? EMPTY
  )
}

export function addUnsent(key: string, entry: Omit<Unsent, "id">): number {
  const id = ++seq
  if (key) write(key, [...(lists.get(key) ?? []), { ...entry, id }])
  return id
}

/** Replaces one entry, or drops it with null. */
export function setUnsent(
  key: string,
  id: number,
  entry: Omit<Unsent, "id"> | null
) {
  const list = lists.get(key)
  if (!list) return
  write(
    key,
    entry
      ? list.map((x) => (x.id === id ? { ...entry, id } : x))
      : list.filter((x) => x.id !== id)
  )
}

/** Keeps only the entries `keep` answers true for. */
export function keepUnsent(key: string, keep: (entry: Unsent) => boolean) {
  const list = lists.get(key)
  if (list) write(key, list.filter(keep))
}

/** Moves a new chat's entries to its thread once the thread is named. */
export function moveUnsent(from: string, to: string) {
  const list = lists.get(from)
  if (!list || from === to || !to) return
  lists.delete(from)
  write(to, [...(lists.get(to) ?? []), ...list])
}

/** Forgets every entry; for tests. */
export function resetUnsent() {
  lists.clear()
  for (const listener of listeners) listener()
}
