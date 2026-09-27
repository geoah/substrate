/** What a POINTER points at, as records to offer.
 *
 * A `reference` property pins the kind its value names, and a `KindInfo` is an
 * authority, a package and a name, so the registry the editor already holds
 * says which collection to read. That is the whole of `collectionFor`, and it
 * is the only place that decides.
 *
 * The four host functions need no special case: they are ordinary `function`
 * records, so they list beside a bundle's with their own cards. */

import { useEffect, useMemo, useState } from "react"
import { useQuery, type UseQueryResult } from "@tanstack/react-query"

import {
  fetchRecordsPage,
  recordsQueryOptions,
  type ListParams,
} from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { TO_ANY } from "@/lib/record-schema"

/** How many records a picker offers before it says so. The collections a
 * declaration usually pins to are registry-shaped (functions, agents,
 * authorities, providers, kinds), so one page is the whole set in every real
 * repository; a pointer at an ordinary data collection is why the cap is said
 * out loud rather than pretended away. */
export const PICKER_PAGE = 200

/** Where a collection lives: the three segments its path is built from. The
 * collection segment is the kind NAME (decision 0033). */
export interface Collection {
  authority: string
  package: string
  name: string
}

/** Which collection holds the records a pointer may name. A pointer at no
 * declared kind offers nothing, because there is no collection to offer and a
 * whole path is the only thing that names a record. */
export function collectionFor(
  pin: string | undefined,
  kinds: KindInfo[]
): Collection | undefined {
  if (!pin || pin === TO_ANY) return undefined
  const declared = kindByIdentity(kinds, pin)
  if (!declared?.name) return undefined
  return {
    authority: declared.authority,
    package: declared.package,
    name: declared.name,
  }
}

/** One offered record: the id a selection inserts (the pin supplies the kind
 * the write joins onto it), and what a reader needs to recognise it. A
 * function's card is its description, which is exactly what somebody choosing
 * a tool wants to read. */
export interface RecordOption {
  /** The record's own id, which is what the control holds and shows. */
  value: string
  title: string
  description: string
}

/** What the picker is offering, and how that read is going. */
export interface RecordOptions {
  options: RecordOption[]
  loading: boolean
  error?: string
  /** The collection outran the page: what is offered is not all there is, and
   * the control has to say so rather than imply the list is the limit. */
  capped: boolean
  /** Asks the read again, for a control offering "Try again" on `error`. */
  retry: () => void
}

function stringProp(
  properties: Record<string, unknown>,
  key: string
): string | undefined {
  const value = properties[key]
  return typeof value === "string" && value.trim() ? value.trim() : undefined
}

/** A record as one offered row: its title (the `title` column, else a declared
 * `name`), and its one-liner where it declares one. */
function optionOf(record: SubstrateRecord): RecordOption {
  const props = record.properties ?? {}
  return {
    value: record.id,
    title: recordTitle(props) || stringProp(props, "name") || "",
    description: stringProp(props, "description") ?? "",
  }
}

/** A read's progress as the picker words it. Only a read that is RUNNING is
 * loading: TanStack reports a paused (offline) or idle query with no data as
 * `isPending` too, and a spinner on that never stops. */
function pendingState(query: UseQueryResult<unknown>): {
  loading: boolean
  error?: string
} {
  if (query.error) return { loading: false, error: query.error.message }
  if (!query.isPending) return { loading: false }
  if (query.fetchStatus === "fetching") return { loading: true }
  if (query.fetchStatus === "paused") {
    return {
      loading: false,
      error: "You’re offline. The list reads when you’re back.",
    }
  }
  return { loading: false, error: "The list wasn’t read." }
}

/** The records a pointer may name, as picker rows. `self` drops one id: a
 * declaration that names itself as its own sub-agent is a loop nobody should
 * be able to spell by accident. */
export function useRecordOptions(
  pin: string | undefined,
  kinds: KindInfo[],
  self?: string
): RecordOptions {
  const collection = collectionFor(pin, kinds)
  const records = useQuery({
    ...recordsQueryOptions({
      authority: collection?.authority ?? "",
      package: collection?.package ?? "",
      name: collection?.name ?? "",
      first: PICKER_PAGE,
    }),
    enabled: Boolean(collection),
  })
  const page = records.data

  return useMemo(() => {
    const retry = () => void records.refetch()
    if (!collection) {
      return { options: [], loading: false, capped: false, retry }
    }
    return {
      options: (page?.records ?? [])
        .filter((r) => !(self && r.id === self))
        .map(optionOf),
      ...pendingState(records),
      capped: Boolean(page?.cursor),
      retry,
    }
  }, [collection, page, self, records])
}

/** What is typed into a picker, as the search grammar's type-ahead: every
 * plain word becomes a word prefix (`gra` → `gra*`), so a name is found
 * before it is finished. A word already carrying an operator (`-word`,
 * `"a phrase"`, `word*`, `OR`) is left as written. */
export function typeaheadQuery(text: string): string {
  return text
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((word) =>
      word !== "OR" && /^[\p{L}\p{N}]+$/u.test(word) ? `${word}*` : word
    )
    .join(" ")
}

/** The records of a collection whose indexed text matches what a reader
 * typed, as picker rows: the read a picker falls back to when the collection
 * outran PICKER_PAGE, so a record the first page did not carry is still
 * reachable by name. `filter.search` is a predicate over every text the kind
 * indexes, in the search grammar, and `typeaheadQuery` makes the typed words
 * prefixes. Off (no read, not loading) until `enabled` and something is
 * typed. */
export function useRecordSearch(
  pin: string | undefined,
  kinds: KindInfo[],
  text: string,
  enabled: boolean
): RecordOptions {
  const collection = collectionFor(pin, kinds)
  const words = typeaheadQuery(text)
  const on = Boolean(collection) && enabled && words.length > 0
  const records = useQuery({
    ...recordsQueryOptions({
      authority: collection?.authority ?? "",
      package: collection?.package ?? "",
      name: collection?.name ?? "",
      first: PICKER_PAGE,
      filter: { search: words },
    }),
    enabled: on,
  })
  const page = records.data

  return useMemo(() => {
    const retry = () => void records.refetch()
    if (!on) return { options: [], loading: false, capped: false, retry }
    return {
      options: (page?.records ?? []).map(optionOf),
      ...pendingState(records),
      capped: Boolean(page?.cursor),
      retry,
    }
  }, [on, page, records])
}

// ── the picker's read: a page to browse, the server to search ───────────────

/** How many records the picker browses before typing takes over: the most
 * recent, enough to recognise one without scrolling past it. Whatever else the
 * collection holds is a server search away. */
export const BROWSE_PAGE = 50

/** How long a picker waits for the server before it says so and offers a
 * retry. A read that never answers must not read as "still loading" forever
 * (owner report, 2026-09-26). */
export const PICKER_TIMEOUT_MS = 15_000

/** How long typing settles before the server is asked. */
export const PICKER_DEBOUNCE_MS = 200

/** Where a picker's read stands. `unresolved`: the pin names no kind this
 * repository declares, so there is nothing to read. `offline`: the browser is
 * offline and the read waits for it (TanStack pauses a query then, and a
 * paused query with no data is `isPending` forever). `error` carries a
 * message and is retried by hand. */
export type PickerStatus =
  "unresolved" | "loading" | "offline" | "error" | "ready"

export interface PickerRecords {
  /** The rows to offer: server matches first, then the loaded page's own. */
  options: RecordOption[]
  status: PickerStatus
  error?: string
  /** The browse page did not hold the whole collection. */
  capped: boolean
  /** What is typed is being answered by the server. */
  searching: boolean
  /** The search answer itself was capped: keep typing to narrow. */
  searchCapped: boolean
  /** A read is on its way, rows in hand or not. */
  busy: boolean
  /** The page held rows, and every one was left out (the record itself, or
   * what the caller already holds): there is nothing OTHER to choose. */
  allHeld: boolean
  retry: () => void
}

class PickerTimeout extends Error {
  constructor() {
    super("The server took too long to answer.")
  }
}

/** One page read that gives up after `ms`, aborting the request with it. */
async function pageWithin(params: ListParams, signal: AbortSignal, ms: number) {
  const inner = new AbortController()
  const forward = () => inner.abort()
  signal.addEventListener("abort", forward)
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    inner.abort()
  }, ms)
  try {
    return await fetchRecordsPage(params, inner.signal)
  } catch (error) {
    if (timedOut) throw new PickerTimeout()
    throw error
  } finally {
    clearTimeout(timer)
    signal.removeEventListener("abort", forward)
  }
}

function useSettled(value: string, ms: number): string {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(timer)
  }, [value, ms])
  return settled
}

/** Whether an option holds every typed word, in anything a reader can see. */
export function optionMatches(option: RecordOption, typed: string): boolean {
  const hay =
    `${option.value} ${option.title} ${option.description}`.toLowerCase()
  return typed
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => hay.includes(word))
}

function statusOf(
  query: UseQueryResult<unknown>
): Pick<PickerRecords, "status" | "error"> {
  if (query.data !== undefined) return { status: "ready" }
  if (query.isError) return { status: "error", error: query.error.message }
  if (query.fetchStatus === "fetching") return { status: "loading" }
  if (query.fetchStatus === "paused") return { status: "offline" }
  // Pending and idle: no read is running and none will start by itself
  // (a cancelled read reverts here). Said as what it is, with the retry.
  return { status: "error", error: "The list wasn’t read." }
}

/** The records a picker offers: the most recent page of the pinned
 * collection to browse, and, once something is typed, the server's own
 * search over the WHOLE collection (`filter.search`, typed words made
 * prefixes) merged with whatever the loaded page matches (so an id or a
 * one-liner the search index does not hold still finds its row). Every read
 * ends: rows, an empty answer, or an error the caller offers to retry. */
export function usePickerRecords(
  pin: string | undefined,
  kinds: KindInfo[],
  text: string,
  { self, exclude }: { self?: string; exclude?: ReadonlySet<string> } = {}
): PickerRecords {
  const collection = collectionFor(pin, kinds)
  const typed = useSettled(text.trim(), PICKER_DEBOUNCE_MS)
  const words = typeaheadQuery(typed)
  const base = {
    authority: collection?.authority ?? "",
    package: collection?.package ?? "",
    name: collection?.name ?? "",
    first: BROWSE_PAGE,
  }
  const page = useQuery({
    ...recordsQueryOptions(base),
    queryFn: ({ signal }) => pageWithin(base, signal, PICKER_TIMEOUT_MS),
    enabled: Boolean(collection),
    retry: false,
  })
  const capped = Boolean(page.data?.cursor)
  // A page that holds the whole collection is searched where it is; only a
  // capped one sends what is typed to the server.
  const searching = Boolean(collection) && capped && words.length > 0
  const searchParams = { ...base, filter: { search: words } }
  const found = useQuery({
    ...recordsQueryOptions(searchParams),
    queryFn: ({ signal }) =>
      pageWithin(searchParams, signal, PICKER_TIMEOUT_MS),
    enabled: searching,
    retry: false,
  })

  if (!collection) {
    return {
      options: [],
      status: "unresolved",
      capped: false,
      searching: false,
      searchCapped: false,
      busy: false,
      allHeld: false,
      retry: () => {},
    }
  }
  const keep = (o: RecordOption) =>
    !(self && o.value === self) && !exclude?.has(o.value)
  const loaded = (page.data?.records ?? []).map(optionOf)
  const local = loaded
    .filter(keep)
    .filter((o) => !typed || optionMatches(o, typed))
  const active = searching ? found : page
  const state = statusOf(active)
  // The search's placeholder is the previous search's answer, so rows stay
  // put while the next one is on its way.
  const remote = searching ? (found.data?.records ?? []).map(optionOf) : []
  const seen = new Set<string>()
  const options = [...remote.filter(keep), ...local].filter(
    (o) => !seen.has(o.value) && (seen.add(o.value), true)
  )
  return {
    options,
    status: state.status,
    error: state.error,
    busy: active.fetchStatus === "fetching",
    allHeld: loaded.length > 0 && !loaded.some(keep),
    capped,
    searching,
    searchCapped: searching && Boolean(found.data?.cursor),
    retry: () => void active.refetch(),
  }
}
