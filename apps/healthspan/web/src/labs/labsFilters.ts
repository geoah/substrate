// The Labs filters: independent groups, AND across groups and OR within the
// status group, kept in the hash's query with the defaults left out.

import type { LabStatus } from "../rules/labs"
import { isRangeId, type RangeId, type TimeDomain } from "../lib/timeDomain"
import type { LabResult } from "./types"

export type Scope = "all" | "important"

export const SCOPES: { value: Scope; label: string }[] = [
  { value: "important", label: "Important" },
  { value: "all", label: "All" },
]

export const STATUSES: { value: LabStatus; label: string }[] = [
  { value: "range", label: "Out of range" },
  { value: "target", label: "Off target" },
  { value: "overdue", label: "Overdue" },
  { value: "ok", label: "In range" },
]

export interface LabsFilters {
  scope: Scope
  /** Selected statuses; empty is no constraint. */
  status: LabStatus[]
  range: RangeId
  /** A panel name, or undefined for all panels. */
  panel?: string
  /** A source id, or undefined for all sources. */
  source?: string
  q: string
}

/** Every key is present, the optional ones as undefined, so spreading the
 * defaults over a filter set clears all of it. */
export const DEFAULT_FILTERS: LabsFilters = {
  scope: "important",
  status: [],
  range: "2Y",
  panel: undefined,
  source: undefined,
  q: "",
}

const STATUS_ORDER = STATUSES.map((s) => s.value)

function isStatus(v: string): v is LabStatus {
  return (STATUS_ORDER as string[]).includes(v)
}

/** The filters a query holds; unknown values fall back to the defaults. */
export function parseFilters(params: URLSearchParams): LabsFilters {
  const scope = params.get("scope")
  const range = params.get("range")
  const status = (params.get("status") ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(isStatus)
  return {
    scope:
      scope === "all" || scope === "important" ? scope : DEFAULT_FILTERS.scope,
    status: STATUS_ORDER.filter((s) => status.includes(s)),
    range: isRangeId(range) ? range : DEFAULT_FILTERS.range,
    panel: params.get("panel") || undefined,
    source: params.get("source") || undefined,
    q: params.get("q") ?? "",
  }
}

/** The filters as query parameters, defaults omitted, `status`
 * comma-joined in a fixed order. */
export function filtersToParams(f: LabsFilters): URLSearchParams {
  const out = new URLSearchParams()
  if (f.scope !== DEFAULT_FILTERS.scope) out.set("scope", f.scope)
  const status = STATUS_ORDER.filter((s) => f.status.includes(s))
  if (status.length) out.set("status", status.join(","))
  if (f.range !== DEFAULT_FILTERS.range) out.set("range", f.range)
  if (f.panel) out.set("panel", f.panel)
  if (f.source) out.set("source", f.source)
  if (f.q) out.set("q", f.q)
  return out
}

/** What a view needs for filtering. `LabView` satisfies it. */
export interface Filterable {
  code: string
  name: string
  panel: string
  important: boolean
  statuses: Set<LabStatus>
  /** Present when any rule finds something on any series. */
  finding?: unknown
  /** Other names it goes by, searched too. */
  entry?: { aliases: string[] }
  series: { results: Pick<LabResult, "at" | "source">[] }[]
}

function after(r: Pick<LabResult, "at">, start: number): boolean {
  return Date.parse(r.at) >= start
}

type Group = "scope" | "status" | "range" | "panel" | "source" | "q"

/** Whether a view passes every group except those in `skip`. */
function passes(
  v: Filterable,
  f: LabsFilters,
  domain: TimeDomain,
  skip: Group[] = []
): boolean {
  const start =
    f.range === "All" ? Number.NEGATIVE_INFINITY : domain.start.getTime()
  const results = () => v.series.flatMap((s) => s.results)
  if (!skip.includes("scope") && f.scope === "important" && !v.important)
    return false
  if (
    !skip.includes("status") &&
    f.status.length &&
    !f.status.some((s) => v.statuses.has(s))
  )
    return false
  // A view with no result in the range is hidden unless a rule finds
  // something on it (an overdue result is old by definition).
  if (
    !skip.includes("range") &&
    !v.finding &&
    !results().some((r) => after(r, start))
  )
    return false
  if (!skip.includes("panel") && f.panel && v.panel !== f.panel) return false
  // A view kept for a finding is matched on any of its results, since the
  // range rule kept it for results outside the range.
  if (
    !skip.includes("source") &&
    f.source &&
    !results().some(
      (r) => r.source === f.source && (!!v.finding || after(r, start))
    )
  )
    return false
  if (!skip.includes("q")) {
    const needle = f.q.trim().toLowerCase()
    if (
      needle &&
      !v.name.toLowerCase().includes(needle) &&
      !v.code.toLowerCase().includes(needle) &&
      !(v.entry?.aliases ?? []).some((a) => a.toLowerCase().includes(needle))
    )
      return false
  }
  return true
}

/** The views the filters keep, in their input order. */
export function applyFilters<V extends Filterable>(
  views: readonly V[],
  f: LabsFilters,
  domain: TimeDomain
): V[] {
  return views.filter((v) => passes(v, f, domain))
}

export interface FilterCounts {
  /** Views per scope, counted before every other filter. */
  scope: Record<Scope, number>
  /** Views holding each status among those passing every other group. */
  status: Record<LabStatus, number>
}

/** The counts the toggles show. */
export function filterCounts(
  views: readonly Filterable[],
  f: LabsFilters,
  domain: TimeDomain
): FilterCounts {
  const status: Record<LabStatus, number> = {
    range: 0,
    target: 0,
    overdue: 0,
    ok: 0,
  }
  for (const v of views) {
    if (!passes(v, f, domain, ["status"])) continue
    for (const s of v.statuses) status[s]++
  }
  return {
    scope: {
      all: views.length,
      important: views.filter((v) => v.important).length,
    },
    status,
  }
}

/** Whether any filter differs from its default ("Clear filters" resets
 * all, range included). */
export function isFiltered(f: LabsFilters): boolean {
  return [...filtersToParams(f).keys()].length > 0
}

/** How many of the filters a phone keeps in its Filters sheet differ from
 * their defaults. */
export function activeSecondary(f: LabsFilters): number {
  return (
    f.status.length +
    (f.range !== DEFAULT_FILTERS.range ? 1 : 0) +
    (f.panel ? 1 : 0) +
    (f.source ? 1 : 0)
  )
}
