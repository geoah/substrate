/** Live data for every surface that shows records: ONE change-feed tail per
 * tab, ref-counted over the union of every mounted caller's kinds, reopened
 * when the union changes and closed when the last caller unmounts. Decision
 * 0061 shapes what it does with a row: a change names the records it moved
 * (`affected`) and never their values, so the tail invalidates and the
 * queries refetch.
 *
 * The union always carries the app collection (`core/app`), so saving an app
 * remounts the guest showing it, the kind collection itself, so an import or
 * a declaration edit invalidates `["registry"]` and every declaration and
 * every implementor set derived from it follows, and the package collection,
 * whose rows hold the versions an app's floor is held against. Invalidation
 * is by identity, not by query: a change to `tasks/task/t9` touches every
 * `["records", authority, pkg, name]` list, the single-record read and the
 * bounded count, and so every guest subscription observing one of them.
 * Rows are batched for a beat so an import or a sync refetches once.
 *
 * SUBSCRIBE, THEN REFETCH: the tail opens at the head, so a write between a
 * list's read and the stream opening is in neither. The first `live` a tail
 * reports is the HANDOFF: every watched kind's lists, counts and
 * single-record reads are invalidated (an app edited in that gap would
 * otherwise run its older source under its older grant for as long as no
 * later change happened to name it), and so is the registry, because a
 * declaration can change in that gap too. A later `live` is a reconnect
 * resuming from the last seen seq, which misses nothing; the lists are
 * invalidated once more anyway, since `staleTime` alone schedules no refetch.
 *
 * The resume cursor is a seq in one history generation. Retention can
 * overtake it during an outage and a restore can retire its generation
 * while a tab is open; the transport reports either as `compacted` and
 * returns. Nothing read under the old cursor can be trusted, so the same
 * handoff runs and a fresh tail opens at the new head. A terminal error
 * frame is `stopped`: the tail is left down rather than hammered, and the
 * status store says so for a screen to show, with `retryLiveTail` as the
 * way back. */

import { useEffect, useSyncExternalStore } from "react"
import { useQueryClient, type QueryClient } from "@tanstack/react-query"

import { APP_KIND } from "@/lib/api/apps"
import {
  watchChanges,
  type WatchHandle,
  type WatchStatus,
} from "@/lib/api/changes"
import { CORE_PACKAGE, splitKind } from "@/lib/api/http"

const KIND_KIND = `${CORE_PACKAGE}/kind`
const PACKAGE_KIND = `${CORE_PACKAGE}/package`
const ALWAYS = [APP_KIND, KIND_KIND, PACKAGE_KIND]
const BATCH_MS = 150

interface Subscriber {
  kinds: string[]
  client: QueryClient
}

const subscribers = new Map<symbol, Subscriber>()
let tail: WatchHandle | undefined
let tailKey = ""
/** Which tail a callback belongs to: a stopped tail may still report `off`
 * after its successor opened, and a stale callback must not touch the
 * status or reopen anything. */
let tailGeneration = 0
let reconcileTimer: ReturnType<typeof setTimeout> | undefined
let flushTimer: ReturnType<typeof setTimeout> | undefined
const pending = new Map<string, Set<string>>()

// ── the status store ────────────────────────────────────────────────────────

/** The transport's states, plus `off` for no tail at all. `stopped` is
 * terminal until the union changes or `retryLiveTail` is called. */
export interface LiveStatus {
  state: WatchStatus
  /** The transport's word for a retry, a compaction or a stop. */
  detail?: string
}

let status: LiveStatus = { state: "off" }
const statusListeners = new Set<() => void>()

function setStatus(next: LiveStatus) {
  if (next.state === status.state && next.detail === status.detail) return
  status = next
  for (const listener of [...statusListeners]) listener()
}

export function subscribeLiveStatus(listener: () => void): () => void {
  statusListeners.add(listener)
  return () => void statusListeners.delete(listener)
}

export function liveStatusSnapshot(): LiveStatus {
  return status
}

/** The shared tail's state, for a screen that wants to say "live updates
 * stopped" and offer `retryLiveTail`. */
export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(
    subscribeLiveStatus,
    liveStatusSnapshot,
    liveStatusSnapshot
  )
}

/** Open a fresh tail at the head for the current union, whatever the old
 * one was doing: the recovery from `stopped`, and what a compaction does on
 * its own. */
export function retryLiveTail(): void {
  tailKey = ""
  scheduleReconcile()
}

// ── invalidation ────────────────────────────────────────────────────────────

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

/** The queries one kind's change touches: its lists and count, the named
 * records, or EVERY record read of the kind when no ids are named (a
 * handoff, which cannot know what moved). */
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
  if (!ids) {
    void client.invalidateQueries({
      queryKey: ["record", authority, pkg, name],
    })
    return
  }
  for (const id of ids) {
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

/** The handoff: everything read before this tail was at the head may be
 * stale, the vocabulary included. The implementor sets are derived from the
 * registry (`lib/apps/grant.ts`), so invalidating it refreshes them too. */
function handoff(kinds: string[]) {
  for (const client of clients()) {
    for (const kind of kinds) invalidateKind(client, kind)
    void client.invalidateQueries({ queryKey: ["registry"] })
  }
}

/** A reconnect from a cursor: the lists are refreshed, the rest was never
 * out of date. */
function refetchWatched(kinds: string[]) {
  for (const client of clients()) {
    for (const kind of kinds) {
      if (kind === KIND_KIND) continue
      invalidateKind(client, kind, new Set())
    }
  }
}

// ── the tail ────────────────────────────────────────────────────────────────

function reconcile() {
  reconcileTimer = undefined
  const kinds = subscribers.size ? union() : []
  const key = kinds.join("\n")
  if (key === tailKey) return
  tail?.stop()
  tail = undefined
  tailKey = key
  const generation = ++tailGeneration
  if (!kinds.length) {
    setStatus({ state: "off" })
    return
  }
  let handedOff = false
  tail = watchChanges({
    filter: { kinds },
    onRow: (row) => {
      if (generation !== tailGeneration) return
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
    onStatus: (state, detail) => {
      if (generation !== tailGeneration) return
      setStatus({ state, detail })
      if (state === "live") {
        if (handedOff) refetchWatched(kinds)
        else handoff(kinds)
        handedOff = true
      }
      // The transport does not reconnect after a terminal frame, and neither
      // does this: a union change or `retryLiveTail` opens the next one.
      if (state === "stopped") tail = undefined
    },
    onCompacted: () => {
      if (generation !== tailGeneration) return
      handoff(kinds)
      tail = undefined
      tailKey = ""
      scheduleReconcile()
    },
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
