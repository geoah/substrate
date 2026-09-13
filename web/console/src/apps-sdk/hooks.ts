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

/** One live list per query, subscribed to the bridge while a component
 * listens and dropped when the last one leaves.
 *
 * The first page is the subscription's: the host answers it and pushes it
 * again whenever the tail moves it. The pages `loadMore` walked past it are
 * the store's own, each read once under the cursor of the page before, so
 * after a push they are the ones that can be stale: a row deleted, edited or
 * filtered out on page two would stay as it was. So a push walks the same
 * depth again from the pushed page's cursor (a keyset cursor is the page
 * before it, so the walk is sequential) and the old pages stay on screen
 * until it lands. The cursor is always the last walked page's, never the
 * first's, so a push cannot make Load more fetch page two twice; a push
 * whose page has no cursor (the collection fits one page now) or carries a
 * `forbidden` error (the grant stopped covering the read) drops every walked
 * page; and rows are deduplicated by (kind, id) across all of them. */
function createRecordsStore(q: Query): RecordsStore {
  const listeners = new Set<() => void>()
  let first: Page = loading()
  let walked: Page[] = []
  /** How many pages past the first the reader asked for: what a re-walk
   * restores, a failed Load more included. */
  let depth = 0
  /** A Load more or a re-walk that failed, shown until the next one lands
   * or the next push. */
  let walkError: SdkError | undefined
  let snapshot: Page = first
  let off: (() => void) | undefined
  /** The walk in flight, by number: a push starts a new one and a result of
   * an older one is dropped, so a Load more answered after the push cannot
   * append a page that no longer follows the first. */
  let walk = 0
  let walking = false

  const cursor = () =>
    walked.length ? walked[walked.length - 1].cursor : first.cursor

  const publish = () => {
    const seen = new Set<string>()
    const records: SubstrateRecord[] = []
    for (const page of [first, ...walked]) {
      for (const r of page.records) {
        const key = `${r.kind} ${r.id}`
        if (seen.has(key)) continue
        seen.add(key)
        records.push(r)
      }
    }
    snapshot = {
      ...first,
      records,
      cursor: cursor(),
      error: first.error ?? walkError,
    }
    for (const l of [...listeners]) l()
  }

  const cancel = () => {
    walk++
    walking = false
  }

  const list = (after: string) => currentApp().records.list({ ...q, after })

  /** Run one walk; only the walk still current when it settles lands. */
  const run = (
    job: (token: number) => Promise<Page[]>,
    land: (pages: Page[]) => void
  ) => {
    cancel()
    const token = walk
    walking = true
    job(token).then(
      (pages) => {
        if (token !== walk) return
        walking = false
        walkError = undefined
        land(pages)
        depth = walked.length
        publish()
      },
      (error: SdkError) => {
        if (token !== walk) return
        walking = false
        walkError = error
        publish()
      }
    )
  }

  /** After a push: the walked pages read again to the asked depth, from the
   * pushed page's cursor. */
  const rewalk = () => {
    cancel()
    if (!depth) return
    const from = first.cursor
    if (!from) {
      walked = []
      depth = 0
      return
    }
    run(
      async (token) => {
        const pages: Page[] = []
        let after: string | undefined = from
        while (pages.length < depth && after && token === walk) {
          const next: Page = await list(after)
          pages.push(next)
          after = next.cursor
        }
        return pages
      },
      (pages) => {
        walked = pages
      }
    )
  }

  return {
    subscribe(listener) {
      listeners.add(listener)
      if (listeners.size === 1) {
        off = currentApp().records.subscribe(q, (page) => {
          first = page
          walkError = undefined
          if (page.error) {
            cancel()
            if (page.error.code === "forbidden") {
              walked = []
              depth = 0
            }
          } else {
            rewalk()
          }
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
      const after = cursor()
      if (!after || walking || first.loading || first.error) return
      depth = walked.length + 1
      run(
        async () => [await list(after)],
        (pages) => {
          walked = [...walked, ...pages]
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
