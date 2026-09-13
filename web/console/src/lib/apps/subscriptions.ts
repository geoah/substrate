/** The host's side of a guest subscription: one TanStack observer per
 * subscription over the console's own query cache, so a list open in the
 * console and the same list open in an app are one query, and the live tail's
 * invalidation of `["records", authority, package, name]` re-runs it and the
 * new page is pushed. A fan-out is one `QueriesObserver` over its reads. The
 * observer is what holds the query live; closing the subscription (or the
 * bridge) drops it, and the query goes cold with the rest. */

import {
  QueriesObserver,
  type QueryClient,
  type QueryObserverOptions,
  type QueryObserverResult,
} from "@tanstack/react-query"

import { recordsQueryOptions, type ListParams } from "@/lib/api/records"
import type { Page } from "@/lib/api/types"

/** How a read becomes query options, injectable so the store is tested
 * against a client that never fetches. */
export type OptionsFor = (
  params: ListParams
) => QueryObserverOptions<Page, Error, Page, Page, readonly unknown[]>

export interface SubscriptionResult {
  pages: (Page | undefined)[]
  errors: (Error | null)[]
  /** Every read has answered at least once. */
  settled: boolean
}

export interface SubscriptionStore {
  /** Open a subscription over `reads`; `onResult` fires on every observer
   * result whose data or error moved, the first one included. */
  open(
    id: string,
    reads: ListParams[],
    onResult: (result: SubscriptionResult) => void
  ): void
  close(id: string): void
  closeAll(): void
  /** The kind identities every open subscription reads, for the live tail. */
  kinds(): string[]
  readonly size: number
}

interface Open {
  kinds: string[]
  unsubscribe: () => void
}

function kindOf(p: ListParams): string {
  return `${p.authority}/${p.package}/${p.name}`
}

const defaultOptionsFor: OptionsFor = (params) =>
  recordsQueryOptions(params) as ReturnType<OptionsFor>

export function createSubscriptions(
  client: QueryClient,
  optionsFor: OptionsFor = defaultOptionsFor
): SubscriptionStore {
  const open = new Map<string, Open>()

  return {
    open(id, reads, onResult) {
      this.close(id)
      const observer = new QueriesObserver<Page[]>(
        client,
        reads.map((r) => optionsFor(r))
      )
      let last: { data: unknown; error: unknown }[] | undefined
      const deliver = (results: QueryObserverResult<Page, Error>[]) => {
        const now = results.map((r) => ({ data: r.data, error: r.error }))
        const moved =
          !last ||
          now.some(
            (n, i) => n.data !== last![i]?.data || n.error !== last![i]?.error
          )
        if (!moved) return
        last = now
        onResult({
          pages: results.map((r) => r.data),
          errors: results.map((r) => r.error ?? null),
          settled: results.every((r) => !r.isPending || r.isError),
        })
      }
      // An observer fires only on change, so the current result is delivered
      // by hand first: a cached page answers the subscribe at once.
      type Results = QueryObserverResult<Page, Error>[]
      const unsubscribe = observer.subscribe((results) =>
        deliver(results as Results)
      )
      deliver(observer.getCurrentResult() as Results)
      open.set(id, { kinds: reads.map(kindOf), unsubscribe })
    },
    close(id) {
      const entry = open.get(id)
      if (!entry) return
      open.delete(id)
      entry.unsubscribe()
    },
    closeAll() {
      for (const id of [...open.keys()]) this.close(id)
    },
    kinds() {
      const out = new Set<string>()
      for (const entry of open.values()) for (const k of entry.kinds) out.add(k)
      return [...out].sort()
    },
    get size() {
      return open.size
    },
  }
}
