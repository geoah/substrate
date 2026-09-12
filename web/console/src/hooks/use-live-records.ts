/** Live data for every surface that shows records: ONE change-feed tail per
 * tab, ref-counted over the union of every mounted caller's kinds, reopened
 * when the union changes and closed when the last caller unmounts. Decision
 * 0061 shapes what it does with a row: a change names the records it moved
 * (`affected`) and never their values, so the tail invalidates and the
 * queries refetch.
 *
 * The union always carries the two apps kinds (`core/view`, `core/app`), so
 * saving a view re-renders the screen showing it, and the kind collection
 * itself, so an import or a declaration edit invalidates `["registry"]` and
 * every `PropSpec` follows. Invalidation is by identity, not by query: a
 * change to `tasks/task/t9` touches every `["records", authority, pkg, name]`
 * list, the single-record read and the bounded count. Rows are batched for a
 * beat so an import or a sync refetches once.
 *
 * SUBSCRIBE, THEN REFETCH: the tail opens at the head, so a write between a
 * list's read and the stream opening is in neither. Every time the stream
 * reports `live` (first open, reopen on a union change, reconnect) the
 * watched kinds' record queries are invalidated once; `staleTime` alone
 * schedules no refetch. */

import { useEffect } from "react"
import { useQueryClient, type QueryClient } from "@tanstack/react-query"

import { APP_KIND, VIEW_KIND } from "@/lib/api/apps"
import { watchChanges, type WatchHandle } from "@/lib/api/changes"
import { CORE_PACKAGE, splitKind } from "@/lib/api/http"

const KIND_KIND = `${CORE_PACKAGE}/kind`
const ALWAYS = [VIEW_KIND, APP_KIND, KIND_KIND]
const BATCH_MS = 150

interface Subscriber {
  kinds: string[]
  client: QueryClient
}

const subscribers = new Map<symbol, Subscriber>()
let tail: WatchHandle | undefined
let tailKey = ""
let reconcileTimer: ReturnType<typeof setTimeout> | undefined
let flushTimer: ReturnType<typeof setTimeout> | undefined
const pending = new Map<string, Set<string>>()

function union(): string[] {
  const kinds = new Set<string>(ALWAYS)
  for (const s of subscribers.values()) for (const k of s.kinds) kinds.add(k)
  return [...kinds].sort()
}

function clients(): Set<QueryClient> {
  const out = new Set<QueryClient>()
  for (const s of subscribers.values()) out.add(s.client)
  return out
}

function invalidateKind(client: QueryClient, kind: string, ids?: Set<string>) {
  if (kind === KIND_KIND) {
    void client.invalidateQueries({ queryKey: ["registry"] })
  }
  const { authority, pkg, name } = splitKind(kind)
  void client.invalidateQueries({
    queryKey: ["records", authority, pkg, name],
  })
  void client.invalidateQueries({
    queryKey: ["records-count", authority, pkg, name],
  })
  for (const id of ids ?? []) {
    void client.invalidateQueries({
      queryKey: ["record", authority, pkg, name, id],
    })
  }
}

function flush() {
  flushTimer = undefined
  const touched = new Map(pending)
  pending.clear()
  for (const client of clients()) {
    for (const [kind, ids] of touched) invalidateKind(client, kind, ids)
  }
}

/** The handoff: what was read before the stream opened may be stale. */
function refetchWatched(kinds: string[]) {
  for (const client of clients()) {
    for (const kind of kinds) {
      if (kind === KIND_KIND) continue
      invalidateKind(client, kind)
    }
  }
}

function reconcile() {
  reconcileTimer = undefined
  const kinds = subscribers.size ? union() : []
  const key = kinds.join("\n")
  if (key === tailKey) return
  tail?.stop()
  tail = undefined
  tailKey = key
  if (!kinds.length) return
  tail = watchChanges({
    filter: { kinds },
    onRow: (row) => {
      const affected = row.affected?.length
        ? row.affected
        : [{ kind: row.kind, id: row.recordId }]
      for (const a of affected) {
        const ids = pending.get(a.kind) ?? new Set<string>()
        ids.add(a.id)
        pending.set(a.kind, ids)
      }
      if (flushTimer === undefined) flushTimer = setTimeout(flush, BATCH_MS)
    },
    onStatus: (status) => {
      if (status === "live") refetchWatched(kinds)
    },
    // A compacted cursor cannot happen here: the tail opens bare at the head
    // and resumes from what it has seen, and a restore between the two
    // reopens at the new head through the same reconnect.
  })
}

/** Deferred a tick, so a strict-mode remount or two callers mounting in one
 * commit reopen the tail once, not once per caller. */
function scheduleReconcile() {
  if (reconcileTimer !== undefined) clearTimeout(reconcileTimer)
  reconcileTimer = setTimeout(reconcile, 0)
}

/** The kinds the shared tail currently watches (for tests and the status
 * line); empty when no caller is mounted. */
export function liveTailKinds(): string[] {
  return tailKey ? tailKey.split("\n") : []
}

/** Join the shared tail for `kinds` (full references) while the caller is
 * mounted. `undefined` or an empty list contributes nothing, so a surface can
 * wait for its declaration before subscribing. */
export function useLiveRecords(kinds: string[] | undefined): void {
  const client = useQueryClient()
  // Keyed on the SET of kinds, not the array identity, so a caller may build
  // the list inline on every render.
  const key = kinds?.length ? JSON.stringify([...new Set(kinds)].sort()) : ""

  useEffect(() => {
    if (!key) return
    const id = Symbol("live-records")
    subscribers.set(id, { kinds: JSON.parse(key) as string[], client })
    scheduleReconcile()
    return () => {
      subscribers.delete(id)
      scheduleReconcile()
    }
  }, [key, client])
}
