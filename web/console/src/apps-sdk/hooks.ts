/** The React layer over `core.ts`: each hook is a subscription to something
 * the app object already holds (a records subscription, the route, the host
 * context, the declarations, the inputs), read through
 * `useSyncExternalStore` so a page renders the moment the host pushes it and
 * nothing here sets state from an effect. The hooks render only after
 * `createApp()` resolved, which the bootstrap guarantees. */

import { useCallback, useMemo, useSyncExternalStore } from "react"

import {
  currentApp,
  inputStatus,
  type Declaration,
  type HostContext,
  type InputState,
  type Page,
  type Query,
  type SdkError,
  type SubstrateRecord,
} from "./core"

/** A fresh object per store, never shared: React compares snapshots by
 * identity, and a new store whose first snapshot equalled the old one's
 * would look unchanged when the query moves, so the render that swaps the
 * subscription would be bailed out and the new one never opened. */
const loading = (): Page => ({ records: [], loading: true })

interface RecordsStore {
  subscribe(listener: () => void): () => void
  snapshot(): Page
  loadMore(): void
}

/** One live page per query, subscribed to the bridge while a component
 * listens and dropped when the last one leaves. `loadMore` appends the next
 * cursor's page; a pushed first page keeps what was appended, minus rows it
 * now carries itself, so a live change does not fold a walked list back to
 * one page. */
function createRecordsStore(q: Query): RecordsStore {
  const listeners = new Set<() => void>()
  let first: Page = loading()
  let extra: SubstrateRecord[] = []
  let snapshot: Page = first
  let off: (() => void) | undefined
  let walking = false

  const publish = () => {
    const seen = new Set(first.records.map((r) => `${r.kind} ${r.id}`))
    const appended = extra.filter((r) => !seen.has(`${r.kind} ${r.id}`))
    snapshot = appended.length
      ? { ...first, records: [...first.records, ...appended] }
      : first
    for (const l of [...listeners]) l()
  }

  return {
    subscribe(listener) {
      listeners.add(listener)
      if (listeners.size === 1) {
        off = currentApp().records.subscribe(q, (page) => {
          first = page
          publish()
        })
      }
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0) {
          off?.()
          off = undefined
        }
      }
    },
    snapshot: () => snapshot,
    loadMore() {
      const cursor = snapshot.cursor
      if (!cursor || walking) return
      walking = true
      currentApp()
        .records.list({ ...q, after: cursor })
        .then(
          (next) => {
            walking = false
            extra = [...extra, ...next.records]
            first = { ...first, cursor: next.cursor }
            publish()
          },
          (error: SdkError) => {
            walking = false
            first = { ...first, error }
            publish()
          }
        )
    },
  }
}

const stores = new Map<string, { store: RecordsStore; uses: number }>()

/** Stores are shared by query so two components over one query hold one
 * subscription, and released when the last render of the key is gone. */
function useRecordsStore(q: Query): RecordsStore {
  const key = JSON.stringify(q)
  const store = useMemo(() => {
    const held = stores.get(key)
    if (held) return held.store
    const made = createRecordsStore(JSON.parse(key) as Query)
    stores.set(key, { store: made, uses: 0 })
    return made
  }, [key])
  const subscribe = useCallback(
    (listener: () => void) => {
      const held = stores.get(key)
      if (held) held.uses++
      const off = store.subscribe(listener)
      return () => {
        off()
        const h = stores.get(key)
        if (h && --h.uses <= 0) stores.delete(key)
      }
    },
    [key, store]
  )
  useSyncExternalStore(subscribe, store.snapshot, store.snapshot)
  return store
}

export function useRecords(q: Query): Page & { loadMore(): void } {
  const store = useRecordsStore(q)
  const page = store.snapshot()
  return useMemo(() => ({ ...page, loadMore: store.loadMore }), [page, store])
}

export function useRecord(
  kind: string,
  id: string
): { record?: SubstrateRecord; loading: boolean; error?: SdkError } {
  const page = useRecords({ kind, filter: { ids: [id] }, first: 1 })
  return useMemo(
    () => ({
      record: page.records.find((r) => r.id === id),
      loading: page.loading,
      error: page.error,
    }),
    [page, id]
  )
}

export function useHost(): HostContext {
  const app = currentApp()
  return useSyncExternalStore(
    app.onHostContextChanged,
    () => app.hostContext,
    () => app.hostContext
  )
}

export function useKind(identity: string): Declaration | undefined {
  // Read through the context so a new one (a kind that landed) re-renders;
  // the declaration itself is cached per row by the app.
  const ctx = useHost()
  const kind = ctx.kinds?.find((k) => k.identity === identity)
  return kind ? currentApp().kinds.get(identity) : undefined
}

export function useInput(name: string): InputState & { status?: string } {
  const ctx = useHost()
  return useMemo(() => {
    const state: InputState = ctx.app?.inputs?.[name] ?? { state: "none" }
    return { ...state, status: inputStatus(name, state) }
  }, [ctx, name])
}

export function useRoute(): { path: string; navigate(path: string): void } {
  const app = currentApp()
  const path = useSyncExternalStore(
    app.route.onChange,
    () => app.route.path,
    () => app.route.path
  )
  return useMemo(() => ({ path, navigate: app.route.navigate }), [path, app])
}
